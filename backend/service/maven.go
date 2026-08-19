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

// findWarInTarget scans <projectDir>/target/ for the first .war file and returns its full path.
func findWarInTarget(projectDir string) (string, error) {
	targetDir := filepath.Join(projectDir, "target")
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return "", fmt.Errorf("cannot read target directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".war") {
			return filepath.Join(targetDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no .war file found in %s", targetDir)
}

// CheckWarExists returns true if a .war file exists in <projectDir>/target/.
func (s *MavenService) CheckWarExists(projectDir string) bool {
	_, err := findWarInTarget(projectDir)
	return err == nil
}

// validateProjectDir normalizes the project directory and checks it is safe to
// operate on. Returns the cleaned path to use for the build.
func validateProjectDir(projectDir string) (string, error) {
	projectDir = filepath.Clean(strings.TrimSpace(projectDir))
	if projectDir == "" || projectDir == "." {
		return "", fmt.Errorf("project directory path is empty")
	}
	if !filepath.IsAbs(projectDir) {
		return "", fmt.Errorf("project directory must be an absolute path: %s", projectDir)
	}
	return projectDir, nil
}

// RunBuild deletes <projectDir>/target, then executes `mvn install` in the project directory.
// Streams output via Wails events. Only one build per WAR ID at a time.
func (s *MavenService) RunBuild(warID int, profile string) error {
	war, err := s.storage.GetWar(warID)
	if err != nil {
		return fmt.Errorf("WAR artifact not found: %w", err)
	}

	projectDir, err := validateProjectDir(war.SourcePath)
	if err != nil {
		return err
	}

	// Validate that the directory contains a pom.xml before deleting target
	pomPath := filepath.Join(projectDir, "pom.xml")
	if _, err := os.Stat(pomPath); os.IsNotExist(err) {
		return fmt.Errorf("no pom.xml found in %s", projectDir)
	}

	s.mu.Lock()
	if _, exists := s.builds[warID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("build already in progress for WAR %d", warID)
	}

	// Delete target directory while holding the lock to prevent races
	targetDir := filepath.Join(projectDir, "target")
	if err := os.RemoveAll(targetDir); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to remove target directory: %w", err)
	}

	var mvnCmd string
	if runtime.GOOS == "windows" {
		mvnCmd = "mvn.cmd"
	} else {
		mvnCmd = "mvn"
	}

	args := []string{"install", "-DskipTests"}
	if profile != "" {
		args = append(args, "-P"+profile)
	}
	cmd := command(mvnCmd, args...)
	cmd.Dir = projectDir
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
