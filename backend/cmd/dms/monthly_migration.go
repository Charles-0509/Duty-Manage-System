package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"personnel-management-go/internal/store"
)

func runMonthlyMigration(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("migrate-monthly-reports 不接受额外参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	connection, err := net.DialTimeout("tcp", "127.0.0.1:"+app.appPort(), time.Second)
	if err == nil {
		connection.Close()
		return fmt.Errorf("端口%s仍有服务运行，请先停止DMS再迁移", app.appPort())
	}
	semesterDir, err := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters"))
	if err != nil {
		return err
	}
	databases, err := listSemesterDatabases(semesterDir)
	if err != nil {
		return err
	}
	needsMigration := false
	for _, path := range databases {
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			return err
		}
		var version int
		err = db.QueryRow(`PRAGMA user_version`).Scan(&version)
		db.Close()
		if err != nil {
			return err
		}
		if version != 4 && version != 5 {
			return fmt.Errorf("%s版本%d不在本次迁移范围", path, version)
		}
		needsMigration = needsMigration || version == 4
	}
	if !needsMigration {
		fmt.Println("全部学期库已是版本5，无需再次迁移")
		return nil
	}
	if err = os.MkdirAll(filepath.Join(app.root, "outputs"), 0700); err != nil {
		return err
	}
	backupRoot, err := os.MkdirTemp(filepath.Join(app.root, "outputs"), "monthly-migration-backup-")
	if err != nil {
		return err
	}
	if err = performBackup(app, backupOptions{outDir: backupRoot, noGit: true}); err != nil {
		return err
	}
	templateDir, err := app.resolvePath(app.envValue("WORK_STUDY_TEMPLATE_DIR", "../data/work-study/templates"))
	if err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(templateDir, "勤工助学学生工作记录表模板.docx"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	staging, err := os.MkdirTemp(semesterDir, ".monthly-migrate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	results := []store.MonthlyMigrationResult{}
	for _, source := range databases {
		target := filepath.Join(staging, filepath.Base(source))
		if err = backupSQLite(source, target); err != nil {
			return err
		}
		result, err := store.MigrateMonthlyReports(target, template)
		if err != nil {
			return fmt.Errorf("迁移未安装，原数据保持原样；备份%s：%w", backupRoot, err)
		}
		result.Database = source
		results = append(results, result)
	}
	// Every database was converted and checked before any production file changes.
	installed := []string{}
	for _, source := range databases {
		if err = os.Rename(filepath.Join(staging, filepath.Base(source)), source); err != nil {
			for _, original := range installed {
				if restoreErr := copyFile(filepath.Join(backupRoot, "latest", "semesters", filepath.Base(original)), original); restoreErr != nil {
					fmt.Fprintf(os.Stderr, "恢复%s失败：%v\n", original, restoreErr)
				}
			}
			return fmt.Errorf("安装迁移库失败，备份%s：%w", backupRoot, err)
		}
		installed = append(installed, source)
		removeSQLiteSidecars(source)
	}
	controlPath, err := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db"))
	if err != nil {
		return err
	}
	control, err := sql.Open("sqlite", controlPath)
	if err != nil {
		return err
	}
	_, err = control.Exec(`UPDATE system_state SET context_version=context_version+1 WHERE id=1`)
	control.Close()
	if err != nil {
		return err
	}
	report, _ := json.MarshalIndent(struct {
		Backup    string                         `json:"backup"`
		Databases []store.MonthlyMigrationResult `json:"databases"`
	}{backupRoot, results}, "", "  ")
	if err = os.WriteFile(filepath.Join(backupRoot, "migration-report.json"), report, 0600); err != nil {
		return err
	}
	fmt.Printf("迁移完成。回滚备份：%s\n", backupRoot)
	for _, result := range results {
		fmt.Printf("%s：财务%d，历史%d，冻结记录表%d，跳过=%v\n", filepath.Base(result.Database), result.FinanceCount, result.LaborCount, result.GeneratedRecords, result.Skipped)
		for _, warning := range result.Warnings {
			fmt.Printf("  历史资料提示：%s\n", warning)
		}
	}
	return nil
}
