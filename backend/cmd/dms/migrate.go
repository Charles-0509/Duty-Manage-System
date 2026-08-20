package main

import (
	"fmt"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/store"
)

func runMigrate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("migrate 不接受参数")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	results, err := store.MigrateSchedulePlans(cfg)
	if err != nil {
		return err
	}
	for _, result := range results {
		status := "已验证"
		if result.From != result.To {
			status = "已迁移"
		}
		fmt.Printf("%s: %s schema v%d -> v%d，排班明细 %d 条\n", status, result.Database, result.From, result.To, result.Entries)
	}
	fmt.Printf("迁移完成，共处理 %d 个学期数据库\n", len(results))
	return nil
}
