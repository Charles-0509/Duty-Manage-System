package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"personnel-management-go/internal/config"

	"github.com/google/uuid"
)

type SchedulePlanMigrationResult struct {
	Database string
	From     int
	To       int
	Entries  int
}

func MigrateSchedulePlans(cfg config.AppConfig) ([]SchedulePlanMigrationResult, error) {
	var err error
	cfg.ControlDatabasePath, err = resolveConfigPath(cfg, cfg.ControlDatabasePath)
	if err != nil {
		return nil, err
	}
	cfg.SemesterDatabaseDir, err = resolveConfigPath(cfg, cfg.SemesterDatabaseDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cfg.ControlDatabasePath); err != nil {
		return nil, fmt.Errorf("控制数据库不存在: %w", err)
	}
	control, err := sql.Open("sqlite", cfg.ControlDatabasePath)
	if err != nil {
		return nil, err
	}
	defer control.Close()
	if err := configureSQLite(control); err != nil {
		return nil, err
	}

	rows, err := control.Query(`SELECT database_filename FROM semesters ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	filenames := make([]string, 0)
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			rows.Close()
			return nil, err
		}
		filenames = append(filenames, filename)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	results := make([]SchedulePlanMigrationResult, 0, len(filenames))
	for _, filename := range filenames {
		path := filepath.Join(cfg.SemesterDatabaseDir, filename)
		result, err := migrateSchedulePlanDatabase(path)
		if err != nil {
			return results, fmt.Errorf("迁移 %s 失败: %w", filename, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func migrateSchedulePlanDatabase(path string) (SchedulePlanMigrationResult, error) {
	result := SchedulePlanMigrationResult{Database: path, To: semesterSchemaVersion}
	if _, err := os.Stat(path); err != nil {
		return result, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return result, err
	}
	defer db.Close()
	if err := configureSQLite(db); err != nil {
		return result, err
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&result.From); err != nil {
		return result, err
	}
	if result.From == semesterSchemaVersion {
		entries, err := verifySchedulePlanDatabase(db)
		result.Entries = entries
		return result, err
	}
	if result.From != 3 {
		return result, fmt.Errorf("不支持从 schema v%d 迁移", result.From)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_entries`).Scan(&result.Entries); err != nil {
		return result, err
	}
	tx, err := db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	planID := uuid.NewString()
	statements := []string{
		`CREATE TABLE schedule_plans (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			is_published INTEGER NOT NULL DEFAULT 0 CHECK (is_published IN (0, 1)),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE schedule_entries_v4 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			schedule_plan_id TEXT NOT NULL,
			shift_code TEXT NOT NULL,
			real_name TEXT NOT NULL,
			member_id INTEGER,
			week_type TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(schedule_plan_id, shift_code, real_name, week_type),
			FOREIGN KEY (schedule_plan_id) REFERENCES schedule_plans(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO schedule_plans (id, name, is_published) VALUES (?, '默认排班表', 1)`, planID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(`
		INSERT INTO schedule_entries_v4 (id, schedule_plan_id, shift_code, real_name, member_id, week_type, created_at)
		SELECT id, ?, shift_code, real_name, member_id, week_type, created_at FROM schedule_entries
	`, planID); err != nil {
		return result, err
	}
	for _, statement := range []string{
		`DROP TABLE schedule_entries`,
		`ALTER TABLE schedule_entries_v4 RENAME TO schedule_entries`,
		`CREATE INDEX idx_schedule_entries_plan_shift ON schedule_entries(schedule_plan_id, shift_code)`,
		`CREATE UNIQUE INDEX idx_schedule_plans_one_published ON schedule_plans(is_published) WHERE is_published = 1`,
		`UPDATE semester_settings SET schema_version = 4 WHERE id = 1`,
		`PRAGMA user_version = 4`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return result, err
		}
	}
	var copied int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM schedule_entries`).Scan(&copied); err != nil {
		return result, err
	}
	if copied != result.Entries {
		return result, fmt.Errorf("排班明细数量不一致：迁移前 %d，迁移后 %d", result.Entries, copied)
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	_, err = verifySchedulePlanDatabase(db)
	return result, err
}

func verifySchedulePlanDatabase(db *sql.DB) (int, error) {
	var userVersion, settingsVersion, entries, published int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return 0, err
	}
	if err := db.QueryRow(`SELECT schema_version FROM semester_settings WHERE id = 1`).Scan(&settingsVersion); err != nil {
		return 0, err
	}
	if userVersion != semesterSchemaVersion || settingsVersion != semesterSchemaVersion {
		return 0, fmt.Errorf("schema 版本不一致：user_version=%d settings=%d", userVersion, settingsVersion)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_entries`).Scan(&entries); err != nil {
		return 0, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_plans WHERE is_published = 1`).Scan(&published); err != nil {
		return 0, err
	}
	if published > 1 {
		return 0, fmt.Errorf("存在多张已发布排班表")
	}
	foreignKeys, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return 0, err
	}
	hasForeignKeyError := foreignKeys.Next()
	foreignKeys.Close()
	if hasForeignKeyError {
		return 0, fmt.Errorf("外键检查失败")
	}
	var quickCheck string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&quickCheck); err != nil || quickCheck != "ok" {
		return 0, fmt.Errorf("数据库完整性检查失败")
	}
	return entries, nil
}
