package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func TestPerformSanitizeProducesConsistentPrivateDataFreeSnapshot(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	cfg := config.AppConfig{
		ControlDatabasePath:  filepath.Join(dataDir, "control.db"),
		SemesterDatabaseDir:  filepath.Join(dataDir, "semesters"),
		JWTSecret:            "0123456789abcdef0123456789abcdef",
		AdminPassword:        "strong-admin-password",
		FirstMonday:          "20260302",
		WorkStudyTemplateDir: filepath.Join(dataDir, "templates"),
		WorkStudyContent:     "真实机房位置",
	}
	appStore, err := store.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "private-user", RealName: "真实姓名", StudentNumber: "202600009999",
		Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.SaveSchedule(map[string][]string{"Mon-1": {"真实姓名(单双)"}}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.SaveFinalSchedule(1, types.SaveFinalScheduleRequest{
		SelectedDate: "2026-03-02", Schedule: map[string][]string{"Mon-1": {"真实姓名"}},
	}, "系统管理员"); err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.CreateWorkOrder(types.SaveWorkOrderRequest{
		Title: "包含隐私的工单标题", BelongingMonth: "2026-04",
		WorkSessions: []types.WorkSession{{Date: "2026-04-01", WorkerName: "真实姓名", Duration: 2}},
	}, "真实姓名"); err != nil {
		t.Fatal(err)
	}
	if err := appStore.Close(); err != nil {
		t.Fatal(err)
	}

	control, err := sql.Open("sqlite", cfg.ControlDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Exec(`INSERT INTO refresh_tokens (account_id, token_hash, expires_at) VALUES (1, 'private-token-hash', '2099-01-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Exec(`INSERT INTO audit_logs (username, real_name, action, ip) VALUES ('private-user', '真实姓名', '敏感操作', '198.51.100.10')`); err != nil {
		t.Fatal(err)
	}
	control.Close()

	semesterDBs, err := listSemesterDatabases(cfg.SemesterDatabaseDir)
	if err != nil || len(semesterDBs) != 1 {
		t.Fatalf("semester databases=%v err=%v", semesterDBs, err)
	}
	semester, err := sql.Open("sqlite", semesterDBs[0])
	if err != nil {
		t.Fatal(err)
	}
	secretBlob := []byte("UNIQUE_PRIVATE_FINANCE_BLOB")
	if _, err := semester.Exec(`
		INSERT INTO finance_batches
			(id, created_at, start_date, end_date, output_month, work_order_ids_json, include_management,
			 management_months, excel_filename, csv_filename, excel_blob, csv_blob, excel_sha256, csv_sha256)
		VALUES ('private-batch', '2026-04-01', '2026-04-01', '2026-04-30', '2026-04', '[]', 0,
			0, 'private.xlsx', 'private.csv', ?, ?, 'hash', 'hash')
	`, secretBlob, secretBlob); err != nil {
		t.Fatal(err)
	}
	if _, err := semester.Exec(`
		INSERT INTO labor_conversion_runs
			(id, created_at, input_filename, output_name, target_total_cents, original_total_cents,
			 final_total_cents, team_fund_cents, seed, people_json, result_json, workbook_blob)
		VALUES ('private-run', '2026-04-01', 'private.xlsx', 'private-result.xlsx', 100, 100,
			100, 0, 1, '[{"Name":"真实姓名","StudentNumber":"202600009999"}]', '{}', ?)
	`, secretBlob); err != nil {
		t.Fatal(err)
	}
	semester.Close()

	templateDir := cfg.WorkStudyTemplateDir
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "private-template.docx"), []byte("private template"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &appContext{root: dir, backendDir: dir, env: map[string]string{
		"CONTROL_DATABASE_PATH": cfg.ControlDatabasePath,
		"SEMESTER_DATABASE_DIR": cfg.SemesterDatabaseDir,
	}}
	outDir := filepath.Join(dir, "sanitized")
	result, err := performSanitize(app, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountCount != 2 || result.SemesterCount != 1 || result.AdminUsername != "admin" {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(outDir, "work-study")); !os.IsNotExist(err) {
		t.Fatal("global template directory was copied")
	}

	sanitizedControl, err := sql.Open("sqlite", filepath.Join(outDir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	var username, realName, studentNumber, passwordHash string
	if err := sanitizedControl.QueryRow(`SELECT username, real_name, student_number, password_hash FROM accounts WHERE is_system_admin = 0`).Scan(&username, &realName, &studentNumber, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if username != "user-0001" || realName != "成员0001" || studentNumber != "900000000001" {
		t.Fatalf("sanitized account=%q %q %q", username, realName, studentNumber)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(result.Password)); err != nil {
		t.Fatalf("generated password does not match sanitized account: %v", err)
	}
	for _, table := range []string{"refresh_tokens", "audit_logs"} {
		var count int
		if err := sanitizedControl.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	sanitizedControl.Close()

	sanitizedSemesterPath := filepath.Join(outDir, "semesters", filepath.Base(semesterDBs[0]))
	sanitizedSemester, err := sql.Open("sqlite", sanitizedSemesterPath)
	if err != nil {
		t.Fatal(err)
	}
	var memberName, sessionName, orderTitle string
	if err := sanitizedSemester.QueryRow(`SELECT real_name FROM users WHERE role = 'USER'`).Scan(&memberName); err != nil {
		t.Fatal(err)
	}
	if err := sanitizedSemester.QueryRow(`SELECT worker_name FROM work_sessions LIMIT 1`).Scan(&sessionName); err != nil {
		t.Fatal(err)
	}
	if err := sanitizedSemester.QueryRow(`SELECT title FROM work_orders LIMIT 1`).Scan(&orderTitle); err != nil {
		t.Fatal(err)
	}
	if memberName != "成员0001" || sessionName != memberName || !strings.HasPrefix(orderTitle, "脱敏工单") {
		t.Fatalf("member=%q session=%q order=%q", memberName, sessionName, orderTitle)
	}
	for _, table := range []string{"finance_batches", "labor_conversion_runs"} {
		var count int
		if err := sanitizedSemester.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	sanitizedSemester.Close()

	for _, path := range []string{filepath.Join(outDir, "control.db"), sanitizedSemesterPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range [][]byte{[]byte("真实姓名"), []byte("202600009999"), secretBlob} {
			if bytes.Contains(content, forbidden) {
				t.Fatalf("sanitized database %s still contains private marker %q", path, forbidden)
			}
		}
		if ok, message := sqliteQuickCheck(path); !ok {
			t.Fatalf("sanitized database failed quick_check: %s", message)
		}
	}
}
