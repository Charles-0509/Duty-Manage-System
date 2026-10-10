package store

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"math"
	"os"
	"path/filepath"
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
	tail, err := AllocateMonthlyReport(MonthlySnapshot{Members: []MonthlyMember{{ID: 1, Name: "尾差成员"}}}, 5010, []int64{1})
	if err != nil || tail.Declared != 5000 || tail.Unallocated != 10 {
		t.Fatalf("non-25 target must keep incomplete draft: %+v %v", tail, err)
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
	snapshot, distribution, _, _, err := s.readMonthlyVersion(draft.Report.ID, draft.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	csvData, err := monthlyCSV(snapshot, distribution)
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
		if start.Hour() < 8 || end.Hour() > 22 || (end.Hour() == 22 && end.Minute() > 30) || !start.Before(end) {
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
	for _, kind := range []string{"csv", "finance", "finance-csv", "package"} {
		if _, _, e := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, kind); !errors.Is(e, sql.ErrNoRows) {
			t.Fatalf("removed %s output still available: %v", kind, e)
		}
	}
}

func TestMonthlyPreflightRejectsDuplicateIdentitiesAndSmallTemplate(t *testing.T) {
	snapshot := MonthlySnapshot{Conditions: types.MonthlyConditions{OutputMonth: "2026-09"}, Members: []MonthlyMember{{ID: 1, Name: "重复姓名", StudentNumber: "202600000081"}, {ID: 2, Name: "重复姓名", StudentNumber: "202600000082"}}}
	distribution, err := AllocateMonthlyReport(snapshot, 160000, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	issues, _ := monthlyChecks(snapshot, distribution, buildWorkStudyTemplateDocx(t, 1))
	if len(issues) == 0 {
		t.Fatal("duplicate identities or insufficient template rows were accepted")
	}
	snapshot.Members = []MonthlyMember{{ID: 1, Name: "边界成员", StudentNumber: "202600000081"}}
	snapshot.Duty = []MonthlyDuty{{MemberID: 1, Date: "2026-08-31", Start: "20:10", End: "22:10"}, {MemberID: 1, Date: "2026-09-30", Start: "20:10", End: "22:10"}}
	distribution, err = AllocateMonthlyReport(snapshot, 195000, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	// Explicit manual exception at the hard cap remains schedulable across a mapped month.
	distribution, err = AdjustMonthlyReport(snapshot, map[int64]int64{1: 195000}, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	issues, data := monthlyChecks(snapshot, distribution, buildWorkStudyTemplateDocx(t, 40))
	if len(issues) > 0 || !strings.Contains(string(data), ",2026,9,") {
		t.Fatalf("cross-month mapping/hard cap: %v", issues)
	}
	issues, _ = monthlyChecks(snapshot, distribution, buildWorkStudyTemplateDocx(t, 1))
	if len(issues) == 0 {
		t.Fatal("short template accepted")
	}
}

func TestMonthlyDeclarationPreservesLateShiftAndAvoidsShortTails(t *testing.T) {
	for _, test := range []struct {
		name   string
		amount int64
		duty   []MonthlyDuty
	}{
		{"late shift", 5000, []MonthlyDuty{{MemberID: 1, Date: "2026-10-09", Start: "20:10", End: "22:10"}}},
		{"adjacent evening shifts", 10000, []MonthlyDuty{{MemberID: 1, Date: "2026-10-09", Start: "18:10", End: "20:10"}, {MemberID: 1, Date: "2026-10-09", Start: "20:10", End: "22:10"}}},
		{"five hour remainder", 12500, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			member := MonthlyMember{ID: 1, Name: "排时成员", StudentNumber: "202600000091"}
			snapshot := MonthlySnapshot{Conditions: types.MonthlyConditions{OutputMonth: "2026-10"}, Members: []MonthlyMember{member}, Duty: test.duty}
			data, err := monthlyCSV(snapshot, MonthlyDistribution{Rows: []MonthlyAllocatedMember{{MonthlyMember: member, Declared: test.amount}}})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff"))).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			var hours float64
			for _, row := range rows[1:] {
				if row[1] == "合计" {
					continue
				}
				h, _ := strconv.ParseFloat(row[6], 64)
				if h < 2 || h != math.Trunc(h) {
					t.Fatalf("short/fractional declaration: %v", row)
				}
				hours += h
			}
			if int64(hours*2500) != test.amount {
				t.Fatalf("hours do not match amount: %s", data)
			}
			if test.name != "five hour remainder" && !strings.Contains(string(data), "22:10") {
				t.Fatalf("actual late shift was truncated: %s", data)
			}
		})
	}
}

func TestMonthlyDraftDownloadsSaveAndReplaceOnlyThreeOutputs(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "output-helper", RealName: "报表成员", StudentNumber: "202600000092", Role: "USER", InitialPassword: "strong-member-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveWorkStudyTemplate(buildWorkStudyTemplateDocx(t, 40)); err != nil {
		t.Fatal(err)
	}
	draft, err := s.CreateMonthlyReport(types.MonthlyReportRequest{MonthlyConditions: types.MonthlyConditions{OutputMonth: "2026-10", StartDate: "2026-10-01", EndDate: "2026-10-31"}, TargetTotal: "100.00"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Version.Status != "draft" || len(draft.Files) != 3 {
		t.Fatalf("draft outputs: %+v", draft)
	}
	for kind, want := range map[string]string{"labor": "2026年10月劳务计算.xlsx", "work-study": "2026年10月劳务勤助转换.xlsx", "records": "10月勤工助学记录表.zip"} {
		name, data, e := s.DownloadMonthlyFile(draft.Report.ID, draft.Version.ID, kind)
		if e != nil || name != want || len(data) == 0 {
			t.Fatalf("draft %s: %s %v", kind, name, e)
		}
	}
	cache, err := s.monthlyCachePath(draft.Report.ID)
	if err != nil {
		t.Fatal(err)
	}
	wordPath := filepath.Join(cache, "records", "报表成员_勤工助学学生工作记录表.docx")
	before, err := os.ReadFile(wordPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := []types.MonthlyAmountRow{{MemberID: draft.Rows[0].MemberID, Amount: "125.00"}}
	revised, err := s.NewMonthlyVersion(draft.Report.ID, types.MonthlyVersionRequest{BaseVersionID: draft.Version.ID, Mode: "manual", Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(wordPath)
	if err != nil || bytes.Equal(before, after) {
		t.Fatalf("Word cache was not replaced: %v", err)
	}
	var packet, finance, csv []byte
	if err = s.db.QueryRow(`SELECT package_blob FROM monthly_report_versions WHERE id=?`, revised.Version.ID).Scan(&packet); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT finance_xlsx_blob,finance_csv_blob FROM monthly_report_sources WHERE id=(SELECT source_id FROM monthly_report_versions WHERE id=?)`, revised.Version.ID).Scan(&finance, &csv); err != nil {
		t.Fatal(err)
	}
	if len(packet)+len(finance)+len(csv) != 0 {
		t.Fatal("unused ZIP/financial/CSV outputs retained")
	}
	if _, err = s.FinalizeMonthlyReport(revised.Report.ID, types.MonthlyFinalizeRequest{VersionID: revised.Version.ID}); err != nil {
		t.Fatal(err)
	}
	rows[0].Amount = "25.00"
	invalid, err := s.NewMonthlyVersion(draft.Report.ID, types.MonthlyVersionRequest{BaseVersionID: revised.Version.ID, Mode: "manual", Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	if len(invalid.Issues) == 0 || len(invalid.Files) != 0 {
		t.Fatal("one-hour declaration silently rounded or stale outputs kept")
	}
	if _, err = os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("invalid new revision kept stale Word files: %v", err)
	}
}
