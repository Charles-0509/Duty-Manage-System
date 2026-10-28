package store

import (
	"math"
	"testing"

	"personnel-management-go/internal/types"
)

func TestFinanceMembersMatchIndividualQueriesAndCurrentFilters(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	for _, member := range []types.CreateMemberRequest{
		{Username: "pay-owner", RealName: "负责人", Role: "OWNER"},
		{Username: "pay-worker", RealName: "值班人员", Role: "USER"},
		{Username: "pay-hr", RealName: "人事", Role: "HR"},
		{Username: "pay-finance", RealName: "财务", Role: "FINANCE"},
		{Username: "pay-removed", RealName: "移出成员", Role: "USER"},
		{Username: "pay-viewer", RealName: "只读访客", Role: "VIEWER"},
	} {
		member.InitialPassword = "strong-member-password"
		if err := s.CreateSemesterMember(member); err != nil {
			t.Fatal(err)
		}
	}
	removed, _ := s.GetUserByUsername("pay-removed")
	if err := s.RemoveSemesterMember(removed.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := s.CreateSchedulePlan("薪酬统计计划", map[string][]string{"Mon-1": {"负责人(单双)", "值班人员(单双)"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishSchedulePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	order, err := s.CreateWorkOrder(types.SaveWorkOrderRequest{Title: "计入工单", BelongingMonth: "2026-09", WorkSessions: []types.WorkSession{
		{Date: "2026-09-20", WorkerName: "负责人", Duration: 1},
		{Date: "2026-09-20", WorkerName: "值班人员", Duration: 3},
	}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWorkOrder(types.SaveWorkOrderRequest{Title: "未选工单", BelongingMonth: "2026-09", WorkSessions: []types.WorkSession{{Date: "2026-09-07", WorkerName: "值班人员", Duration: 10}}}, "admin"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		start, end string
		include    bool
		months     int
		orders     []string
	}{{"2026-09-07", "2026-09-08", true, 1, []string{order.ID}}, {"2026-09-07", "2026-09-08", false, 1, nil}, {"2026-09-08", "2026-09-08", true, 2, []string{order.ID}}} {
		summary, err := s.GetFinanceSummaryForRange(test.start, test.end, test.orders, test.include, test.months, "", "ADMIN")
		if err != nil {
			t.Fatal(err)
		}
		if len(summary.MemberSummaries) != 4 {
			t.Fatalf("summary includes a removed/admin/viewer account: %+v", summary.MemberSummaries)
		}
		users, _ := s.ListUsers()
		roles := map[string]string{}
		for _, user := range users {
			roles[user.RealName] = user.Role
		}
		var totals types.FinanceMemberSummary
		for _, member := range summary.MemberSummaries {
			personal, err := s.GetFinanceSummaryForRange(test.start, test.end, test.orders, test.include, test.months, member.RealName, roles[member.RealName])
			if err != nil {
				t.Fatal(err)
			}
			if len(personal.MemberSummaries) != 0 || member.DutyAmount != personal.DutyAmount || member.WorkOrderAmount != personal.WorkOrderAmount || member.ManagementAmount != personal.ManagementAmount || member.TotalAmount != personal.TotalAmount {
				t.Fatalf("member row differs from its personal summary: %+v, %+v", member, personal)
			}
			if member.RealName == "值班人员" && member.ManagementAmount != 0 {
				t.Fatalf("zero management pay must remain zero: %+v", member)
			}
			totals.DutyAmount += member.DutyAmount
			totals.WorkOrderAmount += member.WorkOrderAmount
			totals.ManagementAmount += member.ManagementAmount
			totals.TotalAmount += member.TotalAmount
		}
		if math.Abs(totals.DutyAmount-summary.DutyAmount) > 1e-9 || math.Abs(totals.WorkOrderAmount-summary.WorkOrderAmount) > 1e-9 || math.Abs(totals.ManagementAmount-summary.ManagementAmount) > 1e-9 || math.Abs(totals.TotalAmount-summary.TotalAmount) > 1e-9 {
			t.Fatalf("member totals differ from overall summary: %+v, %+v", totals, summary)
		}
	}
	first, err := s.GetFinanceSummaryForRange("2026-09-07", "2026-09-08", []string{order.ID}, true, 1, "", "ADMIN")
	if err != nil || first.DutyAmount != 100 || first.WorkOrderAmount != 200 || first.ManagementAmount != 2000 || first.TotalAmount != 2300 {
		t.Fatalf("incorrect filtered pay: %+v, %v", first, err)
	}
}
