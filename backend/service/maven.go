package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type MavenService struct {
	storage *StorageService
	ctx     context.Context
	mu      sync.Mutex
	builds  map[int]*exec.Cmd
}

func NewMavenService(storage *StorageService) *MavenService {
	return &MavenService{
		storage: storage,
		builds:  make(map[int]*exec.Cmd),
	}
}

func (s *MavenService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// FindProjectRoot walks up from the directory containing sourcePath
// looking for a pom.xml. Returns error after 10 levels or at filesystem root.
func (s *MavenService) FindProjectRoot(sourcePath string) (string, error) {
	dir := filepath.Dir(sourcePath)
	const maxLevels = 10

	for range maxLevels {
		pomPath := filepath.Join(dir, "pom.xml")
		if _, err := os.Stat(pomPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no pom.xml found: reached filesystem root from %s", sourcePath)
		}
		dir = parent
	}

	return "", fmt.Errorf("no pom.xml found within %d levels above %s", maxLevels, sourcePath)
}

// CheckWarExists returns true if the file at sourcePath exists and is a regular file.
func (s *MavenService) CheckWarExists(sourcePath string) bool {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// RunBuild executes `mvn clean install` in the project root derived from the WAR's sourcePath.
// Streams output via Wails events. Only one build per WAR ID at a time.
func (s *MavenService) RunBuild(warID int, profile string) error {
	war, err := s.storage.GetWar(warID)
	if err != nil {
		return fmt.Errorf("WAR artifact not found: %w", err)
	}

	projectRoot, err := s.FindProjectRoot(war.SourcePath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if _, exists := s.builds[warID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("build already in progress for WAR %d", warID)
	}

	var mvnCmd string
	if runtime.GOOS == "windows" {
		mvnCmd = "mvn.cmd"
	} else {
		mvnCmd = "mvn"
	}

	args := []string{"clean", "install", "-DskipTests"}
	if profile != "" {
		args = append(args, "-P"+profile)
	}
	cmd := exec.Command(mvnCmd, args...)
	cmd.Dir = projectRoot
	cmd.Env = os.Environ()

	s.builds[warID] = cmd
	s.mu.Unlock()

	logEvent := fmt.Sprintf("maven-log-%d", warID)
	doneEvent := fmt.Sprintf("maven-done-%d", warID)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.removeBuild(warID)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.removeBuild(warID)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		s.removeBuild(warID)
		return fmt.Errorf("failed to start mvn: %w", err)
	}

	// Stream stdout and stderr in background goroutines
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if s.ctx != nil {
				wailsRuntime.EventsEmit(s.ctx, logEvent, scanner.Text())
			}
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if s.ctx != nil {
				wailsRuntime.EventsEmit(s.ctx, logEvent, scanner.Text())
			}
		}
	}()

	// Wait for process completion in background, then emit done event
	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		s.removeBuild(warID)

		result := map[string]any{
			"success": waitErr == nil,
			"error":   "",
		}
		if waitErr != nil {
			result["error"] = waitErr.Error()
		}
		if s.ctx != nil {
			wailsRuntime.EventsEmit(s.ctx, doneEvent, result)
		}
	}()

	return nil
}

func (s *MavenService) removeBuild(warID int) {
	s.mu.Lock()
	delete(s.builds, warID)
	s.mu.Unlock()
}
