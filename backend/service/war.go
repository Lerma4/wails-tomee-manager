package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type WarService struct {
	configService *StorageService
}

func NewWarService(storage *StorageService) *WarService {
	return &WarService{
		configService: storage,
	}
}

func (s *WarService) DeployAll() error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	if config.TomEEPath == "" {
		return fmt.Errorf("tomee path not configured")
	}

	wars, err := s.configService.ListWars()
	if err != nil {
		return err
	}

	webappsDir := filepath.Join(config.TomEEPath, "webapps")

	for _, war := range wars {
		if !war.Enabled {
			continue
		}

		warFile, err := findWarInTarget(war.SourcePath)
		if err != nil {
			return fmt.Errorf("WAR not found for project %s: %w", war.SourcePath, err)
		}

		destPath := filepath.Join(webappsDir, war.DestName)

		if err := copyFile(warFile, destPath); err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", warFile, destPath, err)
		}
	}

	return nil
}

func (s *WarService) DeploySingle(warId int) error {
	config, err := s.configService.LoadConfig()
	if err != nil {
		return err
	}
	if config.TomEEPath == "" {
		return fmt.Errorf("tomee path not configured")
	}

	war, err := s.configService.GetWar(warId)
	if err != nil {
		return fmt.Errorf("WAR artifact with id %d not found: %w", warId, err)
	}

	warFile, err := findWarInTarget(war.SourcePath)
	if err != nil {
		return fmt.Errorf("WAR not found for project %s: %w", war.SourcePath, err)
	}

	destPath := filepath.Join(config.TomEEPath, "webapps", war.DestName)
	if err := copyFile(warFile, destPath); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", warFile, destPath, err)
	}

	return nil
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
