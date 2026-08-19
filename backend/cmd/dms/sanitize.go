package main

import (
	crand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type sanitizedAccount struct {
	ID            int64
	AccountUUID   string
	OldRealName   string
	Username      string
	RealName      string
	StudentNumber string
	SystemAdmin   bool
}

type sanitizeResult struct {
	OutputDir     string
	AdminUsername string
	Password      string
	AccountCount  int
	SemesterCount int
}

func runSanitize(args []string) error {
	outDir := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-out":
			index++
			if index >= len(args) {
				return fmt.Errorf("-out 需要目录参数")
			}
			outDir = args[index]
		default:
			return fmt.Errorf("sanitize 不接受参数 %q", args[index])
		}
	}
	if strings.TrimSpace(outDir) == "" {
		return fmt.Errorf("用法: dms sanitize -out <目录>")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	result, err := performSanitize(app, expandHome(outDir))
	if err != nil {
		return err
	}
	fmt.Println("脱敏快照完成:")
	fmt.Printf("  输出目录: %s\n", result.OutputDir)
	fmt.Printf("  学期库数: %d\n", result.SemesterCount)
	fmt.Printf("  脱敏账户: %d\n", result.AccountCount)
	fmt.Printf("  管理员账号: %s\n", result.AdminUsername)
	fmt.Printf("  所有账户临时密码: %s\n", result.Password)
	fmt.Println("  全局模板: 未复制")
	return nil
}

func performSanitize(app *appContext, outDir string) (sanitizeResult, error) {
	controlPath, err := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db"))
	if err != nil {
		return sanitizeResult{}, err
	}
	semesterDir, err := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters"))
	if err != nil {
		return sanitizeResult{}, err
	}
	semesterDBs, err := listSemesterDatabases(semesterDir)
	if err != nil || len(semesterDBs) == 0 {
		return sanitizeResult{}, fmt.Errorf("学期数据库目录不可用或没有 .db 文件: %s", semesterDir)
	}
	if _, err := os.Stat(controlPath); err != nil {
		return sanitizeResult{}, fmt.Errorf("控制数据库不存在: %s", controlPath)
	}

	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return sanitizeResult{}, err
	}
	if _, err := os.Stat(outDir); err == nil {
		return sanitizeResult{}, fmt.Errorf("输出目录已存在，请选择新目录: %s", outDir)
	} else if !os.IsNotExist(err) {
		return sanitizeResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outDir), 0o700); err != nil {
		return sanitizeResult{}, err
	}
	if err := os.Mkdir(outDir, 0o700); err != nil {
		return sanitizeResult{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(outDir)
		}
	}()
	outSemesterDir := filepath.Join(outDir, "semesters")
	if err := os.Mkdir(outSemesterDir, 0o700); err != nil {
		return sanitizeResult{}, err
	}

	outControl := filepath.Join(outDir, "control.db")
	if err := backupSQLite(controlPath, outControl); err != nil {
		return sanitizeResult{}, err
	}
	for _, source := range semesterDBs {
		if err := backupSQLite(source, filepath.Join(outSemesterDir, filepath.Base(source))); err != nil {
			return sanitizeResult{}, err
		}
	}

	password, err := randomSanitizedPassword()
	if err != nil {
		return sanitizeResult{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return sanitizeResult{}, err
	}
	accounts, err := sanitizeControlDatabase(outControl, string(passwordHash))
	if err != nil {
		return sanitizeResult{}, err
	}
	aliases := make(map[string]sanitizedAccount, len(accounts))
	adminUsername := "admin"
	for _, account := range accounts {
		aliases[account.AccountUUID] = account
		if account.SystemAdmin {
			adminUsername = account.Username
		}
	}
	for _, source := range semesterDBs {
		target := filepath.Join(outSemesterDir, filepath.Base(source))
		if err := sanitizeSemesterDatabase(target, aliases); err != nil {
			return sanitizeResult{}, fmt.Errorf("脱敏学期库 %s 失败: %w", filepath.Base(target), err)
		}
	}

	manifest := struct {
		CreatedAt         string `json:"createdAt"`
		AccountCount      int    `json:"accountCount"`
		SemesterCount     int    `json:"semesterCount"`
		TemplatesIncluded bool   `json:"templatesIncluded"`
	}{time.Now().Format(time.RFC3339), len(accounts), len(semesterDBs), false}
	manifestContent, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), append(manifestContent, '\n'), 0o600); err != nil {
		return sanitizeResult{}, err
	}
	if err := os.Chmod(outControl, 0o600); err != nil {
		return sanitizeResult{}, err
	}
	for _, source := range semesterDBs {
		if err := os.Chmod(filepath.Join(outSemesterDir, filepath.Base(source)), 0o600); err != nil {
			return sanitizeResult{}, err
		}
	}
	complete = true
	return sanitizeResult{outDir, adminUsername, password, len(accounts), len(semesterDBs)}, nil
}

func randomSanitizedPassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := crand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func sanitizeControlDatabase(path, passwordHash string) ([]sanitizedAccount, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.Query(`
		SELECT id, account_uuid, real_name, is_system_admin
		FROM accounts
		ORDER BY is_system_admin DESC, account_uuid ASC
	`)
	if err != nil {
		return nil, err
	}
	accounts := []sanitizedAccount{}
	memberIndex := 0
	adminIndex := 0
	for rows.Next() {
		var account sanitizedAccount
		var systemAdmin int
		if err := rows.Scan(&account.ID, &account.AccountUUID, &account.OldRealName, &systemAdmin); err != nil {
			rows.Close()
			return nil, err
		}
		account.SystemAdmin = systemAdmin == 1
		if account.SystemAdmin {
			adminIndex++
			account.Username = "admin"
			account.RealName = "系统管理员"
			if adminIndex > 1 {
				account.Username = fmt.Sprintf("admin-%02d", adminIndex)
				account.RealName = fmt.Sprintf("系统管理员%02d", adminIndex)
			}
		} else {
			memberIndex++
			account.Username = fmt.Sprintf("user-%04d", memberIndex)
			account.RealName = fmt.Sprintf("成员%04d", memberIndex)
			account.StudentNumber = fmt.Sprintf("9000%08d", memberIndex)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM refresh_tokens`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM audit_logs`); err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if _, err := tx.Exec(`
			UPDATE accounts
			SET username = ?, real_name = ?, student_number = ''
			WHERE id = ?
		`, fmt.Sprintf("__sanitized_user_%d", account.ID), fmt.Sprintf("__sanitized_name_%d", account.ID), account.ID); err != nil {
			return nil, err
		}
	}
	for _, account := range accounts {
		if _, err := tx.Exec(`
			UPDATE accounts
			SET username = ?, real_name = ?, student_number = ?, password_hash = ?,
				must_change_password = 0, session_version = session_version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, account.Username, account.RealName, account.StudentNumber, passwordHash, account.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := compactSanitizedDatabase(db); err != nil {
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	removeSQLiteSidecars(path)
	return accounts, nil
}

func sanitizeSemesterDatabase(path string, aliases map[string]sanitizedAccount) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.Query(`SELECT id, account_uuid, real_name FROM users ORDER BY id`)
	if err != nil {
		return err
	}
	type localMember struct {
		id      int64
		oldName string
		alias   sanitizedAccount
	}
	members := []localMember{}
	for rows.Next() {
		var member localMember
		var accountUUID string
		if err := rows.Scan(&member.id, &accountUUID, &member.oldName); err != nil {
			rows.Close()
			return err
		}
		alias, ok := aliases[accountUUID]
		if !ok {
			rows.Close()
			return fmt.Errorf("成员 %d 的全局账户不存在", member.id)
		}
		member.alias = alias
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{`DELETE FROM labor_conversion_runs`, `DELETE FROM finance_batches`} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	for _, member := range members {
		for _, statement := range []string{
			`UPDATE availability_entries SET real_name = ? WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`,
			`UPDATE schedule_entries SET real_name = ? WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`,
			`UPDATE final_schedule_entries SET real_name = ? WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`,
			`UPDATE work_sessions SET worker_name = ? WHERE member_id = ? OR (member_id IS NULL AND worker_name = ?)`,
		} {
			if _, err := tx.Exec(statement, member.alias.RealName, member.id, member.oldName); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE final_schedules SET updated_by = ? WHERE updated_by = ?`, member.alias.RealName, member.oldName); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE work_orders SET created_by = ? WHERE created_by = ?`, member.alias.RealName, member.oldName); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			UPDATE users
			SET username = ?, real_name = ?, student_number = ''
			WHERE id = ?
		`, fmt.Sprintf("__sanitized_user_%d", member.id), fmt.Sprintf("__sanitized_name_%d", member.id), member.id); err != nil {
			return err
		}
	}
	for _, member := range members {
		if _, err := tx.Exec(`
			UPDATE users
			SET username = ?, real_name = ?, student_number = ?, password_hash = '', must_change_password = 0,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, member.alias.Username, member.alias.RealName, member.alias.StudentNumber, member.id); err != nil {
			return err
		}
	}
	orderRows, err := tx.Query(`SELECT id FROM work_orders ORDER BY created_time, id`)
	if err != nil {
		return err
	}
	orderIDs := []string{}
	for orderRows.Next() {
		var id string
		if err := orderRows.Scan(&id); err != nil {
			orderRows.Close()
			return err
		}
		orderIDs = append(orderIDs, id)
	}
	if err := orderRows.Err(); err != nil {
		orderRows.Close()
		return err
	}
	orderRows.Close()
	for index, id := range orderIDs {
		if _, err := tx.Exec(`UPDATE work_orders SET title = ? WHERE id = ?`, fmt.Sprintf("脱敏工单%04d", index+1), id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE semester_settings SET work_study_content = '脱敏测试数据'`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := compactSanitizedDatabase(db); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	removeSQLiteSidecars(path)
	return nil
}

func compactSanitizedDatabase(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	if _, err := db.Exec(`VACUUM`); err != nil {
		return err
	}
	_, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func removeSQLiteSidecars(path string) {
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}
