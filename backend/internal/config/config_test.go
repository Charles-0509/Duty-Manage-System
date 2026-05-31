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
