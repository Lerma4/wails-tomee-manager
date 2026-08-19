package service

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	// Update ports in server.xml
	if err := s.updateServerXml(config); err != nil {
		return fmt.Errorf("failed to update server.xml: %w", err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		binPath := filepath.Join(config.TomEEPath, "bin", "catalina.bat")
		cmd = exec.Command(binPath, "jpda", "run")
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("JPDA_ADDRESS=%d", config.DebugPort))
		cmd.Env = append(cmd.Env, "JPDA_TRANSPORT=dt_socket")
	} else {
		binPath := filepath.Join(config.TomEEPath, "bin", "catalina.sh")
		_ = os.Chmod(binPath, 0755) // best effort: script may already be executable
		cmd = exec.Command(binPath, "jpda", "run")
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("JPDA_ADDRESS=%d", config.DebugPort))
		cmd.Env = append(cmd.Env, "JPDA_TRANSPORT=dt_socket")
	}

	// Set CATALINA_HOME and CATALINA_BASE
	cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_HOME=%s", config.TomEEPath))
	cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_BASE=%s", config.TomEEPath))
	if config.JavaHome != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("JAVA_HOME=%s", config.JavaHome))
	}

	// Stream logs
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return err
	}

	s.cmd = cmd
	s.process = cmd.Process

	go s.streamLog(stdout, "INFO")
	go s.streamLog(stderr, "ERROR")

	// Clear process state when TomEE exits naturally
	go func() {
		_ = cmd.Wait() // exit status is irrelevant here; we only clear the state
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd = nil
			s.process = nil
		}
		s.mu.Unlock()
	}()

	return nil
}

func (s *TomEEService) streamLog(pipe javaIoReader, level string) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		text := scanner.Text()
		if s.ctx != nil {
			wailsRuntime.EventsEmit(s.ctx, "tomee-log", fmt.Sprintf("[%s] %s", level, text))
		}
	}
}

type javaIoReader interface {
	Read(p []byte) (n int, err error)
}

func (s *TomEEService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

// stopLocked performs the actual stop logic. Caller must hold s.mu.
//
// The shutdown script talks to TomEE over the shutdown port, so it also stops
// an instance that was started outside this app — we do not need to own the
// process to shut it down cleanly.
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

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		binPath := filepath.Join(config.TomEEPath, "bin", "shutdown.bat")
		cmd = exec.Command(binPath)
	} else {
		binPath := filepath.Join(config.TomEEPath, "bin", "shutdown.sh")
		_ = os.Chmod(binPath, 0755) // best effort: script may already be executable
		cmd = exec.Command(binPath)
	}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_HOME=%s", config.TomEEPath))
	cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_BASE=%s", config.TomEEPath))
	if config.JavaHome != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("JAVA_HOME=%s", config.JavaHome))
	}

	if err := cmd.Run(); err != nil {
		// Graceful shutdown failed. Force kill only what we own — an externally
		// started instance has no process handle here, so the error stands.
		if s.process != nil {
			_ = s.process.Kill()
		}
		s.cmd = nil
		s.process = nil
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	s.cmd = nil
	s.process = nil
	return nil
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
		time.Sleep(5 * time.Second)
	}
	return s.startLocked()
}

func (s *TomEEService) updateServerXml(config model.Config) error {
	serverXmlPath := filepath.Join(config.TomEEPath, "conf", "server.xml")

	doc := etree.NewDocument()
	if err := doc.ReadFromFile(serverXmlPath); err != nil {
		return err
	}

	// Update Server Shutdown Port
	// <Server port="...">
	if server := doc.FindElement("//Server"); server != nil {
		server.CreateAttr("port", fmt.Sprintf("%d", config.ShutdownPort))
	}

	// Update HTTP Connector Port
	// <Connector port="..." protocol="HTTP/1.1">
	for _, connector := range doc.FindElements("//Connector") {
		protocol := connector.SelectAttrValue("protocol", "")
		// Check if it's HTTP/1.1 or similar (often just HTTP/1.1 or org.apache.coyote.http11.Http11NioProtocol)
		if strings.Contains(protocol, "HTTP") || protocol == "" { // Assuming default is HTTP if not specified? No, AJP usually specifies protocol.
			// Let's look for standard HTTP connector.
			// Usually port 8080.
			// If we want to be precise, we might need more config from user, but let's assume the main HTTP connector.
			// Or we can check if it DOESN'T have "AJP" in protocol.
			if !strings.Contains(protocol, "AJP") {
				connector.CreateAttr("port", fmt.Sprintf("%d", config.HTTPPort))
			}
		}
	}

	return doc.WriteToFile(serverXmlPath)
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
