package service

import (
	"database/sql"
	"fmt"
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
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dbPath, err)
	}
	return s.open(filepath.Join(dbPath, "data.db"))
}

// open connects to a database file and brings its schema up to date. Split out
// of Init so tests can drive it against a temporary file.
func (s *StorageService) open(dbFile string) error {
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

	// Migrations: ALTER TABLE errors out when the column is already there, which
	// is the normal case on every run after the first.
	for _, migration := range []string{
		`ALTER TABLE config ADD COLUMN java_home TEXT DEFAULT ''`,
		`ALTER TABLE config ADD COLUMN vm_options TEXT DEFAULT ''`,
		`ALTER TABLE config ADD COLUMN open_browser BOOLEAN DEFAULT 0`,
		`ALTER TABLE config ADD COLUMN isolated_base BOOLEAN DEFAULT 0`,
		`ALTER TABLE wars ADD COLUMN deploy_mode TEXT DEFAULT 'copy'`,
		`ALTER TABLE wars ADD COLUMN deployed_as TEXT DEFAULT ''`,
	} {
		_, _ = s.db.Exec(migration)
	}

	if err := s.backfillDeployedAs(); err != nil {
		return err
	}

	// Init default config if not exists
	_, err := s.db.Exec(`INSERT OR IGNORE INTO config (id, tomee_path, java_home, http_port, debug_port, shutdown_port) VALUES (1, '', '', 8080, 8000, 8005)`)
	return err
}

// backfillDeployedAs assumes an artifact configured before this column existed
// is deployed under the context its name implies. Without it, changing the
// context path of an existing artifact would leave the old one behind, because
// nothing recorded where it went.
//
// Guessing wrong is harmless: removing a deployment that is not there is a
// no-op.
func (s *StorageService) backfillDeployedAs() error {
	rows, err := s.db.Query(`SELECT id, dest_name FROM wars WHERE COALESCE(deployed_as, '') = ''`)
	if err != nil {
		return err
	}
	pending := map[int]string{}
	for rows.Next() {
		var id int
		var destName string
		if err := rows.Scan(&id, &destName); err != nil {
			rows.Close()
			return err
		}
		pending[id] = contextName(destName)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for id, context := range pending {
		if _, err := s.db.Exec(`UPDATE wars SET deployed_as=? WHERE id=?`, context, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *StorageService) SaveConfig(config model.Config) error {
	_, err := s.db.Exec(
		`UPDATE config SET tomee_path=?, java_home=?, http_port=?, debug_port=?, shutdown_port=?, vm_options=?, open_browser=?, isolated_base=? WHERE id=1`,
		config.TomEEPath, config.JavaHome, config.HTTPPort, config.DebugPort, config.ShutdownPort,
		config.VMOptions, config.OpenBrowser, config.IsolatedBase)
	return err
}

func (s *StorageService) LoadConfig() (model.Config, error) {
	var config model.Config
	row := s.db.QueryRow(`SELECT tomee_path, java_home, http_port, debug_port, shutdown_port, vm_options, open_browser, isolated_base FROM config WHERE id=1`)
	err := row.Scan(&config.TomEEPath, &config.JavaHome, &config.HTTPPort, &config.DebugPort, &config.ShutdownPort,
		&config.VMOptions, &config.OpenBrowser, &config.IsolatedBase)
	return config, err
}

func (s *StorageService) SaveWar(war model.WarArtifact) error {
	if war.DeployMode == "" {
		war.DeployMode = model.DeployCopy
	}
	if war.ID == 0 {
		_, err := s.db.Exec(`INSERT INTO wars (source_path, dest_name, enabled, deploy_mode, deployed_as) VALUES (?, ?, ?, ?, ?)`,
			war.SourcePath, war.DestName, war.Enabled, war.DeployMode, war.DeployedAs)
		return err
	}
	_, err := s.db.Exec(`UPDATE wars SET source_path=?, dest_name=?, enabled=?, deploy_mode=?, deployed_as=? WHERE id=?`,
		war.SourcePath, war.DestName, war.Enabled, war.DeployMode, war.DeployedAs, war.ID)
	return err
}

func (s *StorageService) DeleteWar(id int) error {
	_, err := s.db.Exec(`DELETE FROM wars WHERE id=?`, id)
	return err
}

func (s *StorageService) GetWar(id int) (model.WarArtifact, error) {
	var war model.WarArtifact
	row := s.db.QueryRow(`SELECT id, source_path, dest_name, enabled, COALESCE(deploy_mode, 'copy'), COALESCE(deployed_as, '') FROM wars WHERE id=?`, id)
	err := row.Scan(&war.ID, &war.SourcePath, &war.DestName, &war.Enabled, &war.DeployMode, &war.DeployedAs)
	return war, err
}

func (s *StorageService) ListWars() ([]model.WarArtifact, error) {
	rows, err := s.db.Query(`SELECT id, source_path, dest_name, enabled, COALESCE(deploy_mode, 'copy'), COALESCE(deployed_as, '') FROM wars ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wars []model.WarArtifact
	for rows.Next() {
		var war model.WarArtifact
		if err := rows.Scan(&war.ID, &war.SourcePath, &war.DestName, &war.Enabled, &war.DeployMode, &war.DeployedAs); err != nil {
			return nil, err
		}
		wars = append(wars, war)
	}
	return wars, rows.Err()
}
