package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"tomee-manager/backend/model"
)

type WarService struct {
	configService *StorageService
}

func NewWarService(storage *StorageService) *WarService {
	return &WarService{
		configService: storage,
	}
}

// validateContextPath rejects anything that could escape webapps/ or produce a
// file name the platform cannot represent. Slashes are allowed and meaningful:
// "/commerciale" is the ordinary way to write a context path, and "api/v1" is a
// nested one.
func validateContextPath(path string) error {
	name := strings.TrimSpace(path)
	if name == "" {
		return fmt.Errorf("context path is empty")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid context path %q: it must not contain %q", path, "..")
	}
	if strings.ContainsAny(name, `:*?"<>|`) {
		return fmt.Errorf("invalid context path %q: it becomes a file name, so it cannot contain : * ? \" < > |", path)
	}
	return nil
}

// deployment is where a single artifact can end up. Only one of these is in
// place at a time; the others are removed so Tomcat never deploys it twice.
type deployment struct {
	warFile     string // webapps/app.war
	unpackedDir string // webapps/app
	descriptor  string // conf/Catalina/localhost/app.xml
}

func deploymentFor(base, destName string) deployment {
	// Everything is keyed off the context name. Under webapps/ Tomcat derives
	// the context from the file name, so there the two cannot differ; a context
	// descriptor is what lets the artifact keep its own name.
	ctx := contextName(destName)
	return deployment{
		warFile:     filepath.Join(webappsDir(base), ctx+".war"),
		unpackedDir: filepath.Join(webappsDir(base), ctx),
		descriptor:  contextFile(base, destName),
	}
}

// clear removes every trace of the deployment except the ones listed in keep.
func (d deployment) clear(keep ...string) error {
	kept := map[string]bool{}
	for _, k := range keep {
		kept[k] = true
	}
	for _, path := range []string{d.warFile, d.unpackedDir, d.descriptor} {
		if kept[path] {
			continue
		}
		if err := removeDeployed(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *WarService) DeployAll() error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	base, err := prepareInstance(config)
	if err != nil {
		return err
	}

	wars, err := s.configService.ListWars()
	if err != nil {
		return err
	}

	for _, war := range wars {
		if !war.Enabled {
			continue
		}
		if err := s.deployAndRecord(base, war); err != nil {
			return err
		}
	}
	return nil
}

// deployAndRecord deploys and remembers the context it landed on, clearing the
// previous one first when the context path has been changed. Without this a
// rename leaves the old context deployed and the app answers on both URLs.
func (s *WarService) deployAndRecord(base string, war model.WarArtifact) error {
	if err := validateContextPath(war.DestName); err != nil {
		return err
	}
	current := contextName(war.DestName)

	if war.DeployedAs != "" && war.DeployedAs != current {
		if err := deploymentFor(base, war.DeployedAs).clear(); err != nil {
			return fmt.Errorf("failed to remove the previous context %q: %w", war.DeployedAs, err)
		}
	}
	if err := deployWar(base, war); err != nil {
		return err
	}
	if war.DeployedAs != current {
		war.DeployedAs = current
		return s.configService.SaveWar(war)
	}
	return nil
}

func (s *WarService) DeploySingle(warId int) error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	base, err := prepareInstance(config)
	if err != nil {
		return err
	}

	war, err := s.configService.GetWar(warId)
	if err != nil {
		return fmt.Errorf("WAR artifact with id %d not found: %w", warId, err)
	}
	return s.deployAndRecord(base, war)
}

// Undeploy removes the artifact from the server without touching the build
// output or the entry in the list.
func (s *WarService) Undeploy(warId int) error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	base, err := instanceDir(config)
	if err != nil {
		return err
	}
	war, err := s.configService.GetWar(warId)
	if err != nil {
		return fmt.Errorf("WAR artifact with id %d not found: %w", warId, err)
	}
	if err := validateContextPath(war.DestName); err != nil {
		return err
	}
	// The previously deployed context may differ from the one configured now.
	for _, name := range deployedContexts(war) {
		if err := deploymentFor(base, name).clear(); err != nil {
			return err
		}
	}
	if war.DeployedAs != "" {
		war.DeployedAs = ""
		return s.configService.SaveWar(war)
	}
	return nil
}

// deployedContexts lists every context this artifact may currently occupy: the
// configured one, plus the one it was last deployed as if that differs.
func deployedContexts(war model.WarArtifact) []string {
	names := []string{war.DestName}
	if war.DeployedAs != "" && war.DeployedAs != contextName(war.DestName) {
		names = append(names, war.DeployedAs)
	}
	return names
}

// IsDeployed reports whether the artifact is currently present on the server,
// in any of the three shapes.
func (s *WarService) IsDeployed(warId int) (bool, error) {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return false, err
	}
	base, err := instanceDir(config)
	if err != nil {
		return false, err
	}
	war, err := s.configService.GetWar(warId)
	if err != nil {
		return false, err
	}
	if err := validateContextPath(war.DestName); err != nil {
		return false, err
	}
	for _, name := range deployedContexts(war) {
		d := deploymentFor(base, name)
		for _, path := range []string{d.descriptor, d.warFile, d.unpackedDir} {
			if _, err := os.Stat(path); err == nil {
				return true, nil
			}
		}
	}
	return false, nil
}

func deployWar(base string, war model.WarArtifact) error {
	if err := validateContextPath(war.DestName); err != nil {
		return err
	}
	target := deploymentFor(base, war.DestName)

	switch war.DeployMode {
	case model.DeployWar, model.DeployExploded:
		docBase, err := docBaseFor(war)
		if err != nil {
			return err
		}
		// The copied artifacts must go first: leaving them next to the
		// descriptor makes Tomcat deploy the same context twice. The descriptor
		// itself is kept — writeContextDescriptor rewrites it only when its
		// contents would actually change, which is what lets a re-deploy succeed
		// while the server is holding the file open.
		if err := target.clear(target.descriptor); err != nil {
			return err
		}
		if err := writeContextDescriptor(target.descriptor, docBase); err != nil {
			return fmt.Errorf("failed to write context descriptor %s: %w", target.descriptor, err)
		}
		return nil

	default: // model.DeployCopy, and anything unset from an older database
		warFile, err := findWarInTarget(war.SourcePath)
		if err != nil {
			return fmt.Errorf("WAR not found for project %s: %w", war.SourcePath, err)
		}
		if err := target.clear(target.warFile); err != nil {
			return err
		}
		if err := copyFile(warFile, target.warFile); err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", warFile, target.warFile, err)
		}
		return nil
	}
}

// docBaseFor resolves the build output a context descriptor should point at.
func docBaseFor(war model.WarArtifact) (string, error) {
	if war.DeployMode == model.DeployExploded {
		dir, err := findExplodedInTarget(war.SourcePath)
		if err != nil {
			return "", fmt.Errorf("exploded webapp not found for project %s: %w", war.SourcePath, err)
		}
		return dir, nil
	}
	warFile, err := findWarInTarget(war.SourcePath)
	if err != nil {
		return "", fmt.Errorf("WAR not found for project %s: %w", war.SourcePath, err)
	}
	return warFile, nil
}

func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return nil
}
