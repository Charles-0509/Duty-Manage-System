package store

import (
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestAutoScheduleBalancesAndWarns(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	// Two members with complementary availability.
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{Username: "membera", RealName: "成员甲", Role: "USER", InitialPassword: "password"}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{Username: "memberb", RealName: "成员乙", Role: "USER", InitialPassword: "password"}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.SaveAvailability("成员甲", types.SaveAvailabilityRequest{Single: []string{"Mon-1", "Tue-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.SaveAvailability("成员乙", types.SaveAvailabilityRequest{Double: []string{"Mon-1", "Tue-1"}}); err != nil {
		t.Fatal(err)
	}

	result, err := appStore.GenerateAutoSchedule(1)
	if err != nil {
		t.Fatalf("GenerateAutoSchedule: %v", err)
	}
	if len(result.Schedule["Mon-1"]) != 2 {
		t.Fatalf("Mon-1 assignments = %v, want one odd-week and one even-week member", result.Schedule["Mon-1"])
	}
	joined := strings.Join(result.Schedule["Mon-1"], ",")
	if !strings.Contains(joined, "(单)") || !strings.Contains(joined, "(双)") {
		t.Fatalf("expected single and double week labels, got %v", result.Schedule["Mon-1"])
	}
	// 30 slots × 2 parities: most units are unstaffed and must be warned.
	if len(result.Warnings) == 0 {
		t.Fatal("expected understaff warnings for uncovered slots")
	}

	if _, err := appStore.GenerateAutoSchedule(0); err == nil {
		t.Fatal("perSlot=0 accepted")
	}
	if _, err := appStore.GenerateAutoSchedule(4); err == nil {
		t.Fatal("perSlot=4 accepted")
	}
}
