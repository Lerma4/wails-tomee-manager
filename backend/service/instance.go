package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tomee-manager/backend/model"

	"github.com/beevik/etree"
)

// instanceDir returns the CATALINA_BASE the server should run against: the
// installation itself, or a private directory when the config asks for an
// isolated instance.
func instanceDir(config model.Config) (string, error) {
	if config.TomEEPath == "" {
		return "", fmt.Errorf("tomee path not configured")
	}
	if !config.IsolatedBase {
		return config.TomEEPath, nil
	}
	appDataDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDataDir, "tomee-manager", "instance"), nil
}

// prepareInstance returns the CATALINA_BASE with the layout Tomcat expects
// already in place. For an isolated instance the conf/ tree is seeded once from
// the installation and then left alone, so edits made there survive.
func prepareInstance(config model.Config) (string, error) {
	base, err := instanceDir(config)
	if err != nil {
		return "", err
	}

	if config.IsolatedBase {
		confDir := filepath.Join(base, "conf")
		if _, statErr := os.Stat(confDir); os.IsNotExist(statErr) {
			src := filepath.Join(config.TomEEPath, "conf")
			if err := os.CopyFS(confDir, os.DirFS(src)); err != nil {
				return "", fmt.Errorf("failed to seed %s from %s: %w", confDir, src, err)
			}
		}
		// Tomcat does not create these itself and fails to start without them.
		for _, dir := range []string{"logs", "temp", "work", "webapps"} {
			if err := os.MkdirAll(filepath.Join(base, dir), 0o755); err != nil {
				return "", err
			}
		}
	}

	// Context descriptors live here in both modes.
	if err := os.MkdirAll(contextDir(base), 0o755); err != nil {
		return "", err
	}
	return base, nil
}

// ResetInstance deletes the isolated instance directory so the next start seeds
// a fresh copy of the installation's conf/. No-op when isolation is off.
func (s *TomEEService) ResetInstance() error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	if !config.IsolatedBase {
		return fmt.Errorf("isolated instance is not enabled")
	}
	if s.IsRunning() {
		return fmt.Errorf("stop TomEE before resetting the instance directory")
	}
	base, err := instanceDir(config)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(base); err != nil {
		return err
	}
	_, err = prepareInstance(config)
	return err
}

// InstanceDir exposes the effective CATALINA_BASE to the UI.
func (s *TomEEService) InstanceDir() (string, error) {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return "", err
	}
	return instanceDir(config)
}

func webappsDir(base string) string { return filepath.Join(base, "webapps") }

func contextDir(base string) string {
	return filepath.Join(base, "conf", "Catalina", "localhost")
}

// contextName maps a deployment name to the descriptor file name Tomcat looks
// for: "logistico.war" -> "logistico", "" -> "ROOT". Nested contexts use
// Tomcat's own "a#b.war" spelling, which needs no translation here.
func contextName(destName string) string {
	name := strings.TrimSpace(destName)
	if len(name) >= 4 && strings.EqualFold(name[len(name)-4:], ".war") {
		name = name[:len(name)-4]
	}
	if name == "" || strings.EqualFold(name, "ROOT") {
		return "ROOT"
	}
	return name
}

func contextFile(base, destName string) string {
	return filepath.Join(contextDir(base), contextName(destName)+".xml")
}

// contextDescriptor renders the descriptor that points a context at a docBase
// outside webapps/, which is how the build output gets served without being
// copied anywhere.
func contextDescriptor(docBase string) ([]byte, error) {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	ctx := doc.CreateElement("Context")
	ctx.CreateAttr("docBase", docBase)
	ctx.CreateAttr("reloadable", "false")
	doc.Indent(2)
	return doc.WriteToBytes()
}

// writeContextDescriptor writes the descriptor unless the file already says
// exactly this.
//
// Skipping the identical write is what makes re-deploying while the server runs
// work at all: on Windows Tomcat keeps the descriptor open, so rewriting it
// fails with a sharing violation even when nothing would change.
func writeContextDescriptor(path, docBase string) error {
	want, err := contextDescriptor(docBase)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, want) {
		return nil
	}
	return os.WriteFile(path, want, 0o644)
}

// removeRetryWindow covers the moment right after a shutdown, while the dying
// JVM still holds its files open on Windows.
const removeRetryWindow = 3 * time.Second

// removeDeployed deletes a deployed artifact, retrying briefly because a server
// that has just been told to stop keeps its files locked for a moment.
func removeDeployed(path string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	deadline := time.Now().Add(removeRetryWindow)
	for {
		err := os.RemoveAll(path)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("failed to remove %s (stop TomEE first: it keeps deployed files open while running): %w", path, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// findExplodedInTarget returns <projectDir>/target/<dir> for the first
// directory holding a WEB-INF — the exploded webapp Maven writes next to the
// packaged .war.
func findExplodedInTarget(projectDir string) (string, error) {
	targetDir := filepath.Join(projectDir, "target")
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return "", fmt.Errorf("cannot read target directory: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(targetDir, e.Name())
		if _, err := os.Stat(filepath.Join(candidate, "WEB-INF")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no exploded webapp (a directory containing WEB-INF) found in %s", targetDir)
}
