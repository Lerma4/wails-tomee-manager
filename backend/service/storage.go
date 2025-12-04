package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"tomee-manager/backend/model"

	_ "modernc.org/sqlite"
)

type StorageService struct {
	db *sql.DB
}

func NewStorageService() *StorageService {
	return &StorageService{}
}

func (s *StorageService) Init() error {
	appDataDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dbPath := filepath.Join(appDataDir, "tomee-manager")
	os.MkdirAll(dbPath, 0755)
	dbFile := filepath.Join(dbPath, "data.db")

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		return err
	}
	s.db = db

	return s.createTables()
}

func (s *StorageService) createTables() error {
	queryConfig := `
	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		tomee_path TEXT,
		http_port INTEGER,
		debug_port INTEGER,
		shutdown_port INTEGER
	);`

	queryWars := `
	CREATE TABLE IF NOT EXISTS wars (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_path TEXT,
		dest_name TEXT,
		enabled BOOLEAN
	);`

	if _, err := s.db.Exec(queryConfig); err != nil {
		return err
	}
	if _, err := s.db.Exec(queryWars); err != nil {
		return err
	}

	// Init default config if not exists
	_, err := s.db.Exec(`INSERT OR IGNORE INTO config (id, tomee_path, http_port, debug_port, shutdown_port) VALUES (1, '', 8080, 8000, 8005)`)
	return err
}

func (s *StorageService) SaveConfig(config model.Config) error {
	_, err := s.db.Exec(`UPDATE config SET tomee_path=?, http_port=?, debug_port=?, shutdown_port=? WHERE id=1`,
		config.TomEEPath, config.HTTPPort, config.DebugPort, config.ShutdownPort)
	return err
}

func (s *StorageService) LoadConfig() (model.Config, error) {
	var config model.Config
	row := s.db.QueryRow(`SELECT tomee_path, http_port, debug_port, shutdown_port FROM config WHERE id=1`)
	err := row.Scan(&config.TomEEPath, &config.HTTPPort, &config.DebugPort, &config.ShutdownPort)
	return config, err
}

func (s *StorageService) SaveWar(war model.WarArtifact) error {
	if war.ID == 0 {
		_, err := s.db.Exec(`INSERT INTO wars (source_path, dest_name, enabled) VALUES (?, ?, ?)`,
			war.SourcePath, war.DestName, war.Enabled)
		return err
	} else {
		_, err := s.db.Exec(`UPDATE wars SET source_path=?, dest_name=?, enabled=? WHERE id=?`,
			war.SourcePath, war.DestName, war.Enabled, war.ID)
		return err
	}
}

func (s *StorageService) DeleteWar(id int) error {
	_, err := s.db.Exec(`DELETE FROM wars WHERE id=?`, id)
	return err
}

func (s *StorageService) ListWars() ([]model.WarArtifact, error) {
	rows, err := s.db.Query(`SELECT id, source_path, dest_name, enabled FROM wars`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wars []model.WarArtifact
	for rows.Next() {
		var war model.WarArtifact
		if err := rows.Scan(&war.ID, &war.SourcePath, &war.DestName, &war.Enabled); err != nil {
			return nil, err
		}
		wars = append(wars, war)
	}
	return wars, nil
}
