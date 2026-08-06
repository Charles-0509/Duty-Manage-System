package main

import (
	"fmt"
	"log"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load migration config: %v", err)
	}
	appStore, err := store.New(cfg)
	if err != nil {
		log.Fatalf("semester migration failed: %v", err)
	}
	defer appStore.Close()

	active := appStore.ActiveSemester()
	semesters, err := appStore.ListSemesters()
	if err != nil {
		log.Fatalf("failed to verify migrated semesters: %v", err)
	}
	fmt.Printf("Semester migration ready: active=%s id=%s total=%d\n", active.Name, active.ID, len(semesters))
}
