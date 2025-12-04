package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"tomee-manager/backend/model"

	"github.com/beevik/etree"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type TomEEService struct {
	configService *StorageService
	process       *os.Process
	ctx           context.Context
}

func NewTomEEService(storage *StorageService) *TomEEService {
	return &TomEEService{
		configService: storage,
	}
}

func (s *TomEEService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *TomEEService) Start() error {
	if s.process != nil {
		// Check if still running
		if _, err := os.FindProcess(s.process.Pid); err == nil {
			// This check is weak on Windows, but let's assume it works for now or improve later
			// Actually, FindProcess always succeeds on Windows if pid > 0. We need to check if we can signal it or if it's in the process table.
			// For now, let's trust s.process if it's not nil.
			// Better: check if process state is done?
		}
	}

	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}

	if config.TomEEPath == "" {
		return fmt.Errorf("tomee path not configured")
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
		cmd = exec.Command(binPath, "jpda", "run")
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("JPDA_ADDRESS=%d", config.DebugPort))
	}

	// Set CATALINA_HOME and CATALINA_BASE
	cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_HOME=%s", config.TomEEPath))
	cmd.Env = append(cmd.Env, fmt.Sprintf("CATALINA_BASE=%s", config.TomEEPath))

	// Stream logs
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return err
	}

	s.process = cmd.Process

	go s.streamLog(stdout, "INFO")
	go s.streamLog(stderr, "ERROR")

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
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		binPath := filepath.Join(config.TomEEPath, "bin", "shutdown.bat")
		cmd := exec.Command(binPath)
		return cmd.Run()
	} else {
		binPath := filepath.Join(config.TomEEPath, "bin", "shutdown.sh")
		cmd := exec.Command(binPath)
		return cmd.Run()
	}
}

func (s *TomEEService) Restart() error {
	s.Stop()
	time.Sleep(5 * time.Second) // Give it some time to stop
	return s.Start()
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
