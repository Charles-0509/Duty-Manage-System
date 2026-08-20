package store

import (
	"errors"
	"testing"

	"personnel-management-go/internal/types"
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
