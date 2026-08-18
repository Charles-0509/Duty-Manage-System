package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsBackendEnvFromProjectRoot(t *testing.T) {
	for _, key := range []string{"APP_PORT", "CONTROL_DATABASE_PATH", "SEMESTER_DATABASE_DIR", "JWT_SECRET"} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	backendDir := filepath.Join(root, "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backendDir, ".env.example"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backendDir, ".env"), []byte("APP_PORT=3456\nCONTROL_DATABASE_PATH=../data/control.db\nSEMESTER_DATABASE_DIR=../data/semesters\nJWT_SECRET=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "3456" || cfg.ControlDatabasePath != "../data/control.db" || cfg.SemesterDatabaseDir != "../data/semesters" || cfg.JWTSecret != "from-file" {
		t.Fatalf("backend .env was not loaded: %+v", cfg)
	}
}
