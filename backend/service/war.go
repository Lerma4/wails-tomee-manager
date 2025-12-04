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

		// Check if source exists
		if _, err := os.Stat(war.SourcePath); os.IsNotExist(err) {
			// Log error but continue? Or fail?
			// For now let's return error to alert user
			return fmt.Errorf("source war not found: %s", war.SourcePath)
		}

		destPath := filepath.Join(webappsDir, war.DestName)

		// Copy file
		if err := copyFile(war.SourcePath, destPath); err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", war.SourcePath, destPath, err)
		}
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
