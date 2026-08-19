package service

import (
	"database/sql"
	"path/filepath"
	"testing"
	"tomee-manager/backend/model"
)

func openTempStorage(t *testing.T) *StorageService {
	t.Helper()
	s := NewStorageService()
	if err := s.open(filepath.Join(t.TempDir(), "data.db")); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	return s
}

func TestConfigRoundTrip(t *testing.T) {
	s := openTempStorage(t)

	want := model.Config{
		TomEEPath: `C:\tomee`, JavaHome: `C:\jdk8`,
		HTTPPort: 8080, DebugPort: 8000, ShutdownPort: 8005,
		VMOptions: "-Xmx2g -Dfoo=bar", OpenBrowser: true, IsolatedBase: true,
	}
	if err := s.SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != want {
		t.Errorf("round trip lost data:\n got %+v\nwant %+v", got, want)
	}
}

func TestWarDefaultsToCopyMode(t *testing.T) {
	s := openTempStorage(t)

	// DeployMode left empty, as an older UI would send it.
	if err := s.SaveWar(model.WarArtifact{SourcePath: `C:\proj`, DestName: "app.war", Enabled: true}); err != nil {
		t.Fatalf("SaveWar: %v", err)
	}
	wars, err := s.ListWars()
	if err != nil {
		t.Fatalf("ListWars: %v", err)
	}
	if len(wars) != 1 {
		t.Fatalf("got %d wars, want 1", len(wars))
	}
	if wars[0].DeployMode != model.DeployCopy {
		t.Errorf("DeployMode = %q, want %q", wars[0].DeployMode, model.DeployCopy)
	}

	updated := wars[0]
	updated.DeployMode = model.DeployExploded
	if err := s.SaveWar(updated); err != nil {
		t.Fatalf("SaveWar update: %v", err)
	}
	got, err := s.GetWar(updated.ID)
	if err != nil {
		t.Fatalf("GetWar: %v", err)
	}
	if got.DeployMode != model.DeployExploded {
		t.Errorf("DeployMode = %q, want %q", got.DeployMode, model.DeployExploded)
	}
}

// The user's database predates every column added since; opening it must
// migrate in place rather than fail or lose the existing rows.
func TestMigratesLegacySchema(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	for _, q := range []string{
		`CREATE TABLE config (id INTEGER PRIMARY KEY CHECK (id = 1), tomee_path TEXT, http_port INTEGER, debug_port INTEGER, shutdown_port INTEGER)`,
		`CREATE TABLE wars (id INTEGER PRIMARY KEY AUTOINCREMENT, source_path TEXT, dest_name TEXT, enabled BOOLEAN)`,
		`INSERT INTO config VALUES (1, 'C:\tomee', 8080, 8001, 8005)`,
		`INSERT INTO wars (source_path, dest_name, enabled) VALUES ('C:\proj', 'logistico.war', 1)`,
	} {
		if _, err := legacy.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	s := NewStorageService()
	if err := s.open(dbFile); err != nil {
		t.Fatalf("open migrated: %v", err)
	}
	defer func() { _ = s.db.Close() }()

	config, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after migration: %v", err)
	}
	if config.TomEEPath != `C:\tomee` || config.DebugPort != 8001 {
		t.Errorf("migration lost config: %+v", config)
	}
	if config.VMOptions != "" || config.OpenBrowser || config.IsolatedBase {
		t.Errorf("new columns should default to empty/false, got %+v", config)
	}

	wars, err := s.ListWars()
	if err != nil {
		t.Fatalf("ListWars after migration: %v", err)
	}
	if len(wars) != 1 || wars[0].DestName != "logistico.war" {
		t.Fatalf("migration lost wars: %+v", wars)
	}
	if wars[0].DeployMode != model.DeployCopy {
		t.Errorf("existing war should default to copy mode, got %q", wars[0].DeployMode)
	}
}
