package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSemesterDoctorRequiresCurrentVersionAndMonthlyTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semester.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		`CREATE TABLE semester_settings(id INTEGER,schema_version INTEGER)`,
		`INSERT INTO semester_settings VALUES(1,5)`,
		`PRAGMA user_version=5`,
		`CREATE TABLE monthly_reports(id TEXT)`,
		`CREATE TABLE monthly_report_sources(id TEXT)`,
		`CREATE TABLE monthly_report_versions(id TEXT)`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if ok, reason := sqliteSemesterCheck(path); !ok {
		t.Fatal(reason)
	}
	if _, err = db.Exec(`DROP TABLE monthly_report_versions`); err != nil {
		t.Fatal(err)
	}
	if ok, _ := sqliteSemesterCheck(path); ok {
		t.Fatal("missing monthly table accepted")
	}
}
