package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"personnel-management-go/internal/types"

	_ "modernc.org/sqlite"
)

func TestSchedulePlansPublishUpdateExportImportAndDelete(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	for _, member := range []types.CreateMemberRequest{
		{Username: "plan-a", RealName: "排班甲", Role: "USER", InitialPassword: "strong-member-password"},
		{Username: "plan-b", RealName: "排班乙", Role: "USER", InitialPassword: "strong-member-password"},
	} {
		if err := appStore.CreateSemesterMember(member); err != nil {
			t.Fatal(err)
		}
	}

	first, err := appStore.CreateSchedulePlan("第一张表", map[string][]string{"Mon-1": {"排班甲(单双)"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.IsPublished {
		t.Fatal("new schedule plan was published")
	}
	if schedule, err := appStore.GetSchedule(); err != nil || len(schedule) != 0 {
		t.Fatalf("unpublished schedule=%v err=%v", schedule, err)
	}
	if _, err := appStore.PublishSchedulePlan(first.ID); err != nil {
		t.Fatal(err)
	}
	if schedule, err := appStore.GetSchedule(); err != nil || len(schedule["Mon-1"]) != 1 {
		t.Fatalf("published schedule=%v err=%v", schedule, err)
	}

	second, err := appStore.CreateSchedulePlan("第二张表", map[string][]string{"Tue-1": {"排班乙(单)"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.PublishSchedulePlan(second.ID); err != nil {
		t.Fatal(err)
	}
	plans, err := appStore.ListSchedulePlans()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].ID != second.ID || !plans[0].IsPublished || plans[1].IsPublished {
		t.Fatalf("plans=%+v", plans)
	}
	if _, err := appStore.UpdateSchedulePlan(second.ID, "第二张已更新", map[string][]string{"Wed-1": {"排班甲(双)"}}); err != nil {
		t.Fatal(err)
	}
	if schedule, err := appStore.GetSchedule(); err != nil || len(schedule["Wed-1"]) != 1 {
		t.Fatalf("updated published schedule=%v err=%v", schedule, err)
	}
	if err := appStore.DeleteSchedulePlan(second.ID); !errors.Is(err, ErrPublishedSchedulePlan) {
		t.Fatalf("delete published err=%v", err)
	}
	if err := appStore.DeleteSchedulePlan(first.ID); err != nil {
		t.Fatal(err)
	}

	_, workbook, err := appStore.ExportSchedulePlanWorkbook(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := appStore.ImportSchedulePlanWorkbook("导入表", workbook)
	if err != nil {
		t.Fatal(err)
	}
	if imported.IsPublished {
		t.Fatal("imported schedule plan was published")
	}
	importedPlan, err := appStore.GetSchedulePlan(imported.ID)
	if err != nil || len(importedPlan.Schedule["Wed-1"]) != 1 {
		t.Fatalf("imported=%+v err=%v", importedPlan, err)
	}
	if _, err := appStore.ImportSchedulePlanWorkbook("损坏表", []byte("not xlsx")); !errors.Is(err, ErrInvalidScheduleWorkbook) {
		t.Fatalf("invalid workbook err=%v", err)
	}
	if _, err := appStore.CreateSchedulePlan("导入表", map[string][]string{}); !errors.Is(err, ErrSchedulePlanNameConflict) {
		t.Fatalf("duplicate name err=%v", err)
	}
}

func TestMigrateSchedulePlanDatabaseFromV3IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semester.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE semester_settings (id INTEGER PRIMARY KEY, schema_version INTEGER NOT NULL)`,
		`INSERT INTO semester_settings (id, schema_version) VALUES (1, 3)`,
		`CREATE TABLE schedule_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			shift_code TEXT NOT NULL,
			real_name TEXT NOT NULL,
			member_id INTEGER,
			week_type TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(shift_code, real_name, week_type)
		)`,
		`INSERT INTO schedule_entries (shift_code, real_name, member_id, week_type) VALUES ('Mon-1', '迁移成员', 1, 'both')`,
		`PRAGMA user_version = 3`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()

	result, err := migrateSchedulePlanDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.From != 3 || result.To != 4 || result.Entries != 1 {
		t.Fatalf("result=%+v", result)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	var published, count int
	if err := db.QueryRow(`SELECT name, is_published FROM schedule_plans`).Scan(&name, &published); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_entries WHERE schedule_plan_id != ''`).Scan(&count); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if name != "默认排班表" || published != 1 || count != 1 {
		t.Fatalf("name=%s published=%d count=%d", name, published, count)
	}

	repeated, err := migrateSchedulePlanDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.From != 4 || repeated.Entries != 1 {
		t.Fatalf("repeated=%+v", repeated)
	}
}
