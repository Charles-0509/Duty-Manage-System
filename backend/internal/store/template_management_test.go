package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestLegacyTemplatesCreateGlobalTemplateAndBackfillAllSemesters(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	const (
		username      = "legacy-template-user"
		realName      = "旧模板成员"
		studentNumber = "202600000123"
	)
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: username, RealName: realName, Role: "USER", InitialPassword: "password",
	}); err != nil {
		t.Fatalf("CreateSemesterMember: %v", err)
	}
	created, err := appStore.CreateSemester(types.CreateSemesterRequest{
		Name: "legacy-template-clone", FirstMonday: "20260907", CloneFromID: appStore.ActiveSemester().ID,
	})
	if err != nil {
		t.Fatalf("CreateSemester: %v", err)
	}

	legacyContent, err := fillWorkStudyTemplate(buildWorkStudyTemplateDocx(t, 3), realName, studentNumber, []workStudyRecord{
		{Name: realName, Year: 2026, Month: 6, Day: 8, Start: "8:00", End: "12:00", Hours: "4.0"},
	}, workStudyDefaultContent)
	if err != nil {
		t.Fatalf("build legacy template: %v", err)
	}
	if err := os.MkdirAll(appStore.cfg.WorkStudyTemplateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(appStore.cfg.WorkStudyTemplateDir, realName+"_"+workStudyTemplateSuffix)
	if err := os.WriteFile(legacyPath, legacyContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := appStore.migrateLegacyWorkStudyTemplates(); err != nil {
		t.Fatalf("migrateLegacyWorkStudyTemplates: %v", err)
	}
	_, globalContent, err := appStore.GetWorkStudyTemplate()
	if err != nil {
		t.Fatalf("GetWorkStudyTemplate: %v", err)
	}
	document := readDocxEntry(t, globalContent, "word/document.xml")
	for _, placeholder := range []string{workStudyNamePlaceholder, workStudyStudentNumberPlaceholder} {
		if !strings.Contains(document, placeholder) {
			t.Fatalf("global template missing placeholder %q", placeholder)
		}
	}
	for _, historicalValue := range []string{realName, studentNumber, workStudyDefaultContent, "8:00", "12:00", "4小时"} {
		if strings.Contains(document, historicalValue) {
			t.Fatalf("global template still contains historical value %q", historicalValue)
		}
	}

	assertStudentNumber := func(db *sql.DB, label string) {
		t.Helper()
		var got string
		if err := db.QueryRow(`SELECT student_number FROM users WHERE username = ?`, username).Scan(&got); err != nil {
			t.Fatalf("%s student number query: %v", label, err)
		}
		if got != studentNumber {
			t.Fatalf("%s student number = %q, want %q", label, got, studentNumber)
		}
	}
	assertStudentNumber(appStore.db, "active semester")
	cloneDB, err := sql.Open("sqlite", created.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer cloneDB.Close()
	assertStudentNumber(cloneDB, "cloned semester")

	after, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, legacyContent) {
		t.Fatal("legacy template was modified during migration")
	}
}

func TestSaveGlobalTemplateRequiresBothPlaceholders(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	template, err := rewriteWorkStudyDOCX(buildWorkStudyTemplateDocx(t, 2), func(document []byte) ([]byte, error) {
		return bytes.ReplaceAll(document, []byte(workStudyNamePlaceholder), []byte("静态姓名")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.SaveWorkStudyTemplate(template); err == nil || !strings.Contains(err.Error(), workStudyNamePlaceholder) {
		t.Fatalf("expected missing name placeholder error, got %v", err)
	}
}

func TestBackfillLaborStudentNumberSnapshotsPreservesExistingValues(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	people, err := json.Marshal([]laborPerson{
		{Name: "待回填", StudentNumber: ""},
		{Name: "已有快照", StudentNumber: "202500000001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.db.Exec(`
		INSERT INTO labor_conversion_runs
			(id, created_at, input_filename, output_name, target_total_cents, original_total_cents,
			 final_total_cents, team_fund_cents, people_json, result_json, workbook_blob)
		VALUES ('snapshot-test', '2026-08-18 00:00:00', 'input.xlsx', 'output.xlsx', 0, 0, 0, 0, ?, '{}', X'00')
	`, string(people)); err != nil {
		t.Fatal(err)
	}

	if err := backfillLaborStudentNumberSnapshots(appStore.db, map[string]string{
		"待回填":  "202600000123",
		"已有快照": "202600000999",
	}); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := appStore.db.QueryRow(`SELECT people_json FROM labor_conversion_runs WHERE id = 'snapshot-test'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var got []laborPerson
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if got[0].StudentNumber != "202600000123" || got[1].StudentNumber != "202500000001" {
		t.Fatalf("unexpected snapshot student numbers: %#v", got)
	}
}

func TestResolveWorkStudyStudentNumbersUsesSnapshotThenRemovedMemberFallback(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "removed-number", RealName: "已移出成员", StudentNumber: "202600000321", Role: "USER", InitialPassword: "password",
	}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.RemoveSemesterMember(findLocalUserID(t, appStore, "removed-number")); err != nil {
		t.Fatal(err)
	}
	records := map[string][]workStudyRecord{
		"历史姓名":  {{Name: "历史姓名"}},
		"已移出成员": {{Name: "已移出成员"}},
	}
	snapshot, err := json.Marshal([]laborPerson{{Name: "历史姓名", StudentNumber: "202600000999"}})
	if err != nil {
		t.Fatal(err)
	}
	numbers, err := appStore.resolveWorkStudyStudentNumbers(records, string(snapshot))
	if err != nil {
		t.Fatalf("resolveWorkStudyStudentNumbers: %v", err)
	}
	if numbers["历史姓名"] != "202600000999" || numbers["已移出成员"] != "202600000321" {
		t.Fatalf("unexpected student numbers: %#v", numbers)
	}
}
