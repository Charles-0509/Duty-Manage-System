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

	targetURL := strings.TrimSpace(os.Getenv("SYNC_TARGET_URL"))
	if targetURL == "" {
		log.Fatal("SYNC_TARGET_URL is required")
	}

	syncToken := strings.TrimSpace(os.Getenv("SYNC_TOKEN"))
	if syncToken == "" {
		log.Fatal("SYNC_TOKEN is required")
	}

	appStore, err := store.New(cfg)
	if err != nil {
		log.Fatalf("failed to open local database: %v", err)
	}
	defer appStore.Close()

	tempFile, err := os.CreateTemp(filepath.Dir(cfg.DatabasePath), "personnel-push-*.db")
	if err != nil {
		log.Fatalf("failed to create temporary snapshot file: %v", err)
	}

	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	if err := appStore.CreateSnapshot(tempPath); err != nil {
		log.Fatalf("failed to create local snapshot: %v", err)
	}

	file, err := os.Open(tempPath)
	if err != nil {
		log.Fatalf("failed to open snapshot file: %v", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatalf("failed to read snapshot file info: %v", err)
	}

	timeout := 2 * time.Minute
	client := &http.Client{Timeout: timeout}

	request, err := http.NewRequest(http.MethodPost, targetURL, file)
	if err != nil {
		log.Fatalf("failed to create push request: %v", err)
	}
	request.Header.Set("X-Sync-Token", syncToken)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.ContentLength = fileInfo.Size()

	response, err := client.Do(request)
	if err != nil {
		log.Fatalf("failed to push snapshot: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		log.Fatalf("snapshot push failed: %s %s", response.Status, strings.TrimSpace(string(body)))
	}

	log.Printf("database push completed: %s => %s", cfg.DatabasePath, targetURL)
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[dbpush] ")
}
