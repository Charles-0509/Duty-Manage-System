package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsBackendEnvFromProjectRoot(t *testing.T) {
	for _, key := range []string{"APP_PORT", "CONTROL_DATABASE_PATH", "SEMESTER_DATABASE_DIR", "JWT_SECRET", "DEFAULT_ADMIN_PASSWORD"} {
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
	if err := os.WriteFile(filepath.Join(backendDir, ".env"), []byte("APP_PORT=3456\nCONTROL_DATABASE_PATH=../data/control.db\nSEMESTER_DATABASE_DIR=../data/semesters\nJWT_SECRET=0123456789abcdef0123456789abcdef\nDEFAULT_ADMIN_PASSWORD=strong-admin-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "3456" || cfg.ControlDatabasePath != "../data/control.db" || cfg.SemesterDatabaseDir != "../data/semesters" || cfg.JWTSecret != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("backend .env was not loaded: %+v", cfg)
	}
}

func TestLoadRejectsInsecureAuthenticationDefaults(t *testing.T) {
	for _, key := range []string{"JWT_SECRET", "DEFAULT_ADMIN_PASSWORD"} {
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
	if err := os.WriteFile(filepath.Join(backendDir, ".env"), []byte("JWT_SECRET=please-change-me\nDEFAULT_ADMIN_PASSWORD=admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	if _, err := Load(); err == nil {
		t.Fatal("insecure authentication defaults were accepted")
	}
}

func TestValidatePassword(t *testing.T) {
	for _, password := range []string{"short", "member-account", "please-change-me"} {
		if err := ValidatePassword("member-account", password); !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("ValidatePassword(%q) error = %v", password, err)
		}
	}
	if err := ValidatePassword("member-account", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("strong password rejected: %v", err)
	}
}
