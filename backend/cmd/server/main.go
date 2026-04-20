package main

import (
	"log"

	"personnel-management-go/internal/config"
	api "personnel-management-go/internal/http"
	"personnel-management-go/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	appStore, err := store.New(cfg)
	if err != nil {
		log.Fatalf("failed to initialize store: %v", err)
	}
	defer appStore.Close()

	router := api.NewRouter(cfg, appStore)

	log.Printf("backend listening on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
