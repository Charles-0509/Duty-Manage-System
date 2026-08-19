package store

import (
	"bytes"
	"encoding/json"
	"testing"

	"personnel-management-go/internal/types"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func TestPersonalRecordsUseStableMemberAndLaborSnapshotIdentity(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	studentNumber := "202600000123"
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "personal-member", RealName: "个人旧姓名", StudentNumber: studentNumber,
		Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatal(err)
	}
	memberID := findLocalUserID(t, appStore, "personal-member")
	account, err := appStore.GetUserByUsername("personal-member")
	if err != nil {
		t.Fatal(err)
	}
	if err := appStore.SaveFinalSchedule(2, types.SaveFinalScheduleRequest{
		SelectedDate: "2026-03-12",
		Schedule:     map[string][]string{"Tue-2": {"个人旧姓名"}},
	}, "系统管理员"); err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.CreateWorkOrder(types.SaveWorkOrderRequest{
		Title:          "个人工单",
		BelongingMonth: "2026-04",
		WorkSessions: []types.WorkSession{
			{Date: "2026-04-08", WorkerName: "个人旧姓名", Duration: 2.5},
		},
	}, "系统管理员"); err != nil {
		t.Fatal(err)
	}

	peoplePayload, err := json.Marshal([]laborPerson{{
		Name: "个人旧姓名", StudentNumber: studentNumber, Original: 50000, Adjusted: 75000,
		Remarks: []string{"历史快照"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	historyID := uuid.NewString()
	if _, err := appStore.db.Exec(`
		INSERT INTO labor_conversion_runs
			(id, created_at, input_filename, output_name, csv_name, target_total_cents, original_total_cents,
			 final_total_cents, team_fund_cents, seed, people_json, result_json, workbook_blob)
		VALUES (?, '2026-04-30 12:00:00', '2026-04.xlsx', 'result.xlsx', '', 75000, 50000,
			75000, 0, 1, ?, '{}', X'00')
	`, historyID, string(peoplePayload)); err != nil {
		t.Fatal(err)
	}

	if err := appStore.UpdateSemesterMember(memberID, types.UpdateMemberRequest{
		RealName: "个人新姓名", Role: "USER",
	}); err != nil {
		t.Fatal(err)
	}
	records, err := appStore.GetPersonalRecords(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if records.RealName != "个人新姓名" || records.StudentNumber != studentNumber {
		t.Fatalf("profile=%+v", records)
	}
	if len(records.DutyRecords) != 1 || records.DutyRecords[0].Date != "2026-03-10" || records.DutyRecords[0].TimeSlot != "10:00-12:00" {
		t.Fatalf("duty records=%+v", records.DutyRecords)
	}
	if len(records.WorkRecords) != 1 || records.WorkHours != 2.5 {
		t.Fatalf("work records=%+v total=%v", records.WorkRecords, records.WorkHours)
	}
	if len(records.LaborHistory) != 1 || records.LaborHistory[0].HistoryID != historyID || records.LaborHistory[0].Adjusted != "750.00" {
		t.Fatalf("labor history=%+v", records.LaborHistory)
	}

	content, err := appStore.ExportPersonalRecordsWorkbook(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer workbook.Close()
	if sheets := workbook.GetSheetList(); len(sheets) != 4 {
		t.Fatalf("workbook sheets=%v", sheets)
	}
}
