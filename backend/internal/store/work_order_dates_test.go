package store

import (
	"errors"
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestWorkOrderRejectsBadDatesWithoutDroppingOrChangingSessions(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "date-worker", RealName: "日期测试成员", Role: "USER", InitialPassword: "strong-member-password"}); err != nil {
		t.Fatal(err)
	}
	valid := types.SaveWorkOrderRequest{Title: "日期测试工单", BelongingMonth: "2026-10", WorkSessions: []types.WorkSession{{Date: "2026-10-12", WorkerName: "日期测试成员", Duration: 2}}}
	order, err := s.CreateWorkOrder(valid, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"", "  ", "Invalid Date", "2026-02-30", "2026-13-01", "2026-10-12T00:00:00Z"} {
		t.Run(date, func(t *testing.T) {
			request := valid
			request.WorkSessions = append(append([]types.WorkSession{}, valid.WorkSessions...), types.WorkSession{Date: date, WorkerName: "日期测试成员", Duration: 1})
			for _, save := range []func() error{
				func() error { _, err := s.CreateWorkOrder(request, "admin"); return err },
				func() error { _, err := s.UpdateWorkOrder(order.ID, request); return err },
			} {
				if err := save(); err == nil || !strings.Contains(err.Error(), "第2条") || !strings.Contains(err.Error(), "日期") {
					t.Fatalf("bad date %q should identify its row: %v", date, err)
				}
			}
			var orders, sessions, allSessions int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_orders`).Scan(&orders); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_sessions WHERE work_order_id=? AND date='2026-10-12' AND duration=2`, order.ID).Scan(&sessions); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_sessions`).Scan(&allSessions); err != nil {
				t.Fatal(err)
			}
			if orders != 1 || sessions != 1 || allSessions != 1 {
				t.Fatalf("invalid save changed the existing order: orders=%d validSessions=%d allSessions=%d", orders, sessions, allSessions)
			}
		})
	}
}

func TestMonthlyPreviewReportsAllInvalidLegacyDates(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	var sessions []types.WorkSession
	for i, name := range []string{"成员甲", "成员乙", "成员丙"} {
		username := []string{"legacy-a", "legacy-b", "legacy-c"}[i]
		if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: username, RealName: name, Role: "USER", InitialPassword: "strong-member-password"}); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, types.WorkSession{Date: "2026-10-12", WorkerName: name, Duration: 1})
	}
	order, err := s.CreateWorkOrder(types.SaveWorkOrderRequest{Title: "安装cadence", BelongingMonth: "2026-10", WorkSessions: sessions}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE work_sessions SET date='Invalid Date' WHERE work_order_id=?`, order.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.PreviewMonthlyReport(types.MonthlyConditions{OutputMonth: "2026-10", StartDate: "2026-10-01", EndDate: "2026-10-31", WorkOrderIDs: []string{order.ID}})
	var validation MonthlyValidationError
	if !errors.As(err, &validation) || len(validation.Issues) != 3 {
		t.Fatalf("expected three actionable issues instead of a generic 500: %v", err)
	}
	for _, name := range []string{"成员甲", "成员乙", "成员丙"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("missing affected member %s: %v", name, err)
		}
	}
	if !strings.Contains(err.Error(), "安装cadence") || !strings.Contains(err.Error(), "Invalid Date") {
		t.Fatalf("missing order/date details: %v", err)
	}
}
