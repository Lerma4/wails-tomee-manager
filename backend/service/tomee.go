package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"tomee-manager/backend/model"

	"github.com/beevik/etree"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type TomEEService struct {
	configService *StorageService
	cmd           *exec.Cmd
	process       *os.Process
	ctx           context.Context
	mu            sync.Mutex
}

func NewTomEEService(storage *StorageService) *TomEEService {
	return &TomEEService{
		configService: storage,
	}
}

func (s *TomEEService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *TomEEService) emit(event string, data any) {
	if s.ctx != nil {
		wailsRuntime.EventsEmit(s.ctx, event, data)
	}
}

// IsRunning reports whether TomEE is up: either we own the process, or an
// instance started outside this app is answering on the configured HTTP port.
func (s *TomEEService) IsRunning() bool {
	s.mu.Lock()
	owned := s.process != nil
	s.mu.Unlock()
	// The health check below can block for up to httpProbeTimeout, so it runs
	// without the mutex — the Dashboard polls this every few seconds and must
	// never stall Start/Stop.
	if owned {
		return true
	}

	config, err := s.configService.LoadConfig()
	if err != nil {
		return false
	}
	return config.HTTPPort != 0 && httpAlive(config.HTTPPort)
}

// runningLocked reports whether TomEE is up, ours or not. Caller must hold s.mu.
func (s *TomEEService) runningLocked(config model.Config) bool {
	return s.process != nil || (config.HTTPPort != 0 && httpAlive(config.HTTPPort))
}

func (s *TomEEService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

// startLocked performs the actual start logic. Caller must hold s.mu.
func (s *TomEEService) startLocked() error {
	if s.process != nil {
		return fmt.Errorf("TomEE is already running (pid %d)", s.process.Pid)
	}

	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}

	if config.TomEEPath == "" {
		return fmt.Errorf("tomee path not configured")
	}

	// Check that all required ports are free
	if err := checkPortsFree(config); err != nil {
		return err
	}

	base, err := prepareInstance(config)
	if err != nil {
		return err
	}

	if err := updateServerXml(base, config); err != nil {
		return fmt.Errorf("failed to update server.xml: %w", err)
	}

	// The startup scripts always come from the installation; only CATALINA_BASE
	// moves when the instance is isolated.
	script := "catalina.sh"
	if runtime.GOOS == "windows" {
		script = "catalina.bat"
	}
	binPath := filepath.Join(config.TomEEPath, "bin", script)
	if runtime.GOOS != "windows" {
		_ = os.Chmod(binPath, 0755) // best effort: script may already be executable
	}

	cmd := command(binPath, "jpda", "run")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("JPDA_ADDRESS=%d", config.DebugPort),
		"JPDA_TRANSPORT=dt_socket",
		fmt.Sprintf("CATALINA_HOME=%s", config.TomEEPath),
		fmt.Sprintf("CATALINA_BASE=%s", base),
	)
	if config.JavaHome != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("JAVA_HOME=%s", config.JavaHome))
	}
	if opts := strings.TrimSpace(config.VMOptions); opts != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_OPTS=%s", opts))
	}

	// One pipe for both streams. Catalina's ConsoleHandler writes every log
	// record to stderr, so splitting the two would tag the whole log as errors
	// and scramble the ordering of the lines that do come from stdout.
	logReader, logWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Start(); err != nil {
		logReader.Close()
		logWriter.Close()
		return err
	}
	// The child holds its own handle now; ours has to go or the reader never
	// sees EOF.
	logWriter.Close()

	s.cmd = cmd
	s.process = cmd.Process
	// The process exists, but the server will not answer for a while yet.
	s.emit("tomee-status", "starting")

	go func() {
		defer logReader.Close()
		streamEntries(logReader, func(entry LogEntry) {
			s.emit("tomee-log", entry)
			if strings.Contains(entry.Text, startupMarker) {
				s.emit("tomee-status", "running")
				if config.OpenBrowser {
					wailsRuntime.BrowserOpenURL(s.ctx, appURL(config.HTTPPort))
				}
			}
		})
	}()

	// Clear process state when TomEE exits naturally
	go func() {
		_ = cmd.Wait() // exit status is irrelevant here; we only clear the state
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd = nil
			s.process = nil
		}
		s.mu.Unlock()
		s.emit("tomee-status", "stopped")
	}()

	return nil
}

// startupMarker is what Catalina logs once the server is ready to serve. Tomcat
// leaves this message untranslated, so matching the English text holds on a
// localised JVM too.
const startupMarker = "Server startup in"

func appURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/", port)
}

// OpenInBrowser opens the server root in the default browser.
func (s *TomEEService) OpenInBrowser() error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	if config.HTTPPort == 0 {
		return fmt.Errorf("http port not configured")
	}
	wailsRuntime.BrowserOpenURL(s.ctx, appURL(config.HTTPPort))
	return nil
}

func (s *TomEEService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

// stopLocked performs the actual stop logic. Caller must hold s.mu.
//
// Shutting down goes over the shutdown port, so this also stops an instance
// that was started outside this app — we do not need to own the process to shut
// it down cleanly.
func (s *TomEEService) stopLocked() error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	if config.TomEEPath == "" {
		return fmt.Errorf("tomee path not configured")
	}
	if !s.runningLocked(config) {
		return fmt.Errorf("TomEE is not running")
	}

	base, err := instanceDir(config)
	if err != nil {
		return err
	}

	if err := sendShutdownCommand(base, config.ShutdownPort); err != nil {
		// Graceful shutdown failed. Force kill only what we own — an externally
		// started instance has no process handle here, so the error stands.
		if s.process == nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.emit("tomee-log", LogEntry{
			Level: "WARN",
			Text:  fmt.Sprintf("Graceful shutdown failed (%v); killing the server process instead.", err),
		})
		_ = s.process.Kill()
	}
	s.cmd = nil
	s.process = nil
	return nil
}

// sendShutdownCommand speaks Tomcat's shutdown protocol directly: connect to the
// shutdown port, write the shutdown string, close.
//
// This replaces shutdown.bat, which spawns a whole JVM only to open this socket
// — and which fails on a dual-stack Windows host: Catalina resolves the default
// address "localhost" to ::1 and connects with the single-address Socket
// constructor, while the server binds its shutdown socket on 127.0.0.1. Doing
// it here also works for a server this app did not start.
func sendShutdownCommand(base string, port int) error {
	if port == 0 {
		return fmt.Errorf("shutdown port not configured")
	}
	address, command := shutdownEndpoint(base)

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach the shutdown port at %s:%d: %w", address, port, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte(command)); err != nil {
		return fmt.Errorf("failed to send the shutdown command: %w", err)
	}
	return nil
}

// shutdownEndpoint reads the shutdown address and command out of server.xml,
// falling back to Tomcat's documented defaults when either is absent.
func shutdownEndpoint(base string) (address, command string) {
	address, command = "127.0.0.1", "SHUTDOWN"

	doc := etree.NewDocument()
	if err := doc.ReadFromFile(filepath.Join(base, "conf", "server.xml")); err != nil {
		return address, command
	}
	server := doc.FindElement("//Server")
	if server == nil {
		return address, command
	}
	if value := server.SelectAttrValue("shutdown", ""); value != "" {
		command = value
	}
	// "localhost" is both the default and the value that misresolves, so it maps
	// to the loopback address the server actually binds; anything set explicitly
	// is honoured as written.
	if value := server.SelectAttrValue("address", ""); value != "" && !strings.EqualFold(value, "localhost") {
		address = value
	}
	return address, command
}

func (s *TomEEService) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	if s.runningLocked(config) {
		if err := s.stopLocked(); err != nil {
			return fmt.Errorf("failed to stop TomEE: %w", err)
		}
		// shutdown.bat returns as soon as the command is sent; the JVM keeps
		// running while it tears down data sources and contexts. Waiting for
		// the port to actually close beats a fixed sleep, which was either too
		// short to be safe or too long to sit through.
		if err := waitForPortFree(config.HTTPPort, shutdownTimeout); err != nil {
			return err
		}
	}
	return s.startLocked()
}

// shutdownTimeout is how long Restart waits for the old JVM to let go of the
// HTTP port. Applications with pooled data sources can take a while to close.
const shutdownTimeout = 60 * time.Second

func waitForPortFree(port int, timeout time.Duration) error {
	if port == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		if !portBusy(port) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("TomEE is still holding port %d after %s; it may not have shut down", port, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func updateServerXml(base string, config model.Config) error {
	serverXmlPath := filepath.Join(base, "conf", "server.xml")

	doc := etree.NewDocument()
	if err := doc.ReadFromFile(serverXmlPath); err != nil {
		return err
	}

	if server := doc.FindElement("//Server"); server != nil {
		server.CreateAttr("port", strconv.Itoa(config.ShutdownPort))
	}

	// Only the first plain HTTP connector: pointing several of them at the same
	// port stops the server from starting at all.
	for _, connector := range doc.FindElements("//Connector") {
		if !isPlainHTTPConnector(connector) {
			continue
		}
		connector.CreateAttr("port", strconv.Itoa(config.HTTPPort))
		break
	}

	return doc.WriteToFile(serverXmlPath)
}

// isPlainHTTPConnector picks out the connector the application is served on.
//
// In a real server.xml the protocol attribute is a class name
// ("org.apache.coyote.http11.Http11NioProtocol"), never the literal "HTTP/1.1",
// so it has to be matched case-insensitively. The HTTPS connector uses the very
// same protocol class and is only distinguishable by its SSL attributes.
func isPlainHTTPConnector(connector *etree.Element) bool {
	// An absent protocol attribute means HTTP/1.1, per the Tomcat docs.
	if strings.Contains(strings.ToUpper(connector.SelectAttrValue("protocol", "HTTP/1.1")), "AJP") {
		return false
	}
	for _, attr := range []string{"SSLEnabled", "secure"} {
		if strings.EqualFold(connector.SelectAttrValue(attr, ""), "true") {
			return false
		}
	}
	return !strings.EqualFold(connector.SelectAttrValue("scheme", ""), "https")
}

func checkPortsFree(config model.Config) error {
	ports := map[string]int{
		"HTTP":     config.HTTPPort,
		"Shutdown": config.ShutdownPort,
		"Debug":    config.DebugPort,
	}
	var busy []string
	for name, port := range ports {
		if port == 0 {
			continue
		}
		if portBusy(port) {
			busy = append(busy, fmt.Sprintf("%s port %d", name, port))
		}
	}
	if len(busy) > 0 {
		return fmt.Errorf("the following ports are already in use: %s", strings.Join(busy, ", "))
	}
	return nil
}

// httpProbeTimeout bounds the health check: short enough that the Dashboard
// poll stays responsive, long enough for a busy TomEE to answer.
const httpProbeTimeout = time.Second

// httpAlive reports whether something answers HTTP on the given local port.
// Any response counts — TomEE with no root webapp replies 404, which still
// means the server is up.
func httpAlive(port int) bool {
	client := &http.Client{Timeout: httpProbeTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// portBusy reports whether something is already listening on the given TCP port.
func portBusy(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}
