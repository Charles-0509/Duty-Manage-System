package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"personnel-management-go/internal/config"
)

func BenchmarkListUsers(b *testing.B) {
	appStore := newBenchmarkStore(b, 1000, 0)
	b.ResetTimer()
	for range b.N {
		if _, err := appStore.ListUsers(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListWorkOrdersPage(b *testing.B) {
	appStore := newBenchmarkStore(b, 500, 2000)
	b.ResetTimer()
	for range b.N {
		if _, err := appStore.ListWorkOrdersPage("2026-08", 20, 20); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetDashboard(b *testing.B) {
	appStore := newBenchmarkStore(b, 500, 2000)
	b.ResetTimer()
	for range b.N {
		if _, err := appStore.GetDashboard(true); err != nil {
			b.Fatal(err)
		}
	}
}

func newBenchmarkStore(b *testing.B, memberCount, workOrderCount int) *Store {
	b.Helper()
	dir := b.TempDir()
	envPath := filepath.Join(dir, "backend", ".env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		b.Fatal(err)
	}
	cfg := config.AppConfig{
		ControlDatabasePath:  filepath.Join(dir, "data", "control.db"),
		SemesterDatabaseDir:  filepath.Join(dir, "data", "semesters"),
		JWTSecret:            "0123456789abcdef0123456789abcdef",
		AdminPassword:        "strong-admin-password",
		FirstMonday:          "20260302",
		EnvFilePath:          envPath,
		WorkStudyTemplateDir: filepath.Join(dir, "templates"),
		WorkStudyContent:     "benchmark",
	}
	appStore, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { appStore.Close() })

	controlTx, err := appStore.control.Begin()
	if err != nil {
		b.Fatal(err)
	}
	semesterTx, err := appStore.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for index := 1; index <= memberCount; index++ {
		accountUUID := fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
		username := fmt.Sprintf("member-%04d", index)
		realName := fmt.Sprintf("成员%04d", index)
		studentNumber := fmt.Sprintf("2026%08d", index)
		if _, err := controlTx.Exec(`
			INSERT INTO accounts (account_uuid, username, real_name, student_number, password_hash, is_active, must_change_password)
			VALUES (?, ?, ?, ?, 'benchmark', 1, 0)
		`, accountUUID, username, realName, studentNumber); err != nil {
			b.Fatal(err)
		}
		if _, err := semesterTx.Exec(`
			INSERT INTO users (account_uuid, username, password_hash, real_name, student_number, role, sort_order, is_active, must_change_password)
			VALUES (?, ?, '', ?, ?, 'USER', ?, 1, 0)
		`, accountUUID, username, realName, studentNumber, index); err != nil {
			b.Fatal(err)
		}
	}
	if err := controlTx.Commit(); err != nil {
		b.Fatal(err)
	}
	if err := semesterTx.Commit(); err != nil {
		b.Fatal(err)
	}

	if workOrderCount > 0 {
		workTx, err := appStore.db.Begin()
		if err != nil {
			b.Fatal(err)
		}
		for orderIndex := 1; orderIndex <= workOrderCount; orderIndex++ {
			orderID := fmt.Sprintf("order-%05d", orderIndex)
			if _, err := workTx.Exec(`
				INSERT INTO work_orders (id, title, belonging_month, created_time, created_by)
				VALUES (?, ?, '2026-08', ?, '系统管理员')
			`, orderID, fmt.Sprintf("工单%05d", orderIndex), fmt.Sprintf("2026-08-%02d 12:00:00", orderIndex%28+1)); err != nil {
				b.Fatal(err)
			}
			for sessionIndex := 0; sessionIndex < 5; sessionIndex++ {
				memberID := int64((orderIndex+sessionIndex)%memberCount + 2)
				if _, err := workTx.Exec(`
					INSERT INTO work_sessions (work_order_id, date, worker_name, member_id, duration)
					SELECT ?, '2026-08-15', real_name, id, 2 FROM users WHERE id = ?
				`, orderID, memberID); err != nil {
					b.Fatal(err)
				}
			}
		}
		if err := workTx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	return appStore
}
