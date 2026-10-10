package store

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"personnel-management-go/internal/types"
)

func TestMonthlyMigrationPreservesOriginalFilesAndCanBeRepeated(t *testing.T) {
	s := newTestManagedStore(t)
	cfg := s.cfg
	path := s.ActiveSemester().Database
	finance := []byte("original financial workbook bytes")
	workbook := []byte("original submitted labor workbook bytes")
	csvData := []byte("姓名,年,月,日,起,讫,时数\n旧成员,2026,9,1,8:00,10:00,2.0\n旧成员,合计,,,,,2.0\n")
	financeHash := fmt.Sprintf("%x", sha256.Sum256(finance))
	csvHash := fmt.Sprintf("%x", sha256.Sum256(csvData))
	for _, statement := range legacyMonthlyFixtureSchema {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO finance_batches (id,created_at,start_date,end_date,output_month,work_order_ids_json,include_management,management_months,excel_filename,csv_filename,excel_blob,csv_blob,excel_sha256,csv_sha256) VALUES('old-source','2026-09-30 12:00:00','2026-09-01','2026-09-30','2026-09','[]',0,0,'finance.xlsx','finance.csv',?,?,?,?)`, finance, csvData, financeHash, csvHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO labor_conversion_runs(id,created_at,input_filename,output_name,csv_name,target_total_cents,original_total_cents,final_total_cents,team_fund_cents,seed,csv_output_month,source_finance_batch_id,parent_run_id,is_manual_adjust,people_json,result_json,workbook_blob,csv_blob) VALUES('old-run','2026-09-30 13:00:00','finance.xlsx','labor.xlsx','labor.csv',5000,5000,5000,0,1,'2026-09','old-source','',0,'[{"Name":"旧成员","StudentNumber":"202600000071","Original":5000,"Adjusted":5000}]','{}',?,?)`, workbook, csvData); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE semester_settings SET schema_version=4`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := MigrateMonthlyReports(path, buildWorkStudyTemplateDocx(t, 40))
	if err != nil {
		t.Fatal(err)
	}
	if result.FinanceCount != 1 || result.LaborCount != 1 || result.GeneratedRecords != 1 {
		t.Fatalf("migration result: %+v", result)
	}
	reopened, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reports, err := reopened.ListMonthlyReports()
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports: %v %v", reports, err)
	}
	view, err := reopened.GetMonthlyReport(reports[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Version.Status != "legacy" || view.OriginalTotal != "50.00" {
		t.Fatalf("history: %+v", view)
	}
	for kind, want := range map[string][]byte{"finance": finance, "labor": workbook, "csv": csvData} {
		_, got, err := reopened.DownloadMonthlyFile(reports[0].ID, view.Version.ID, kind)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s not preserved: %v", kind, err)
		}
	}
	if _, err := reopened.NewMonthlyVersion(reports[0].ID, types.MonthlyVersionRequest{BaseVersionID: view.Version.ID, Mode: "copy"}); err != ErrMonthlyReadOnly {
		t.Fatalf("legacy is editable: %v", err)
	}
	again, err := MigrateMonthlyReports(path, nil)
	if err != nil || !again.Skipped {
		t.Fatalf("repeat: %+v %v", again, err)
	}
}

var legacyMonthlyFixtureSchema = []string{
	`CREATE TABLE IF NOT EXISTS finance_batches(id TEXT PRIMARY KEY,created_at TEXT,start_date TEXT,end_date TEXT,output_month TEXT,work_order_ids_json TEXT,include_management INTEGER,management_months INTEGER,excel_filename TEXT,csv_filename TEXT,excel_blob BLOB,csv_blob BLOB,excel_sha256 TEXT,csv_sha256 TEXT)`,
	`CREATE TABLE IF NOT EXISTS labor_conversion_runs(id TEXT PRIMARY KEY,created_at TEXT,input_filename TEXT,output_name TEXT,csv_name TEXT,target_total_cents INTEGER,original_total_cents INTEGER,final_total_cents INTEGER,team_fund_cents INTEGER,seed INTEGER,csv_output_month TEXT,source_finance_batch_id TEXT,parent_run_id TEXT,is_manual_adjust INTEGER,people_json TEXT,result_json TEXT,workbook_blob BLOB,csv_blob BLOB)`,
}
