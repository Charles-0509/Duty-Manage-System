package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLaborSeedFromEnvFile(t *testing.T) {
	t.Setenv("SEED", "")

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("APP_PORT=3000\nSEED=569\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	seed := loadLaborSeed(envPath)
	if seed == nil || *seed != 569 {
		t.Fatalf("loadLaborSeed() = %v, want 569", seed)
	}
}

func TestLoadLaborSeedPrefersEnvironment(t *testing.T) {
	t.Setenv("SEED", "42")

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("SEED=569\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	seed := loadLaborSeed(envPath)
	if seed == nil || *seed != 42 {
		t.Fatalf("loadLaborSeed() = %v, want 42", seed)
	}
}

func TestLoadReadsBackendEnvFromProjectRoot(t *testing.T) {
	for _, key := range []string{"APP_PORT", "DATABASE_PATH", "PRIVATE_MEMBERS_PATH", "JWT_SECRET", "FIRST_MONDAY", "SEED"} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	backendDir := filepath.Join(root, "backend")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backendDir, ".env.example"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backendDir, ".env"), []byte("APP_PORT=3456\nDATABASE_PATH=../data/legacy.db\nPRIVATE_MEMBERS_PATH=../data/member.json\nJWT_SECRET=from-file\nFIRST_MONDAY=20260907\nSEED=569\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "member.json"), []byte(`{"members":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "3456" || cfg.DatabasePath != "../data/legacy.db" || cfg.JWTSecret != "from-file" || cfg.FirstMonday != "20260907" {
		t.Fatalf("backend .env was not loaded: %+v", cfg)
	}
	if cfg.PrivateMembersPath != filepath.Join(dataDir, "member.json") {
		t.Fatalf("member path was not resolved from backend .env: %s", cfg.PrivateMembersPath)
	}
	if cfg.LaborSeed == nil || *cfg.LaborSeed != 569 {
		t.Fatalf("labor seed was not loaded from backend .env: %v", cfg.LaborSeed)
	}
}
