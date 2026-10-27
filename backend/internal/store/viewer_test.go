package store

import (
	"slices"
	"testing"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"
)

func TestViewerIsNotADutyOrMonthlyReportMember(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "public", RealName: "只读访客", Role: "VIEWER", InitialPassword: "admin@"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(config.MemberNames(), "只读访客") {
		t.Fatal("viewer leaked into member metadata")
	}
	availability, err := s.GetAvailabilityOverview()
	if err != nil || len(availability) != 0 {
		t.Fatalf("viewer appeared in duty candidates: %+v, %v", availability, err)
	}
	finance, err := s.financeSummaryUsers()
	if err != nil || len(finance) != 0 {
		t.Fatalf("viewer appeared in finance: %+v, %v", finance, err)
	}
	snapshot, err := s.readMonthlySnapshot(types.MonthlyConditions{OutputMonth: "2026-10", StartDate: "2026-10-01", EndDate: "2026-10-31"})
	if err != nil || len(snapshot.Members) != 0 {
		t.Fatalf("viewer appeared in monthly report: %+v, %v", snapshot.Members, err)
	}
	if err := s.SaveAvailability("只读访客", types.SaveAvailabilityRequest{}); err == nil {
		t.Fatal("viewer was accepted as a duty member")
	}
	if _, err := s.CreateSchedulePlan("invalid-viewer", map[string][]string{"Mon-1": {"只读访客"}}); err == nil {
		t.Fatal("viewer was accepted in a schedule")
	}
	if err := s.validateCurrentSemesterMemberNames([]string{"只读访客"}); err == nil {
		t.Fatal("viewer was accepted as a work-order member")
	}
	users, err := s.ListUsers()
	if err != nil || len(users) != 2 || users[len(users)-1].Role != "VIEWER" {
		t.Fatalf("viewer is missing or not lowest in user management: %+v, %v", users, err)
	}
}

func TestViewerPasswordPolicyResetAndSemesterClone(t *testing.T) {
	s := newTestManagedStore(t)
	defer s.Close()
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "ordinary", RealName: "值班人员", Role: "USER", InitialPassword: "admin@"}); err == nil {
		t.Fatal("ordinary account accepted the shared viewer password policy")
	}
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "public", RealName: "只读访客", Role: "VIEWER", InitialPassword: "admin@"}); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.Authenticate("public", "admin@")
	if err != nil || viewer.MustChangePassword || viewer.Role != "VIEWER" {
		t.Fatalf("viewer cannot sign in: %+v, %v", viewer, err)
	}
	if err := s.UpdateSemesterMember(viewer.ID, types.UpdateMemberRequest{RealName: "只读访客", Role: "USER"}); err == nil {
		t.Fatal("shared viewer account could be promoted to a business role")
	}
	if _, err := s.ResetPassword(viewer.ID, "viewer@"); err != nil {
		t.Fatal(err)
	}
	viewer, err = s.Authenticate("public", "viewer@")
	if err != nil || viewer.MustChangePassword {
		t.Fatalf("admin reset locked viewer behind password change: %+v, %v", viewer, err)
	}
	semester, err := s.CreateSemester(types.CreateSemesterRequest{Name: "next-viewer-semester", FirstMonday: "20260907"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ActivateSemester(semester.ID); err != nil {
		t.Fatal(err)
	}
	viewer, err = s.Authenticate("public", "viewer@")
	if err != nil || viewer.Role != "VIEWER" || viewer.MustChangePassword {
		t.Fatalf("cloning changed viewer permissions: %+v, %v", viewer, err)
	}
	if names, err := s.currentSemesterMemberNames(); err != nil || len(names) != 0 {
		t.Fatalf("cloned viewer appeared as a duty member: %v, %v", names, err)
	}
}
