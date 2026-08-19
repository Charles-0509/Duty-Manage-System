package store

import (
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"
)

func TestGenerateAutoSchedulePrefersPairedAssignments(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("odd-only", []string{"Mon-1"}, nil),
		availabilityItem("even-only", nil, []string{"Mon-1"}),
		availabilityItem("paired", []string{"Mon-1"}, []string{"Mon-1"}),
	}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	if got := result.Schedule["Mon-1"]; !reflect.DeepEqual(got, []string{"paired(单双)"}) {
		t.Fatalf("Mon-1 assignments = %v, want paired member", got)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected uncovered-slot warnings")
	}
}

func TestGenerateAutoScheduleUsesScarcityAsTieBreaker(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("flexible", []string{"Mon-1", "Unused-1", "Unused-2"}, []string{"Mon-1", "Unused-1", "Unused-2"}),
		availabilityItem("scarce", []string{"Mon-1"}, []string{"Mon-1"}),
	}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	if got := result.Schedule["Mon-1"]; !reflect.DeepEqual(got, []string{"scarce(单双)"}) {
		t.Fatalf("Mon-1 assignments = %v, want scarce member preferred on equal load", got)
	}
}

func TestGenerateAutoSchedulePrefersConsecutiveAssignments(t *testing.T) {
	codes := []string{"Mon-1", "Mon-2", "Mon-5", "Mon-6"}
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("member-a", codes, codes),
		availabilityItem("member-b", codes, codes),
	}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	if got := countPairedAssignments(result.Schedule); got != len(codes) {
		t.Fatalf("paired assignments = %d, want %d", got, len(codes))
	}

	for _, memberName := range []string{"member-a", "member-b"} {
		assigned := assignedShiftCodes(result.Schedule, memberName)
		if len(assigned) != 2 {
			t.Fatalf("%s assignments = %v, want two shifts", memberName, assigned)
		}
		if !areConsecutiveShiftCodes(assigned[0], assigned[1]) {
			t.Fatalf("%s assignments = %v, want consecutive shifts", memberName, assigned)
		}
	}
}

func TestGenerateAutoScheduleConnectsGeneratedAssignmentsToLockedAssignments(t *testing.T) {
	codes := []string{"Mon-1", "Mon-2", "Mon-5", "Mon-6"}
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("member-a", codes, codes),
		availabilityItem("member-b", codes, codes),
	}
	locked := map[string][]string{"Mon-1": {"member-a(单双)"}}

	result, err := generateAutoScheduleFromOverviewWithLocked(overview, 1, locked)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverviewWithLocked: %v", err)
	}
	if got := assignedShiftCodes(result.Schedule, "member-a"); !reflect.DeepEqual(got, []string{"Mon-1", "Mon-2"}) {
		t.Fatalf("member-a assignments = %v, want locked Mon-1 followed by Mon-2", got)
	}
	if got := assignedShiftCodes(result.Schedule, "member-b"); !reflect.DeepEqual(got, []string{"Mon-5", "Mon-6"}) {
		t.Fatalf("member-b assignments = %v, want consecutive Mon-5 and Mon-6", got)
	}
}

func TestGenerateAutoSchedulePrefersConsecutiveOddWeekAssignments(t *testing.T) {
	codes := []string{"Mon-1", "Mon-2", "Mon-5", "Mon-6"}
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("member-a", codes, nil),
		availabilityItem("member-b", codes, nil),
	}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	for _, memberName := range []string{"member-a", "member-b"} {
		assigned := assignedShiftCodes(result.Schedule, memberName)
		if len(assigned) != 2 || !areConsecutiveShiftCodes(assigned[0], assigned[1]) {
			t.Fatalf("%s assignments = %v, want two consecutive odd-week shifts", memberName, assigned)
		}
	}
}

func TestGenerateAutoScheduleRespectsSoftCapWhenFeasible(t *testing.T) {
	codes := allScheduleShiftCodes()
	overview := make([]types.AvailabilityOverviewItem, 0, 8)
	for index := 0; index < 8; index++ {
		overview = append(overview, availabilityItem(fmt.Sprintf("member-%02d", index), codes, codes))
	}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	assertCompleteCoverage(t, result.Schedule, 1)
	for _, item := range result.ShiftDistribution {
		if item.Value > autoScheduleSoftLoad {
			t.Fatalf("%s load = %.1f, want <= %.1f", item.Name, item.Value, autoScheduleSoftLoad)
		}
	}
	if countPairedAssignments(result.Schedule) != len(codes) {
		t.Fatalf("paired assignments = %d, want %d", countPairedAssignments(result.Schedule), len(codes))
	}
}

func TestGenerateAutoScheduleUsesHardCapOnlyWhenNeeded(t *testing.T) {
	codes := allScheduleShiftCodes()
	overview := make([]types.AvailabilityOverviewItem, 0, 18)
	for index := 0; index < 18; index++ {
		overview = append(overview, availabilityItem(fmt.Sprintf("member-%02d", index), codes, codes))
	}

	result, err := generateAutoScheduleFromOverview(overview, 3)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	assertCompleteCoverage(t, result.Schedule, 3)
	for _, item := range result.ShiftDistribution {
		if item.Value != autoScheduleHardLoad {
			t.Fatalf("%s load = %.1f, want %.1f", item.Name, item.Value, autoScheduleHardLoad)
		}
	}
	if warningsContain(result.Warnings, "超过 5 班") {
		t.Fatalf("unexpected hard-cap warning: %v", result.Warnings)
	}
}

func TestGenerateAutoScheduleWarnsWhenCoverageRequiresHardCapException(t *testing.T) {
	codes := allScheduleShiftCodes()
	overview := []types.AvailabilityOverviewItem{availabilityItem("only-member", codes, codes)}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	assertCompleteCoverage(t, result.Schedule, 1)
	if len(result.ShiftDistribution) != 1 || result.ShiftDistribution[0].Value != float64(len(codes)) {
		t.Fatalf("distribution = %+v, want one member with %d shifts", result.ShiftDistribution, len(codes))
	}
	if !warningsContain(result.Warnings, "超过 5 班") {
		t.Fatalf("warnings = %v, want hard-cap exception warning", result.Warnings)
	}
}

func TestGenerateAutoScheduleFallsBackToSplitCoverage(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("odd-only", []string{"Mon-1"}, nil),
		availabilityItem("even-only", nil, []string{"Mon-1"}),
	}

	result, err := generateAutoScheduleFromOverview(overview, 1)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverview: %v", err)
	}
	joined := strings.Join(result.Schedule["Mon-1"], ",")
	if !strings.Contains(joined, "odd-only(单)") || !strings.Contains(joined, "even-only(双)") {
		t.Fatalf("Mon-1 assignments = %v, want split fallback", result.Schedule["Mon-1"])
	}
}

func TestGenerateAutoSchedulePreservesLockedAssignmentsAndFillsParityDeficits(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("locked-pair", []string{"Mon-1"}, []string{"Mon-1"}),
		availabilityItem("locked-odd", []string{"Mon-1"}, nil),
		availabilityItem("auto-pair", []string{"Mon-1"}, []string{"Mon-1"}),
		availabilityItem("auto-even", nil, []string{"Mon-1"}),
	}
	locked := map[string][]string{
		"Mon-1": {"locked-pair(单)", "locked-pair(双)", "locked-odd(单)"},
	}

	result, err := generateAutoScheduleFromOverviewWithLocked(overview, 3, locked)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverviewWithLocked: %v", err)
	}
	assertSlotCoverage(t, result.Schedule["Mon-1"], 3, 3)
	for _, label := range []string{"locked-pair(单双)", "locked-odd(单)", "auto-pair(单双)", "auto-even(双)"} {
		if !slices.Contains(result.Schedule["Mon-1"], label) {
			t.Fatalf("Mon-1 assignments = %v, missing %s", result.Schedule["Mon-1"], label)
		}
	}
}

func TestGenerateAutoScheduleAddsOnlyOnePairAfterTwoLockedPairs(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("locked-a", []string{"Mon-1"}, []string{"Mon-1"}),
		availabilityItem("locked-b", []string{"Mon-1"}, []string{"Mon-1"}),
		availabilityItem("auto", []string{"Mon-1"}, []string{"Mon-1"}),
		availabilityItem("unused", []string{"Mon-1"}, []string{"Mon-1"}),
	}
	locked := map[string][]string{
		"Mon-1": {"locked-a(单双)", "locked-b(单双)"},
	}

	result, err := generateAutoScheduleFromOverviewWithLocked(overview, 3, locked)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverviewWithLocked: %v", err)
	}
	assertSlotCoverage(t, result.Schedule["Mon-1"], 3, 3)
	if len(result.Schedule["Mon-1"]) != 3 {
		t.Fatalf("Mon-1 assignments = %v, want two locked pairs plus one generated pair", result.Schedule["Mon-1"])
	}
	for _, label := range []string{"locked-a(单双)", "locked-b(单双)"} {
		if !slices.Contains(result.Schedule["Mon-1"], label) {
			t.Fatalf("Mon-1 assignments = %v, missing locked assignment %s", result.Schedule["Mon-1"], label)
		}
	}
}

func TestGenerateAutoScheduleKeepsLockedOverstaffingAndWarns(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{
		availabilityItem("member-a", nil, nil),
		availabilityItem("member-b", nil, nil),
		availabilityItem("member-c", nil, nil),
		availabilityItem("member-d", nil, nil),
	}
	locked := map[string][]string{
		"Mon-1": {"member-a(单双)", "member-b(单双)", "member-c(单双)", "member-d(单双)"},
	}

	result, err := generateAutoScheduleFromOverviewWithLocked(overview, 3, locked)
	if err != nil {
		t.Fatalf("generateAutoScheduleFromOverviewWithLocked: %v", err)
	}
	assertSlotCoverage(t, result.Schedule["Mon-1"], 4, 4)
	if !warningsContain(result.Warnings, "超过每班 3 人") {
		t.Fatalf("warnings = %v, want preserved-overstaffing warning", result.Warnings)
	}
}

func TestGenerateAutoScheduleIsDeterministic(t *testing.T) {
	codes := allScheduleShiftCodes()
	overview := make([]types.AvailabilityOverviewItem, 0, 10)
	for index := 0; index < 10; index++ {
		overview = append(overview, availabilityItem(fmt.Sprintf("member-%02d", index), codes, codes))
	}

	first, err := generateAutoScheduleFromOverview(overview, 2)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		result, err := generateAutoScheduleFromOverview(overview, 2)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result, first) {
			t.Fatalf("attempt %d produced a different result", attempt+1)
		}
	}
}

func TestGenerateAutoScheduleRejectsInvalidPerSlot(t *testing.T) {
	overview := []types.AvailabilityOverviewItem{availabilityItem("member", []string{"Mon-1"}, []string{"Mon-1"})}
	if _, err := generateAutoScheduleFromOverview(overview, 0); err == nil {
		t.Fatal("perSlot=0 accepted")
	}
	if _, err := generateAutoScheduleFromOverview(overview, 4); err == nil {
		t.Fatal("perSlot=4 accepted")
	}
}

func TestStoreGenerateAutoScheduleUsesSemesterAvailability(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	for _, member := range []types.CreateMemberRequest{
		{Username: "membera", RealName: "成员甲", Role: "USER", InitialPassword: "strong-member-password"},
		{Username: "memberb", RealName: "成员乙", Role: "USER", InitialPassword: "strong-member-password"},
	} {
		if err := appStore.CreateSemesterMember(member); err != nil {
			t.Fatal(err)
		}
	}
	if err := appStore.SaveAvailability("成员甲", types.SaveAvailabilityRequest{Single: []string{"Mon-1", "Tue-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := appStore.SaveAvailability("成员乙", types.SaveAvailabilityRequest{Double: []string{"Mon-1", "Tue-1"}}); err != nil {
		t.Fatal(err)
	}

	result, err := appStore.GenerateAutoSchedule(1, nil)
	if err != nil {
		t.Fatalf("GenerateAutoSchedule: %v", err)
	}
	joined := strings.Join(result.Schedule["Mon-1"], ",")
	if !strings.Contains(joined, "成员甲(单)") || !strings.Contains(joined, "成员乙(双)") {
		t.Fatalf("Mon-1 assignments = %v, want semester availability assignments", result.Schedule["Mon-1"])
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for uncovered slots")
	}
}

func TestGenerateAutoScheduleProductionSnapshot(t *testing.T) {
	snapshotPath := os.Getenv("DMS_AUTO_SCHEDULE_SNAPSHOT_DB")
	if snapshotPath == "" {
		t.Skip("set DMS_AUTO_SCHEDULE_SNAPSHOT_DB to replay a semester database")
	}

	db, err := sql.Open("sqlite", "file:"+snapshotPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer db.Close()

	appStore := &Store{db: db}
	overview, err := appStore.GetAvailabilityOverview()
	if err != nil {
		t.Fatalf("GetAvailabilityOverview: %v", err)
	}
	if len(overview) == 0 {
		t.Fatal("snapshot contains no active members")
	}

	for _, perSlot := range []int{1, 3} {
		perSlot := perSlot
		t.Run(fmt.Sprintf("per-slot-%d", perSlot), func(t *testing.T) {
			first, err := generateAutoScheduleFromOverview(overview, perSlot)
			if err != nil {
				t.Fatalf("generateAutoScheduleFromOverview: %v", err)
			}
			assertCompleteCoverage(t, first.Schedule, perSlot)
			assertLoadWithinWarningPolicy(t, first)
			if perSlot == 1 && maxDistributionLoad(first.ShiftDistribution) > autoScheduleSoftLoad {
				t.Fatalf("maximum load = %.1f, want <= %.1f", maxDistributionLoad(first.ShiftDistribution), autoScheduleSoftLoad)
			}

			for attempt := 0; attempt < 5; attempt++ {
				result, err := generateAutoScheduleFromOverview(overview, perSlot)
				if err != nil {
					t.Fatalf("attempt %d: %v", attempt+1, err)
				}
				if !reflect.DeepEqual(result, first) {
					t.Fatalf("attempt %d produced a different result", attempt+1)
				}
			}
		})
	}

	t.Run("complete-locked-production-schedule", func(t *testing.T) {
		locked, err := generateAutoScheduleFromOverview(overview, 1)
		if err != nil {
			t.Fatalf("generate locked schedule: %v", err)
		}
		completed, err := generateAutoScheduleFromOverviewWithLocked(overview, 3, locked.Schedule)
		if err != nil {
			t.Fatalf("complete locked schedule: %v", err)
		}
		assertCompleteCoverage(t, completed.Schedule, 3)
		assertLoadWithinWarningPolicy(t, completed)
		for shiftCode, labels := range locked.Schedule {
			for _, label := range labels {
				if !slices.Contains(completed.Schedule[shiftCode], label) {
					t.Fatalf("%s lost locked assignment %s: %v", shiftCode, label, completed.Schedule[shiftCode])
				}
			}
		}
	})
}

func BenchmarkGenerateAutoScheduleProductionSize(b *testing.B) {
	codes := allScheduleShiftCodes()
	overview := make([]types.AvailabilityOverviewItem, 0, 40)
	for memberIndex := 0; memberIndex < 40; memberIndex++ {
		single := make([]string, 0, len(codes))
		double := make([]string, 0, len(codes))
		for codeIndex, code := range codes {
			if (memberIndex+codeIndex)%3 != 0 {
				single = append(single, code)
			}
			if (memberIndex+codeIndex)%4 != 0 {
				double = append(double, code)
			}
		}
		overview = append(overview, availabilityItem(fmt.Sprintf("member-%02d", memberIndex), single, double))
	}

	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := generateAutoScheduleFromOverview(overview, 3); err != nil {
			b.Fatal(err)
		}
	}
}

func availabilityItem(name string, single, double []string) types.AvailabilityOverviewItem {
	return types.AvailabilityOverviewItem{
		Username: name,
		RealName: name,
		Availability: types.AvailabilityPayload{
			Single: append([]string(nil), single...),
			Double: append([]string(nil), double...),
		},
	}
}

func allScheduleShiftCodes() []string {
	codes := make([]string, 0, len(config.WeekdaysCode)*len(config.TimeSlots))
	for _, dayCode := range config.WeekdaysCode {
		for shiftIndex := range config.TimeSlots {
			codes = append(codes, fmt.Sprintf("%s-%d", dayCode, shiftIndex+1))
		}
	}
	return codes
}

func assertCompleteCoverage(t *testing.T, schedule map[string][]string, perSlot int) {
	t.Helper()
	for _, code := range allScheduleShiftCodes() {
		odd := 0
		even := 0
		for _, label := range schedule[code] {
			switch {
			case strings.HasSuffix(label, "(单双)"):
				odd++
				even++
			case strings.HasSuffix(label, "(单)"):
				odd++
			case strings.HasSuffix(label, "(双)"):
				even++
			}
		}
		if odd != perSlot || even != perSlot {
			t.Fatalf("%s coverage = odd %d/even %d, want %d/%d", code, odd, even, perSlot, perSlot)
		}
	}
}

func assertSlotCoverage(t *testing.T, labels []string, expectedOdd, expectedEven int) {
	t.Helper()
	odd := 0
	even := 0
	for _, label := range labels {
		switch {
		case strings.HasSuffix(label, "(单双)"):
			odd++
			even++
		case strings.HasSuffix(label, "(单)"):
			odd++
		case strings.HasSuffix(label, "(双)"):
			even++
		}
	}
	if odd != expectedOdd || even != expectedEven {
		t.Fatalf("coverage = odd %d/even %d, want %d/%d; labels=%v", odd, even, expectedOdd, expectedEven, labels)
	}
}

func countPairedAssignments(schedule map[string][]string) int {
	count := 0
	for _, labels := range schedule {
		for _, label := range labels {
			if strings.HasSuffix(label, "(单双)") {
				count++
			}
		}
	}
	return count
}

func warningsContain(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func assertLoadWithinWarningPolicy(t *testing.T, result types.AutoScheduleResponse) {
	t.Helper()
	overloaded := false
	for _, item := range result.ShiftDistribution {
		if item.Value > autoScheduleHardLoad {
			overloaded = true
		}
	}
	if overloaded != warningsContain(result.Warnings, "超过 5 班") {
		t.Fatalf("load/warning mismatch: distribution=%+v warnings=%v", result.ShiftDistribution, result.Warnings)
	}
}

func maxDistributionLoad(distribution []types.ChartItem) float64 {
	maximum := 0.0
	for _, item := range distribution {
		maximum = max(maximum, item.Value)
	}
	return maximum
}

func assignedShiftCodes(schedule map[string][]string, memberName string) []string {
	codes := make([]string, 0)
	for _, code := range allScheduleShiftCodes() {
		for _, label := range schedule[code] {
			if baseName(label) == memberName {
				codes = append(codes, code)
				break
			}
		}
	}
	return codes
}

func areConsecutiveShiftCodes(left, right string) bool {
	for dayIndex, dayCode := range config.WeekdaysCode {
		for shiftIndex := 0; shiftIndex+1 < len(config.TimeSlots); shiftIndex++ {
			first := fmt.Sprintf("%s-%d", dayCode, shiftIndex+1)
			second := fmt.Sprintf("%s-%d", config.WeekdaysCode[dayIndex], shiftIndex+2)
			if (left == first && right == second) || (left == second && right == first) {
				return true
			}
		}
	}
	return false
}
