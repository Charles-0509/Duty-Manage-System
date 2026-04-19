package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/store"
)

func main() {
	cfg := config.Load()

	sourceURL := strings.TrimSpace(os.Getenv("SYNC_SOURCE_URL"))
	if sourceURL == "" {
		log.Fatal("SYNC_SOURCE_URL is required")
	}

	syncToken := strings.TrimSpace(os.Getenv("SYNC_TOKEN"))
	if syncToken == "" {
		log.Fatal("SYNC_TOKEN is required")
	}

	timeout := 2 * time.Minute
	client := &http.Client{Timeout: timeout}

	request, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		log.Fatalf("failed to create sync request: %v", err)
	}
	request.Header.Set("X-Sync-Token", syncToken)

	response, err := client.Do(request)
	if err != nil {
		log.Fatalf("failed to download snapshot: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		log.Fatalf("snapshot download failed: %s %s", response.Status, strings.TrimSpace(string(body)))
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		log.Fatalf("failed to create database directory: %v", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(cfg.DatabasePath), "personnel-sync-*.db")
	if err != nil {
		log.Fatalf("failed to create temporary snapshot file: %v", err)
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, response.Body); err != nil {
		tempFile.Close()
		log.Fatalf("failed to save snapshot: %v", err)
	}

	if err := tempFile.Close(); err != nil {
		log.Fatalf("failed to close temporary snapshot file: %v", err)
	}

	appStore, err := store.New(cfg)
	if err != nil {
		log.Fatalf("failed to open local database: %v", err)
	}
	defer appStore.Close()

	if err := appStore.ImportSnapshot(tempPath); err != nil {
		log.Fatalf("failed to import snapshot into %s: %v", cfg.DatabasePath, err)
	}

	log.Printf("database sync completed: %s <= %s", cfg.DatabasePath, sourceURL)
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[dbsync] ")
}
