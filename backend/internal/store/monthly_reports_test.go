package store

import (
	"bytes"
	"encoding/csv"
	"personnel-management-go/internal/types"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMonthlyAllocationKeepsEarnedPayAndFillsZeroIncomeMembersFirst(t *testing.T) {
	snapshot := MonthlySnapshot{Members: []MonthlyMember{
		{ID: 1, Name: "成员甲", Original: 230000},
		{ID: 2, Name: "成员乙", Original: 0},
		{ID: 3, Name: "成员丙", Original: 25000},
	}}
	result, err := AllocateMonthlyReport(snapshot, 435000, []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{190000, 160000, 85000}
	for index, row := range result.Rows {
		if row.Declared != want[index] {
			t.Fatalf("%s declared=%d, want %d", row.Name, row.Declared, want[index])
		}
	}
	if result.Original != 255000 || result.Declared != 435000 || result.TeamFund != 180000 || result.Unallocated != 0 {
		t.Fatalf("totals: %+v", result)
	}
	if result.Rows[0].SalaryOutgoing != 40000 || result.Rows[1].SalaryIncoming != 40000 || result.Rows[1].TeamFund != 120000 || result.Rows[2].TeamFund != 60000 {
		t.Fatalf("proxy attribution: %+v", result.Rows)
	}
}

func TestMonthlyAllocationRespectsMemberOrderAndExcludedProxies(t *testing.T) {
	snapshot := MonthlySnapshot{Members: []MonthlyMember{{ID: 1, Name: "甲"}, {ID: 2, Name: "乙"}, {ID: 3, Name: "丙"}, {ID: 4, Name: "丁", Original: 50000}}}
	result, err := AllocateMonthlyReport(snapshot, 450000, []int64{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{160000, 160000, 80000, 50000} {
		if result.Rows[i].Declared != want {
			t.Fatalf("order/stage: %+v", result.Rows)
		}
	}
	excluded, err := AllocateMonthlyReport(snapshot, 450000, []int64{4})
	if err != nil {
		t.Fatal(err)
	}
	if excluded.Rows[0].Declared != 0 || excluded.Rows[3].Declared != 190000 || excluded.Unallocated != 260000 {
		t.Fatalf("excluded/capacity: %+v", excluded)
	}
}

func TestMonthlyAmountsKeepFractionalEarnedCentsAndEnforceDeclarationCaps(t *testing.T) {
	snapshot := MonthlySnapshot{Members: []MonthlyMember{{ID: 1, Name: "甲", Original: 2501}, {ID: 2, Name: "乙"}}}
	result, err := AllocateMonthlyReport(snapshot, 5000, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Original != 2501 || result.TeamFund != 2499 || result.Rows[0].SalaryOutgoing != 1 || result.Rows[1].SalaryIncoming != 1 {
		t.Fatalf("earned cents were lost: %+v", result)
	}
	for _, amount := range []int64{192500, 195000} {
		manual, err := AdjustMonthlyReport(snapshot, map[int64]int64{1: amount, 2: 0}, []int64{1, 2})
		if err != nil || manual.Rows[0].Declared != amount {
			t.Fatalf("allowed boundary %d: %v", amount, err)
		}
	}
	for _, amount := range []int64{-2500, 192000, 197500} {
		if _, err := AdjustMonthlyReport(snapshot, map[int64]int64{1: amount, 2: 0}, []int64{1, 2}); err == nil {
			t.Fatalf("invalid amount accepted: %d", amount)
		}
	}
	if _, err := AllocateMonthlyReport(snapshot, 2500, []int64{1, 2}); err == nil {
		t.Fatal("target below earned pay accepted")
	}
	for _, invalid := range []string{"NaN", "Inf", "1e3", "-25", "0.001"} {
		if _, err := ParseMonthlyMoney(invalid); err == nil {
			t.Fatalf("invalid money accepted: %s", invalid)
		}
	}
}

func TestMonthlyVersionsKeepSourcesUntilExplicitRefresh(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	for _, member := range []types.CreateMemberRequest{
		{Username: "monthly-worker", RealName: "月报值班员", StudentNumber: "202600000051", Role: "USER", InitialPassword: "strong-member-password"},
		{Username: "monthly-helper", RealName: "月报成员", StudentNumber: "202600000052", Role: "USER", InitialPassword: "strong-member-password"},
	} {
		if err := s.CreateSemesterMember(member); err != nil {
			t.Fatal(err)
		}
	}
	order, err := s.CreateWorkOrder(types.SaveWorkOrderRequest{Title: "月度工单", BelongingMonth: "2026-09", WorkSessions: []types.WorkSession{{Date: "2026-09-09", WorkerName: "月报值班员", Duration: 30}}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	conditions := types.MonthlyConditions{OutputMonth: "2026-09", StartDate: "2026-09-01", EndDate: "2026-09-30", WorkOrderIDs: []string{order.ID}}
	preview, err := s.PreviewMonthlyReport(conditions)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.CreateMonthlyReport(types.MonthlyReportRequest{MonthlyConditions: conditions, TargetTotal: "2300.00", SourceHash: preview.SourceHash})
	if err != nil {
		t.Fatal(err)
	}
	if draft.OriginalTotal != "1500.00" || draft.Unallocated != "0.00" {
		t.Fatalf("draft: %+v", draft)
	}
	_, err = s.UpdateWorkOrder(order.ID, types.SaveWorkOrderRequest{Title: "更正的工单", BelongingMonth: "2026-09", WorkSessions: []types.WorkSession{{Date: "2026-09-10", WorkerName: "月报值班员", Duration: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	reallocated, err := s.NewMonthlyVersion(draft.Report.ID, types.MonthlyVersionRequest{BaseVersionID: draft.Version.ID, Mode: "allocate", TargetTotal: "2400.00"})
	if err != nil {
		t.Fatal(err)
	}
	if reallocated.OriginalTotal != "1500.00" {
		t.Fatalf("reallocation read live work orders: %s", reallocated.OriginalTotal)
	}
	refreshed, err := s.NewMonthlyVersion(draft.Report.ID, types.MonthlyVersionRequest{BaseVersionID: reallocated.Version.ID, Mode: "refresh", TargetTotal: "3000.00"})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.OriginalTotal != "2500.00" {
		t.Fatalf("refresh did not read correction: %s", refreshed.OriginalTotal)
	}
	original, err := s.GetMonthlyReport(draft.Report.ID, draft.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if original.OriginalTotal != "1500.00" {
		t.Fatal("old version changed")
	}
}

func TestMonthlyFinalizationLimitsTimeAndFreezesAllDownloads(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	if err := s.UpdateSemesterSettings("20260831", "冻结工作内容", DefaultRateConfig()); err != nil {
		t.Fatal(err)
	}
	for _, member := range []types.CreateMemberRequest{
		{Username: "report-worker", RealName: "报表值班员", StudentNumber: "202600000061", Role: "USER", InitialPassword: "strong-member-password"},
		{Username: "report-helper", RealName: "报表成员", StudentNumber: "202600000062", Role: "USER", InitialPassword: "strong-member-password"},
	} {
		if err := s.CreateSemesterMember(member); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := s.CreateSchedulePlan("晚班", map[string][]string{"Mon-6": {"报表值班员(单双)"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishSchedulePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveWorkStudyTemplate(buildWorkStudyTemplateDocx(t, 40)); err != nil {
		t.Fatal(err)
	}
	draft, err := s.CreateMonthlyReport(types.MonthlyReportRequest{MonthlyConditions: types.MonthlyConditions{OutputMonth: "2026-09", StartDate: "2026-09-07", EndDate: "2026-09-07"}, TargetTotal: "2300.00"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.OriginalTotal != "50.00" {
		t.Fatalf("planned fallback must be paid: %s", draft.OriginalTotal)
	}
	final, err := s.FinalizeMonthlyReport(draft.Report.ID, types.MonthlyFinalizeRequest{VersionID: draft.Version.ID})
	if err != nil {
		t.Fatal(err)
	}
	if final.Version.Status != "finalized" || final.Report.FinalVersionID != draft.Version.ID {
		t.Fatalf("final state: %+v", final)
	}
	_, csvData, err := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, "csv")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(csvData), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	daily := map[string]float64{}
	totals := map[string]float64{}
	for _, row := range rows[1:] {
		if row[1] == "合计" {
			continue
		}
		start, e := time.Parse("15:04", row[4])
		if e != nil {
			t.Fatal(e)
		}
		end, e := time.Parse("15:04", row[5])
		if e != nil {
			t.Fatal(e)
		}
		if start.Hour() < 8 || end.Hour() > 22 || (end.Hour() == 22 && end.Minute() != 0) || !start.Before(end) {
			t.Fatalf("invalid time window: %v", row)
		}
		hours, e := strconv.ParseFloat(row[6], 64)
		if e != nil {
			t.Fatal(e)
		}
		if hours != end.Sub(start).Hours() {
			t.Fatalf("time/hours mismatch: %v", row)
		}
		key := strings.Join(row[:4], "/")
		daily[key] += hours
		totals[row[0]] += hours
	}
	for date, hours := range daily {
		if hours > 8 {
			t.Fatalf("%s has %.1f hours", date, hours)
		}
	}
	if totals["报表值班员"] != 28 || totals["报表成员"] != 64 {
		t.Fatalf("declared hours: %v", totals)
	}
	_, records, err := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, "records")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateSemesterSettings("20260831", "新工作内容", RateConfig{DutyCents: 3000, WorkOrderCents: 6000, MgmtLeaderCents: 90000, MgmtOwnerCents: 130000}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveWorkStudyTemplate(buildWorkStudyTemplateDocx(t, 2)); err != nil {
		t.Fatal(err)
	}
	_, again, err := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, "records")
	if err != nil || !bytes.Equal(records, again) {
		t.Fatalf("final Word changed: %v", err)
	}
	_, againCSV, err := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, "csv")
	if err != nil || !bytes.Equal(csvData, againCSV) {
		t.Fatalf("final CSV changed: %v", err)
	}
}
