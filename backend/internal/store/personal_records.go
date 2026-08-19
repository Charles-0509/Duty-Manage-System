package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/xuri/excelize/v2"
)

func (s *Store) GetPersonalRecords(accountID int64) (types.PersonalRecordsResponse, error) {
	var accountUUID, realName, studentNumber string
	if err := s.control.QueryRow(`
		SELECT account_uuid, real_name, student_number
		FROM accounts
		WHERE id = ?
	`, accountID).Scan(&accountUUID, &realName, &studentNumber); err != nil {
		return types.PersonalRecordsResponse{}, err
	}

	result := types.PersonalRecordsResponse{
		RealName:      realName,
		StudentNumber: studentNumber,
		DutyRecords:   []types.PersonalDutyRecord{},
		WorkRecords:   []types.PersonalWorkRecord{},
		LaborHistory:  []types.PersonalLaborRecord{},
	}

	dutyRows, err := s.db.Query(`
		SELECT schedules.week_number, entries.shift_code
		FROM final_schedule_entries AS entries
		JOIN final_schedules AS schedules ON schedules.week_number = entries.week_number
		JOIN users AS members ON members.id = entries.member_id
		WHERE members.account_uuid = ?
	`, accountUUID)
	if err != nil {
		return result, err
	}
	for dutyRows.Next() {
		var item types.PersonalDutyRecord
		if err := dutyRows.Scan(&item.WeekNumber, &item.ShiftCode); err != nil {
			dutyRows.Close()
			return result, err
		}
		item.Date, item.Weekday, item.TimeSlot = personalShiftDetails(s.active.FirstMonday, item.WeekNumber, item.ShiftCode)
		result.DutyRecords = append(result.DutyRecords, item)
	}
	if err := dutyRows.Err(); err != nil {
		dutyRows.Close()
		return result, err
	}
	dutyRows.Close()
	sort.Slice(result.DutyRecords, func(i, j int) bool {
		if result.DutyRecords[i].Date != result.DutyRecords[j].Date {
			return result.DutyRecords[i].Date < result.DutyRecords[j].Date
		}
		return result.DutyRecords[i].ShiftCode < result.DutyRecords[j].ShiftCode
	})
	result.DutyCount = len(result.DutyRecords)

	workRows, err := s.db.Query(`
		SELECT orders.id, orders.title, sessions.date, sessions.duration
		FROM work_sessions AS sessions
		JOIN work_orders AS orders ON orders.id = sessions.work_order_id
		JOIN users AS members ON members.id = sessions.member_id
		WHERE members.account_uuid = ?
		ORDER BY sessions.date DESC, sessions.id DESC
	`, accountUUID)
	if err != nil {
		return result, err
	}
	for workRows.Next() {
		var item types.PersonalWorkRecord
		if err := workRows.Scan(&item.WorkOrderID, &item.WorkOrderTitle, &item.Date, &item.Duration); err != nil {
			workRows.Close()
			return result, err
		}
		result.WorkHours += item.Duration
		result.WorkRecords = append(result.WorkRecords, item)
	}
	if err := workRows.Err(); err != nil {
		workRows.Close()
		return result, err
	}
	workRows.Close()

	var laborAdjustedTotal int64
	if strings.TrimSpace(studentNumber) != "" {
		laborRows, err := s.db.Query(`
			SELECT id, created_at, input_filename, people_json
			FROM labor_conversion_runs
			ORDER BY created_at DESC, id DESC
		`)
		if err != nil {
			return result, err
		}
		for laborRows.Next() {
			var historyID, createdAt, inputFilename, peoplePayload string
			if err := laborRows.Scan(&historyID, &createdAt, &inputFilename, &peoplePayload); err != nil {
				laborRows.Close()
				return result, err
			}
			var people []laborPerson
			if err := json.Unmarshal([]byte(peoplePayload), &people); err != nil {
				laborRows.Close()
				return result, fmt.Errorf("劳务历史 %s 人员快照无法读取: %w", historyID, err)
			}
			for _, person := range people {
				if strings.TrimSpace(person.StudentNumber) != strings.TrimSpace(studentNumber) {
					continue
				}
				tax := estimateLaborTax(person.Adjusted)
				result.LaborHistory = append(result.LaborHistory, types.PersonalLaborRecord{
					HistoryID:     historyID,
					CreatedAt:     createdAt,
					InputFilename: inputFilename,
					Original:      formatLaborMoney(person.Original),
					Adjusted:      formatLaborMoney(person.Adjusted),
					Tax:           formatLaborMoney(tax),
					Net:           formatLaborMoney(person.Adjusted - tax),
					Remark:        strings.Join(person.Remarks, "; "),
				})
				laborAdjustedTotal += person.Adjusted
				break
			}
		}
		if err := laborRows.Err(); err != nil {
			laborRows.Close()
			return result, err
		}
		laborRows.Close()
	}
	result.LaborAdjustedTotal = formatLaborMoney(laborAdjustedTotal)
	return result, nil
}

func personalShiftDetails(firstMonday string, weekNumber int, shiftCode string) (string, string, string) {
	parts := strings.Split(shiftCode, "-")
	if len(parts) != 2 || weekNumber < 1 {
		return "", "", ""
	}
	dayIndex := -1
	for index, code := range config.WeekdaysCode {
		if code == parts[0] {
			dayIndex = index
			break
		}
	}
	shiftNumber, err := strconv.Atoi(parts[1])
	if err != nil || dayIndex < 0 || shiftNumber < 1 || shiftNumber > len(config.TimeSlots) {
		return "", "", ""
	}
	first, err := time.Parse("20060102", firstMonday)
	if err != nil {
		return "", "", ""
	}
	date := first.AddDate(0, 0, (weekNumber-1)*7+dayIndex).Format("2006-01-02")
	return date, config.WeekdaysDisplay[dayIndex], config.TimeSlots[shiftNumber-1]
}

func (s *Store) ExportPersonalRecordsWorkbook(accountID int64) ([]byte, error) {
	records, err := s.GetPersonalRecords(accountID)
	if err != nil {
		return nil, err
	}

	file := excelize.NewFile()
	defer file.Close()
	file.SetSheetName("Sheet1", "概览")
	for rowIndex, row := range [][]any{
		{"姓名", records.RealName},
		{"学号", records.StudentNumber},
		{"实际值班班次数", records.DutyCount},
		{"工单工时合计", records.WorkHours},
		{"劳务调整后合计", records.LaborAdjustedTotal},
	} {
		for columnIndex, value := range row {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			file.SetCellValue("概览", cell, value)
		}
	}
	file.SetColWidth("概览", "A", "A", 22)
	file.SetColWidth("概览", "B", "B", 24)

	file.NewSheet("实际值班")
	for columnIndex, value := range []string{"日期", "周次", "星期", "时间段", "班次编码"} {
		cell, _ := excelize.CoordinatesToCellName(columnIndex+1, 1)
		file.SetCellValue("实际值班", cell, value)
	}
	for rowIndex, item := range records.DutyRecords {
		for columnIndex, value := range []any{item.Date, item.WeekNumber, item.Weekday, item.TimeSlot, item.ShiftCode} {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			file.SetCellValue("实际值班", cell, value)
		}
	}
	file.SetColWidth("实际值班", "A", "E", 18)

	file.NewSheet("工单工时")
	for columnIndex, value := range []string{"日期", "工单", "工时", "工单编号"} {
		cell, _ := excelize.CoordinatesToCellName(columnIndex+1, 1)
		file.SetCellValue("工单工时", cell, value)
	}
	for rowIndex, item := range records.WorkRecords {
		for columnIndex, value := range []any{item.Date, item.WorkOrderTitle, item.Duration, item.WorkOrderID} {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			file.SetCellValue("工单工时", cell, value)
		}
	}
	file.SetColWidth("工单工时", "A", "A", 14)
	file.SetColWidth("工单工时", "B", "B", 32)
	file.SetColWidth("工单工时", "C", "D", 20)

	file.NewSheet("劳务历史")
	for columnIndex, value := range []string{"生成时间", "来源文件", "原金额", "调整后", "预估税额", "税后", "备注", "历史编号"} {
		cell, _ := excelize.CoordinatesToCellName(columnIndex+1, 1)
		file.SetCellValue("劳务历史", cell, value)
	}
	for rowIndex, item := range records.LaborHistory {
		for columnIndex, value := range []any{item.CreatedAt, item.InputFilename, item.Original, item.Adjusted, item.Tax, item.Net, item.Remark, item.HistoryID} {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			file.SetCellValue("劳务历史", cell, value)
		}
	}
	file.SetColWidth("劳务历史", "A", "A", 20)
	file.SetColWidth("劳务历史", "B", "B", 32)
	file.SetColWidth("劳务历史", "C", "F", 14)
	file.SetColWidth("劳务历史", "G", "H", 28)

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
