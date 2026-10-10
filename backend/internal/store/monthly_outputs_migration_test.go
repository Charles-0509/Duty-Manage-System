package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"testing"

	"personnel-management-go/internal/types"
)

func TestMonthlyV5MigrationRemovesOnlyLiveExtraOutputs(t *testing.T) {
	s := newTestManagedStore(t)
	cfg := s.cfg
	path := s.ActiveSemester().Database
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "migrate-output", RealName: "迁移成员", StudentNumber: "202600000093", Role: "USER", InitialPassword: "strong-member-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveWorkStudyTemplate(buildWorkStudyTemplateDocx(t, 40)); err != nil {
		t.Fatal(err)
	}
	draft, err := s.CreateMonthlyReport(types.MonthlyReportRequest{MonthlyConditions: types.MonthlyConditions{OutputMonth: "2026-10", StartDate: "2026-10-01", EndDate: "2026-10-31"}, TargetTotal: "100.00"})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"finance": []byte("original financial workbook"), "csv": []byte("old declaration csv")}
	names := map[string]string{"finance": "old-finance.xlsx", "csv": "old.csv"}
	for _, kind := range []string{"labor", "work-study", "records"} {
		name, data, e := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, kind)
		if e != nil {
			t.Fatal(e)
		}
		files[kind] = data
		names[kind] = name
	}
	packet, err := monthlyPackage(files, names)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(names)
	if _, err = s.db.Exec(`UPDATE monthly_report_versions SET status='finalized',package_blob=?,files_json=? WHERE id=?`, packet, string(metadata), draft.Version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE monthly_report_sources SET finance_xlsx_blob=X'01',finance_csv_blob=X'02' WHERE report_id=?`, draft.Report.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO monthly_report_sources(id,report_id,snapshot_json,source_hash,legacy_metadata_json,finance_xlsx_blob,finance_csv_blob) SELECT 'legacy-source',report_id,snapshot_json,source_hash,'{"id":"old"}',X'03',X'04' FROM monthly_report_sources WHERE report_id=? LIMIT 1`, draft.Report.ID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`ALTER TABLE monthly_report_versions DROP COLUMN labor_xlsx_blob`, `ALTER TABLE monthly_report_versions DROP COLUMN work_study_xlsx_blob`, `ALTER TABLE monthly_report_versions DROP COLUMN records_zip_blob`, `UPDATE semester_settings SET schema_version=5`, `PRAGMA user_version=5`} {
		if _, err = s.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = MigrateMonthlyReports(path, nil); err != nil {
		t.Fatal(err)
	}
	s, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, kind := range []string{"labor", "work-study", "records"} {
		_, data, e := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, kind)
		if e != nil || !bytes.Equal(data, files[kind]) {
			t.Fatalf("%s changed during migration: %v", kind, e)
		}
	}
	var extra, original []byte
	if err = s.db.QueryRow(`SELECT finance_xlsx_blob FROM monthly_report_sources WHERE legacy_metadata_json='' LIMIT 1`).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT finance_xlsx_blob FROM monthly_report_sources WHERE id='legacy-source'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if len(extra) != 0 || !bytes.Equal(original, []byte{3}) {
		t.Fatal("migration deleted legacy original or kept unwanted new file")
	}
	var outer []byte
	if err = s.db.QueryRow(`SELECT package_blob FROM monthly_report_versions WHERE id=?`, draft.Version.ID).Scan(&outer); err != nil {
		t.Fatal(err)
	}
	if len(outer) != 0 {
		t.Fatal("outer package retained")
	}
	for _, kind := range []string{"finance", "csv", "package"} {
		if _, _, err = s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, kind); err != sql.ErrNoRows {
			t.Fatalf("extra %s remains: %v", kind, err)
		}
	}
	if result, e := MigrateMonthlyReports(path, nil); e != nil || !result.Skipped {
		t.Fatalf("idempotence: %+v %v", result, e)
	}
}
