package store

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func TestSemesterLifecycleAndArchivedProtection(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	original := appStore.ActiveSemester()
	created, err := appStore.CreateSemester(types.CreateSemesterRequest{
		Name:        "2026-2027-1",
		FirstMonday: "20260907",
		CloneFromID: original.ID,
	})
	if err != nil {
		t.Fatalf("CreateSemester: %v", err)
	}
	if !created.Draft {
		t.Fatalf("new semester should be a draft: %+v", created)
	}

	activated, err := appStore.ActivateSemester(created.ID)
	if err != nil {
		t.Fatalf("ActivateSemester: %v", err)
	}
	if !activated.Active || activated.Draft {
		t.Fatalf("activated semester state is invalid: %+v", activated)
	}
	items, err := appStore.ListSemesters()
	if err != nil {
		t.Fatalf("ListSemesters: %v", err)
	}
	for _, item := range items {
		if item.ID == original.ID && !item.Archived {
			t.Fatalf("previous semester was not archived: %+v", item)
		}
	}

	if err := appStore.SetSemesterArchived(created.ID, true); err != nil {
		t.Fatalf("SetSemesterArchived: %v", err)
	}
	if err := appStore.UpdateSemesterSettings("20260907", "测试内容", DefaultRateConfig()); err != ErrArchivedSemester {
		t.Fatalf("expected ErrArchivedSemester, got %v", err)
	}
}

func TestSemesterActivationWaitsForInFlightRequest(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	original := appStore.ActiveSemester()
	created, err := appStore.CreateSemester(types.CreateSemesterRequest{
		Name:        "request-lease",
		FirstMonday: "20260907",
		CloneFromID: original.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	requestStore, release, err := appStore.AcquireRequest()
	if err != nil {
		t.Fatal(err)
	}
	activation := make(chan error, 1)
	go func() {
		_, err := appStore.ActivateSemester(created.ID)
		activation <- err
	}()

	select {
	case err := <-activation:
		release()
		t.Fatalf("activation completed before request release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if requestStore.ActiveSemester().ID != original.ID {
		release()
		t.Fatal("in-flight request changed semester")
	}

	release()
	select {
	case err := <-activation:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("activation did not resume after request release")
	}
	if appStore.ActiveSemester().ID != created.ID {
		t.Fatal("new semester was not activated")
	}
}

func TestListUsersSortsByAccountMembershipRoleAndName(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	insertUser := func(username, realName, role string, sortOrder int, member, accountActive bool) {
		t.Helper()
		accountUUID := uuid.NewString()
		if _, err := appStore.control.Exec(`
			INSERT INTO accounts (account_uuid, username, real_name, password_hash, is_active, must_change_password)
			VALUES (?, ?, ?, 'unused', ?, 0)
		`, accountUUID, username, realName, boolToInt(accountActive)); err != nil {
			t.Fatal(err)
		}
		if _, err := appStore.db.Exec(`
			INSERT INTO users (account_uuid, username, password_hash, real_name, role, sort_order, is_active, must_change_password)
			VALUES (?, ?, '', ?, ?, ?, ?, 0)
		`, accountUUID, username, realName, role, sortOrder, boolToInt(member)); err != nil {
			t.Fatal(err)
		}
	}

	insertUser("sort-owner", "Owner", "OWNER", 50, true, true)
	insertUser("sort-leader", "Leader", "LEADER", 1, true, true)
	insertUser("sort-hr", "HR", "HR", 1, true, true)
	insertUser("sort-finance", "Finance", "FINANCE", 1, true, true)
	insertUser("sort-user-low", "Zulu", "USER", 10, true, true)
	insertUser("sort-user-a", "Alpha", "USER", 20, true, true)
	insertUser("sort-user-b", "Beta", "USER", 20, true, true)
	insertUser("sort-off-owner", "Off Owner", "OWNER", 50, false, true)
	insertUser("sort-off-leader", "Off Leader", "LEADER", 1, false, true)
	insertUser("sort-off-hr", "Off HR", "HR", 1, false, true)
	insertUser("sort-off-finance", "Off Finance", "FINANCE", 1, false, true)
	insertUser("sort-off-user", "Off User", "USER", 1, false, true)
	insertUser("sort-disabled-owner", "Disabled Owner", "OWNER", 50, true, false)
	insertUser("sort-disabled-user", "Disabled User", "USER", 1, false, false)

	users, err := appStore.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(users))
	for _, user := range users {
		if user.Username == "admin" || strings.HasPrefix(user.Username, "sort-") {
			got = append(got, user.Username)
		}
	}
	want := []string{
		"admin", "sort-owner", "sort-leader", "sort-hr", "sort-finance",
		"sort-user-low", "sort-user-a", "sort-user-b",
		"sort-off-owner", "sort-off-leader", "sort-off-hr", "sort-off-finance", "sort-off-user",
		"sort-disabled-owner", "sort-disabled-user",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ListUsers order = %v, want %v", got, want)
	}
}

func TestMemberRenameKeepsStableReferences(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "member1", RealName: "旧姓名", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatalf("CreateSemesterMember: %v", err)
	}
	users, err := appStore.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	var memberID int64
	for _, user := range users {
		if user.Username == "member1" {
			memberID = user.ID
		}
	}
	if memberID == 0 {
		t.Fatal("created member not found")
	}
	if err := appStore.SaveAvailability("旧姓名", types.SaveAvailabilityRequest{Single: []string{"Mon_1"}}); err != nil {
		t.Fatalf("SaveAvailability: %v", err)
	}
	if err := appStore.UpdateSemesterMember(memberID, types.UpdateMemberRequest{RealName: "新姓名", Role: "LEADER"}); err != nil {
		t.Fatalf("UpdateSemesterMember: %v", err)
	}
	payload, err := appStore.GetAvailabilityForUser("新姓名")
	if err != nil {
		t.Fatalf("GetAvailabilityForUser: %v", err)
	}
	if len(payload.Single) != 1 || payload.Single[0] != "Mon_1" {
		t.Fatalf("renamed member lost availability: %+v", payload)
	}
}

func TestGlobalProfileIsAuthoritativeWithSemesterSnapshots(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "global-profile", RealName: "全局旧姓名", StudentNumber: "202600000010", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatalf("CreateSemesterMember: %v", err)
	}
	original := appStore.ActiveSemester()
	first, err := appStore.CreateSemester(types.CreateSemesterRequest{Name: "profile-first", FirstMonday: "20260907", CloneFromID: original.ID})
	if err != nil {
		t.Fatalf("CreateSemester: %v", err)
	}
	if _, err := appStore.ActivateSemester(first.ID); err != nil {
		t.Fatalf("ActivateSemester first: %v", err)
	}
	draft, err := appStore.CreateSemester(types.CreateSemesterRequest{Name: "profile-next", FirstMonday: "20261207", CloneFromID: first.ID})
	if err != nil {
		t.Fatalf("CreateSemester second: %v", err)
	}
	memberID := findLocalUserID(t, appStore, "global-profile")
	newNumber := "202600000011"
	if err := appStore.UpdateSemesterMember(memberID, types.UpdateMemberRequest{RealName: "全局新姓名", StudentNumber: &newNumber, Role: "USER"}); err != nil {
		t.Fatalf("UpdateSemesterMember: %v", err)
	}
	var globalName, globalNumber string
	if err := appStore.control.QueryRow(`SELECT real_name, student_number FROM accounts WHERE username = 'global-profile'`).Scan(&globalName, &globalNumber); err != nil {
		t.Fatal(err)
	}
	if globalName != "全局新姓名" || globalNumber != newNumber {
		t.Fatalf("global profile not updated: name=%q number=%q", globalName, globalNumber)
	}
	var draftName, draftNumber string
	draftDB, err := sql.Open("sqlite", draft.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := draftDB.QueryRow(`SELECT real_name, student_number FROM users WHERE username = 'global-profile'`).Scan(&draftName, &draftNumber); err != nil {
		draftDB.Close()
		t.Fatal(err)
	}
	draftDB.Close()
	if draftName != "全局旧姓名" || draftNumber != "202600000010" {
		t.Fatalf("draft snapshot unexpectedly changed: name=%q number=%q", draftName, draftNumber)
	}
	if _, err := appStore.ActivateSemester(draft.ID); err != nil {
		t.Fatalf("ActivateSemester: %v", err)
	}
	users, err := appStore.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user.Username == "global-profile" && (user.RealName != "全局新姓名" || user.StudentNumber != newNumber) {
			t.Fatalf("active semester did not use global profile: %+v", user)
		}
	}
	archivedDB, err := sql.Open("sqlite", original.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer archivedDB.Close()
	if err := archivedDB.QueryRow(`SELECT real_name, student_number FROM users WHERE username = 'global-profile'`).Scan(&draftName, &draftNumber); err != nil {
		t.Fatal(err)
	}
	if draftName != "全局旧姓名" || draftNumber != "202600000010" {
		t.Fatalf("archived snapshot was rewritten: name=%q number=%q", draftName, draftNumber)
	}
}

func TestMemberSoftRemoveAndRestoreKeepsAccountAndHistory(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "soft-member", RealName: "软移除成员", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatalf("CreateSemesterMember: %v", err)
	}
	memberID := findLocalUserID(t, appStore, "soft-member")
	if err := appStore.SaveAvailability("软移除成员", types.SaveAvailabilityRequest{Single: []string{"Tue_2"}}); err != nil {
		t.Fatalf("SaveAvailability: %v", err)
	}

	var accountUUID, semesterAccountUUID string
	if err := appStore.control.QueryRow(`SELECT account_uuid FROM accounts WHERE username = 'soft-member'`).Scan(&accountUUID); err != nil {
		t.Fatalf("load global account: %v", err)
	}
	if _, err := uuid.Parse(accountUUID); err != nil {
		t.Fatalf("invalid global account UUID %q: %v", accountUUID, err)
	}
	if err := appStore.db.QueryRow(`SELECT account_uuid FROM users WHERE id = ?`, memberID).Scan(&semesterAccountUUID); err != nil {
		t.Fatalf("load semester membership: %v", err)
	}
	if semesterAccountUUID != accountUUID {
		t.Fatalf("semester membership UUID %q does not match account UUID %q", semesterAccountUUID, accountUUID)
	}

	if err := appStore.RemoveSemesterMember(memberID); err != nil {
		t.Fatalf("RemoveSemesterMember: %v", err)
	}
	if _, err := appStore.Authenticate("soft-member", "strong-member-password"); err == nil {
		t.Fatal("removed member should not authenticate into the current semester")
	}
	var accountCount int
	if err := appStore.control.QueryRow(`SELECT COUNT(*) FROM accounts WHERE account_uuid = ? AND is_active = 1`, accountUUID).Scan(&accountCount); err != nil {
		t.Fatal(err)
	}
	if accountCount != 1 {
		t.Fatalf("global account was removed or disabled: count=%d", accountCount)
	}
	payload, err := appStore.GetAvailabilityForUser("软移除成员")
	if err != nil {
		t.Fatalf("GetAvailabilityForUser after removal: %v", err)
	}
	if len(payload.Single) != 1 || payload.Single[0] != "Tue_2" {
		t.Fatalf("soft removal lost historical availability: %+v", payload)
	}

	if err := appStore.RestoreSemesterMember(memberID); err != nil {
		t.Fatalf("RestoreSemesterMember: %v", err)
	}
	if _, err := appStore.Authenticate("soft-member", "strong-member-password"); err != nil {
		t.Fatalf("restored member could not authenticate: %v", err)
	}
}

func TestRemovedMemberExcludedFromCurrentOperationsAndExports(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	for _, member := range []types.CreateMemberRequest{
		{Username: "active-member", RealName: "当前成员", Role: "USER", InitialPassword: "strong-member-password"},
		{Username: "removed-member", RealName: "已移出成员", Role: "LEADER", InitialPassword: "strong-member-password"},
	} {
		if err := appStore.CreateSemesterMember(member); err != nil {
			t.Fatalf("CreateSemesterMember(%s): %v", member.Username, err)
		}
	}

	for _, name := range []string{"当前成员", "已移出成员"} {
		if err := appStore.SaveAvailability(name, types.SaveAvailabilityRequest{Single: []string{"Mon-1"}}); err != nil {
			t.Fatalf("SaveAvailability(%s): %v", name, err)
		}
	}
	if err := appStore.SaveSchedule(map[string][]string{
		"Mon-1": {"当前成员(单双)", "已移出成员(单双)"},
	}); err != nil {
		t.Fatalf("SaveSchedule: %v", err)
	}

	selectedDate := "2026-06-08"
	date, err := time.Parse("2006-01-02", selectedDate)
	if err != nil {
		t.Fatal(err)
	}
	weekNumber := calculateWeekNumber(date, appStore.cfg.FirstMonday)
	if err := appStore.SaveFinalSchedule(weekNumber, types.SaveFinalScheduleRequest{
		SelectedDate: selectedDate,
		Schedule: map[string][]string{
			"Mon-1": {"当前成员", "已移出成员"},
		},
	}, "系统管理员"); err != nil {
		t.Fatalf("SaveFinalSchedule: %v", err)
	}

	workOrder, err := appStore.CreateWorkOrder(types.SaveWorkOrderRequest{
		Title:          "成员过滤测试",
		BelongingMonth: "2026-06",
		WorkSessions: []types.WorkSession{
			{Date: selectedDate, WorkerName: "当前成员", Duration: 1},
			{Date: selectedDate, WorkerName: "已移出成员", Duration: 2},
		},
	}, "系统管理员")
	if err != nil {
		t.Fatalf("CreateWorkOrder: %v", err)
	}

	removedID := findLocalUserID(t, appStore, "removed-member")
	if err := appStore.RemoveSemesterMember(removedID); err != nil {
		t.Fatalf("RemoveSemesterMember: %v", err)
	}

	schedule, err := appStore.GetSchedule()
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	assertContainsOnlyCurrentMember(t, schedule["Mon-1"])

	finalSchedule, err := appStore.GetFinalSchedule(weekNumber, selectedDate)
	if err != nil {
		t.Fatalf("GetFinalSchedule: %v", err)
	}
	assertContainsOnlyCurrentMember(t, finalSchedule.Schedule["Mon-1"])
	availability, err := appStore.GetAvailabilityOverview()
	if err != nil {
		t.Fatalf("GetAvailabilityOverview: %v", err)
	}
	if len(availability) != 1 || availability[0].RealName != "当前成员" {
		t.Fatalf("availability overview still contains removed members: %+v", availability)
	}

	if err := appStore.SaveSchedule(map[string][]string{"Tue-1": {"已移出成员(单双)"}}); err == nil {
		t.Fatal("SaveSchedule accepted a removed member")
	}
	if err := appStore.SaveFinalSchedule(weekNumber, types.SaveFinalScheduleRequest{
		SelectedDate: selectedDate,
		Schedule:     map[string][]string{"Tue-1": {"已移出成员"}},
	}, "系统管理员"); err == nil {
		t.Fatal("SaveFinalSchedule accepted a removed member")
	}
	if _, err := appStore.CreateWorkOrder(types.SaveWorkOrderRequest{
		Title:          "非法工单",
		BelongingMonth: "2026-06",
		WorkSessions:   []types.WorkSession{{Date: selectedDate, WorkerName: "已移出成员", Duration: 1}},
	}, "系统管理员"); err == nil {
		t.Fatal("CreateWorkOrder accepted a removed member")
	}
	if err := appStore.SaveAvailability("已移出成员", types.SaveAvailabilityRequest{Single: []string{"Tue-1"}}); err == nil {
		t.Fatal("SaveAvailability accepted a removed member")
	}

	assertHistoricalMemberRowsKept(t, appStore, workOrder.ID)

	financeContent, err := appStore.ExportFinanceWorkbookForRange(selectedDate, selectedDate, []string{workOrder.ID}, false, 0)
	if err != nil {
		t.Fatalf("ExportFinanceWorkbookForRange: %v", err)
	}
	assertWorkbookMemberNames(t, financeContent, "财务统计")

	workOrderContent, err := appStore.ExportWorkOrdersWorkbook("2026-06")
	if err != nil {
		t.Fatalf("ExportWorkOrdersWorkbook: %v", err)
	}
	assertWorkbookMemberNames(t, workOrderContent, "2026-06")

	csvContent, err := appStore.ExportDutyCSVForRange(selectedDate, selectedDate, "2026-06", []string{workOrder.ID}, false, 0)
	if err != nil {
		t.Fatalf("ExportDutyCSVForRange: %v", err)
	}
	assertCSVMemberNames(t, csvContent)

	dashboard, err := appStore.GetDashboard(true)
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if dashboard.AvailabilityUserCount != 1 {
		t.Fatalf("dashboard availability count = %d, want 1", dashboard.AvailabilityUserCount)
	}
	for _, item := range dashboard.WorkDurationShare {
		if item.Name == "已移出成员" {
			t.Fatalf("dashboard still contains removed member: %+v", dashboard.WorkDurationShare)
		}
	}
}

func assertContainsOnlyCurrentMember(t *testing.T, names []string) {
	t.Helper()
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "当前成员") || strings.Contains(joined, "已移出成员") {
		t.Fatalf("current operation names = %v", names)
	}
}

func assertHistoricalMemberRowsKept(t *testing.T, appStore *Store, workOrderID string) {
	t.Helper()
	checks := []struct {
		query string
		args  []any
	}{
		{query: `SELECT COUNT(*) FROM availability_entries WHERE real_name = ?`, args: []any{"已移出成员"}},
		{query: `SELECT COUNT(*) FROM schedule_entries WHERE real_name = ?`, args: []any{"已移出成员"}},
		{query: `SELECT COUNT(*) FROM final_schedule_entries WHERE real_name = ?`, args: []any{"已移出成员"}},
		{query: `SELECT COUNT(*) FROM work_sessions WHERE work_order_id = ? AND worker_name = ?`, args: []any{workOrderID, "已移出成员"}},
	}
	for _, check := range checks {
		var count int
		if err := appStore.db.QueryRow(check.query, check.args...).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("historical row count = %d for %s", count, check.query)
		}
	}
}

func assertWorkbookMemberNames(t *testing.T, content []byte, sheetName string) {
	t.Helper()
	file, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer file.Close()
	rows, err := file.GetRows(sheetName)
	if err != nil {
		t.Fatalf("read workbook rows: %v", err)
	}
	flattened := make([]string, 0)
	for _, row := range rows {
		flattened = append(flattened, row...)
	}
	joined := strings.Join(flattened, "|")
	if !strings.Contains(joined, "当前成员") || strings.Contains(joined, "已移出成员") {
		t.Fatalf("workbook %s member cells = %s", sheetName, joined)
	}
}

func assertCSVMemberNames(t *testing.T, content []byte) {
	t.Helper()
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	joinedRows := make([]string, 0, len(rows))
	for _, row := range rows {
		joinedRows = append(joinedRows, strings.Join(row, "|"))
	}
	joined := strings.Join(joinedRows, "\n")
	if !strings.Contains(joined, "当前成员") || strings.Contains(joined, "已移出成员") {
		t.Fatalf("CSV member rows = %s", joined)
	}
}

func TestSemesterContextVersionChangesOnLifecycleUpdates(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	originalVersion := appStore.ActiveSemester().ContextVersion
	created, err := appStore.CreateSemester(types.CreateSemesterRequest{
		Name: "context-next", FirstMonday: "20260907", CloneFromID: appStore.ActiveSemester().ID,
	})
	if err != nil {
		t.Fatalf("CreateSemester: %v", err)
	}
	activated, err := appStore.ActivateSemester(created.ID)
	if err != nil {
		t.Fatalf("ActivateSemester: %v", err)
	}
	if activated.ContextVersion <= originalVersion {
		t.Fatalf("context version did not advance on activation: before=%d after=%d", originalVersion, activated.ContextVersion)
	}
	activatedVersion := activated.ContextVersion
	if err := appStore.SetSemesterArchived(created.ID, true); err != nil {
		t.Fatalf("SetSemesterArchived: %v", err)
	}
	if appStore.ActiveSemester().ContextVersion <= activatedVersion {
		t.Fatalf("context version did not advance on active semester archive")
	}
}

func TestSemesterCloneKeepsMembershipStatusRoleAndOrder(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	for _, member := range []types.CreateMemberRequest{
		{Username: "clone-a", RealName: "克隆成员甲", StudentNumber: "202600000001", Role: "USER", InitialPassword: "strong-member-password"},
		{Username: "clone-b", RealName: "克隆成员乙", StudentNumber: "202600000002", Role: "LEADER", InitialPassword: "strong-member-password"},
	} {
		if err := appStore.CreateSemesterMember(member); err != nil {
			t.Fatalf("CreateSemesterMember(%s): %v", member.Username, err)
		}
	}
	memberA := findLocalUserID(t, appStore, "clone-a")
	memberB := findLocalUserID(t, appStore, "clone-b")
	orderA, orderB := 50, 10
	if err := appStore.UpdateSemesterMember(memberA, types.UpdateMemberRequest{RealName: "克隆成员甲", Role: "USER", SortOrder: &orderA}); err != nil {
		t.Fatalf("UpdateSemesterMember A: %v", err)
	}
	if err := appStore.UpdateSemesterMember(memberB, types.UpdateMemberRequest{RealName: "克隆成员乙", Role: "LEADER", SortOrder: &orderB}); err != nil {
		t.Fatalf("UpdateSemesterMember B: %v", err)
	}
	if err := appStore.RemoveSemesterMember(memberA); err != nil {
		t.Fatalf("RemoveSemesterMember: %v", err)
	}

	created, err := appStore.CreateSemester(types.CreateSemesterRequest{
		Name: "clone-target", FirstMonday: "20260907", CloneFromID: appStore.ActiveSemester().ID,
	})
	if err != nil {
		t.Fatalf("CreateSemester: %v", err)
	}
	db, err := sql.Open("sqlite", created.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var removedActive, removedOrder int
	var removedRole string
	if err := db.QueryRow(`SELECT is_active, role, sort_order FROM users WHERE username = 'clone-a'`).Scan(&removedActive, &removedRole, &removedOrder); err != nil {
		t.Fatal(err)
	}
	if removedActive != 0 || removedRole != "USER" || removedOrder != orderA {
		t.Fatalf("removed membership not preserved: active=%d role=%s order=%d", removedActive, removedRole, removedOrder)
	}
	var clonedRole, clonedStudentNumber string
	var clonedOrder int
	if err := db.QueryRow(`SELECT role, student_number, sort_order FROM users WHERE username = 'clone-b'`).Scan(&clonedRole, &clonedStudentNumber, &clonedOrder); err != nil {
		t.Fatal(err)
	}
	if clonedRole != "LEADER" || clonedStudentNumber != "202600000002" || clonedOrder != orderB {
		t.Fatalf("active member data not preserved: role=%s studentNumber=%s order=%d", clonedRole, clonedStudentNumber, clonedOrder)
	}
}

func TestGlobalTemplateSurvivesSemesterSwitch(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "template-user", RealName: "模板成员", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatalf("CreateSemesterMember: %v", err)
	}
	if _, err := appStore.SaveWorkStudyTemplate(buildWorkStudyTemplateDocx(t, 2)); err != nil {
		t.Fatalf("SaveWorkStudyTemplate: %v", err)
	}
	created, err := appStore.CreateSemester(types.CreateSemesterRequest{Name: "next", FirstMonday: "20260907", CloneFromID: appStore.ActiveSemester().ID})
	if err != nil {
		t.Fatalf("CreateSemester: %v", err)
	}
	if _, err := appStore.ActivateSemester(created.ID); err != nil {
		t.Fatalf("ActivateSemester: %v", err)
	}
	item, err := appStore.WorkStudyTemplateStatus()
	if err != nil {
		t.Fatalf("WorkStudyTemplateStatus: %v", err)
	}
	if !item.Exists || item.Filename != workStudyGlobalTemplateFilename {
		t.Fatal("global template was not available after semester switch")
	}
}

func TestSemesterExportImport(t *testing.T) {
	first := newTestManagedStore(t)
	defer first.Close()
	filename, content, err := first.ExportSemester(first.ActiveSemester().ID)
	if err != nil {
		t.Fatalf("ExportSemester: %v", err)
	}
	if filepath.Ext(filename) != ".db" || len(content) == 0 {
		t.Fatalf("invalid export: %s, %d bytes", filename, len(content))
	}

	second := newTestManagedStore(t)
	defer second.Close()
	imported, err := second.ImportSemester(content)
	if err != nil {
		t.Fatalf("ImportSemester: %v", err)
	}
	if !imported.Archived {
		t.Fatalf("imported semester must be archived: %+v", imported)
	}
}

func newTestManagedStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "backend", ".env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("APP_PORT=3000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{
		Port:                 "3000",
		ControlDatabasePath:  filepath.Join(dir, "data", "control.db"),
		SemesterDatabaseDir:  filepath.Join(dir, "data", "semesters"),
		JWTSecret:            "test-secret",
		AdminPassword:        "admin-password",
		FirstMonday:          "20260302",
		EnvFilePath:          envPath,
		WorkStudyTemplateDir: filepath.Join(dir, "templates"),
		WorkStudyContent:     "测试工作",
	}
	appStore, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return appStore
}

func findLocalUserID(t *testing.T, appStore *Store, username string) int64 {
	t.Helper()
	users, err := appStore.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user.Username == username {
			return user.ID
		}
	}
	t.Fatalf("user %s not found", username)
	return 0
}
