package store

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"personnel-management-go/internal/types"
)

// Monthly declaration dates describe the approved reporting conversion, not
// attendance. The actual time records remain immutable in the source snapshot.
func monthlyCSV(snapshot MonthlySnapshot, distribution MonthlyDistribution) ([]byte, error) {
	month, err := time.Parse("2006-01", snapshot.Conditions.OutputMonth)
	if err != nil {
		return nil, fmt.Errorf("申报月份无效")
	}
	allocator := newCSVScheduleAllocator()
	daily := map[string]int{}
	entries := []dutyCSVEntry{}
	place := func(name string, date time.Time, blocks []csvTimeBlock, remaining int) ([]dutyCSVEntry, int) {
		result := []dutyCSVEntry{}
		key := name + "/" + date.Format("2006-01-02")
		for _, block := range blocks {
			block.Start = max(block.Start, 8*60)
			block.End = min(block.End, 22*60+30)
			if block.End <= block.Start {
				continue
			}
			for _, free := range allocator.freeSegments(name, date, block) {
				minutes := min(remaining, free.End-free.Start, 8*60-daily[key]) / 60 * 60
				if remaining-minutes == 60 {
					minutes -= 60 // Keep at least two hours for the next record.
				}
				if minutes < 120 {
					continue
				}
				end := free.Start + minutes
				allocator.occupy(name, date, csvTimeBlock{Start: free.Start, End: end})
				daily[key] += minutes
				remaining -= minutes
				result = append(result, dutyCSVEntry{Name: name, Date: date, StartTime: formatCSVMinute(free.Start), EndTime: formatCSVMinute(end), Hours: float64(minutes) / 60})
			}
		}
		return result, remaining
	}
	for _, member := range distribution.Rows {
		if member.Declared == 0 {
			continue
		}
		if member.Declared%2500 != 0 {
			return nil, fmt.Errorf("%s的申报金额必须为25元整数倍", member.Name)
		}
		remaining := int(member.Declared/2500) * 60
		if remaining < 120 {
			return nil, fmt.Errorf("%s的申报额%s元只折算1小时，无法满足每条至少2小时，请调整至0元或至少50元", member.Name, formatLaborMoney(member.Declared))
		}
		for _, duty := range snapshot.Duty {
			if duty.MemberID != member.ID || remaining <= 0 {
				continue
			}
			date, err := time.Parse("2006-01-02", duty.Date)
			if err != nil {
				return nil, err
			}
			date = mapDateToOutputMonth(date, month)
			start, ok := parseCSVMinute(duty.Start)
			if !ok {
				return nil, fmt.Errorf("来源值班时间无效")
			}
			end, ok := parseCSVMinute(duty.End)
			if !ok {
				return nil, fmt.Errorf("来源值班时间无效")
			}
			allocated, left := place(member.Name, date, []csvTimeBlock{{Start: start, End: end}}, remaining)
			entries = append(entries, allocated...)
			remaining = left
		}
		for _, order := range snapshot.WorkOrders {
			for _, session := range order.WorkSessions {
				if session.WorkerName != member.Name || remaining <= 0 {
					continue
				}
				date, err := time.Parse("2006-01-02", session.Date)
				if err != nil {
					return nil, err
				}
				date = mapDateToOutputMonth(date, month)
				preferred := min(remaining, int(math.Round(session.Duration*float64(snapshot.Rates.WorkOrderCents)/2500))*60)
				if remaining-preferred == 60 {
					preferred -= 60
				}
				if preferred < 120 {
					continue
				}
				allocated, left := place(member.Name, date, csvNormalWorkBlocks(), preferred)
				entries = append(entries, allocated...)
				remaining -= preferred - left
			}
		}
		for _, date := range managementDateOrder(month) {
			if remaining <= 0 {
				break
			}
			allocated, left := place(member.Name, date, []csvTimeBlock{{Start: 8 * 60, End: 12 * 60}, {Start: 14 * 60, End: 18 * 60}, {Start: 18 * 60, End: 22 * 60}, {Start: 12 * 60, End: 14 * 60}}, remaining)
			entries = append(entries, allocated...)
			remaining = left
		}
		if remaining > 0 {
			return nil, fmt.Errorf("%s仍有%.1f小时不能在本月08:00–22:30、每天8小时且每条至少2小时的条件下排入", member.Name, float64(remaining)/60)
		}
	}
	order := map[string]int{}
	for i, m := range snapshot.Members {
		order[m.Name] = i
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Name != b.Name {
			return order[a.Name] < order[b.Name]
		}
		if !a.Date.Equal(b.Date) {
			return a.Date.Before(b.Date)
		}
		as, _ := parseCSVMinute(a.StartTime)
		bs, _ := parseCSVMinute(b.StartTime)
		return as < bs
	})
	// Coalesce adjacent segments to stay within the existing Word template.
	merged := []dutyCSVEntry{}
	for _, entry := range entries {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.Name == entry.Name && last.Date.Equal(entry.Date) && last.EndTime == entry.StartTime {
				last.EndTime = entry.EndTime
				last.Hours += entry.Hours
				continue
			}
		}
		merged = append(merged, entry)
	}
	return writeDutyCSVEntries(merged)
}

func monthlyPeople(distribution MonthlyDistribution) []laborPerson {
	people := []laborPerson{}
	for _, row := range distribution.Rows {
		people = append(people, laborPerson{Name: row.Name, StudentNumber: row.StudentNumber, Original: row.Original, DutyHours: row.DutyHours, WorkOrderHours: row.WorkHours, Management: row.Management, Adjusted: row.Declared})
	}
	return people
}

func monthlyRecords(snapshot MonthlySnapshot, distribution MonthlyDistribution, csvContent, template []byte) ([]byte, error) {
	records, err := parseWorkStudyCSV(csvContent)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, person := range distribution.Rows {
		if person.Declared == 0 {
			continue
		}
		docx, err := fillWorkStudyTemplate(template, person.Name, person.StudentNumber, records[person.Name], snapshot.WorkContent)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", person.Name, err)
		}
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: person.Name + "_" + workStudyTemplateSuffix, Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(docx); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func monthlyChecks(snapshot MonthlySnapshot, distribution MonthlyDistribution, template []byte) ([]string, []byte) {
	issues := []string{}
	seenNames, seenNumbers := map[string]bool{}, map[string]bool{}
	if distribution.Unallocated != 0 {
		issues = append(issues, "申报合计与目标总额不一致")
	}
	for _, row := range distribution.Rows {
		if row.Declared > 0 {
			if seenNames[row.Name] || seenNumbers[row.StudentNumber] {
				issues = append(issues, row.Name+"的姓名或学号重复")
			}
			seenNames[row.Name], seenNumbers[row.StudentNumber] = true, true
		}
		if row.SalaryOutstanding > 0 {
			issues = append(issues, row.Name+"仍有实际应发未安排代发")
		}
		if row.Declared > 0 && (row.StudentNumber == "" || validateStudentNumber(row.StudentNumber) != nil) {
			issues = append(issues, row.Name+"缺少有效学号")
		}
		if row.Declared > monthlyHardCap {
			issues = append(issues, row.Name+"超过1950元")
		}
	}
	if len(template) == 0 {
		issues = append(issues, "缺少全局勤工助学模板")
	} else if _, err := normalizeGlobalWorkStudyTemplate(template); err != nil {
		issues = append(issues, err.Error())
	}
	var allocatedFund int64
	var salaryOut, salaryIn int64
	for _, row := range distribution.Rows {
		allocatedFund += row.TeamFund
		salaryOut += row.SalaryOutgoing
		salaryIn += row.SalaryIncoming
	}
	if allocatedFund != distribution.TeamFund || salaryOut != salaryIn {
		issues = append(issues, "实际薪酬和团队经费的代发关系未对平")
	}
	csvContent, err := monthlyCSV(snapshot, distribution)
	if err != nil {
		issues = append(issues, err.Error())
		return issues, nil
	}
	records, err := parseWorkStudyCSV(csvContent)
	if err != nil {
		issues = append(issues, err.Error())
		return issues, csvContent
	}
	for _, row := range distribution.Rows {
		if row.Declared == 0 {
			continue
		}
		var hours float64
		for _, record := range records[row.Name] {
			value, err := parseMonthlyHours(record.Hours)
			if err != nil {
				issues = append(issues, row.Name+"时数无效")
				continue
			}
			hours += value
		}
		if int64(math.Round(hours*2500)) != row.Declared {
			issues = append(issues, row.Name+"申报时数和金额不一致")
		}
		if len(template) > 0 {
			if _, err = fillWorkStudyTemplate(template, row.Name, row.StudentNumber, records[row.Name], snapshot.WorkContent); err != nil {
				issues = append(issues, row.Name+"："+err.Error())
			}
		}
	}
	return cleanedStringSlice(issues), csvContent
}

func parseMonthlyHours(value string) (float64, error) {
	var hours float64
	_, err := fmt.Sscan(value, &hours)
	return hours, err
}

func (s *Store) monthlyTemplate() ([]byte, error) {
	_, template, err := s.GetWorkStudyTemplate()
	if os.IsNotExist(err) {
		return nil, nil // Missing templates are a preflight issue, not an I/O failure.
	}
	return template, err
}

func (s *Store) CheckMonthlyReport(reportID, versionID string) (types.MonthlyReportView, error) {
	view, err := s.GetMonthlyReport(reportID, versionID)
	if err != nil {
		return view, err
	}
	if view.Version.Status != "draft" {
		return view, nil
	}
	snapshot, distribution, _, _, err := s.readMonthlyVersion(reportID, view.Version.ID)
	if err != nil {
		return view, err
	}
	template, err := s.monthlyTemplate()
	if err != nil {
		return view, err
	}
	issues, _ := monthlyChecks(snapshot, distribution, template)
	view.Issues = issues
	return view, nil
}

func (s *Store) GenerateMonthlyReport(reportID, versionID string) (types.MonthlyReportView, error) {
	s.monthlyFilesMu.Lock()
	defer s.monthlyFilesMu.Unlock()
	return s.generateMonthlyReport(reportID, types.MonthlyFinalizeRequest{VersionID: versionID}, false)
}

func (s *Store) FinalizeMonthlyReport(reportID string, request types.MonthlyFinalizeRequest) (types.MonthlyReportView, error) {
	s.monthlyFilesMu.Lock()
	defer s.monthlyFilesMu.Unlock()
	return s.generateMonthlyReport(reportID, request, true)
}

func (s *Store) prepareMonthlyDraft(reportID, versionID string) (types.MonthlyReportView, error) {
	view, err := s.GenerateMonthlyReport(reportID, versionID)
	var validation MonthlyValidationError
	if errors.As(err, &validation) {
		return view, nil // Keep incomplete drafts editable, without stale output files.
	}
	return view, err
}

func (s *Store) generateMonthlyReport(reportID string, request types.MonthlyFinalizeRequest, finalize bool) (types.MonthlyReportView, error) {
	if s.active.Archived {
		return types.MonthlyReportView{}, ErrArchivedSemester
	}
	view, err := s.GetMonthlyReport(reportID, request.VersionID)
	if err != nil {
		return view, err
	}
	if view.Version.Status == "legacy" || (finalize && view.Version.Status != "draft") {
		return view, ErrMonthlyReadOnly
	}
	if view.Report.LatestVersionID != request.VersionID {
		return view, ErrMonthlyConflict
	}
	snapshot, distribution, _, _, err := s.readMonthlyVersion(reportID, request.VersionID)
	if err != nil {
		return view, err
	}
	for _, row := range distribution.Rows {
		if finalize && row.Declared > monthlyAutoCap && !request.ConfirmHighAmounts {
			return view, monthlyInputErrorf("超过1900元的申报金额需确认")
		}
	}
	template, err := s.monthlyTemplate()
	if err != nil {
		return view, err
	}
	issues, csvContent := monthlyChecks(snapshot, distribution, template)
	if len(issues) > 0 {
		data, _ := json.Marshal(issues)
		var latest string
		if err = s.db.QueryRow(`SELECT latest_version_id FROM monthly_reports WHERE id=?`, reportID).Scan(&latest); err != nil {
			return view, err
		}
		if latest != request.VersionID {
			return view, ErrMonthlyConflict
		}
		if _, err = s.db.Exec(`UPDATE monthly_report_versions SET issues_json=?,files_json='{}',labor_xlsx_blob=NULL,work_study_xlsx_blob=NULL,records_zip_blob=NULL WHERE id=? AND status='draft'`, string(data), request.VersionID); err != nil {
			return view, err
		}
		if view.Version.Status == "draft" {
			if err = s.clearMonthlyCache(reportID); err != nil {
				return view, err
			}
		}
		view.Issues = issues
		return view, MonthlyValidationError{Issues: issues}
	}
	people := monthlyPeople(distribution)
	roles := map[string]string{}
	for _, member := range snapshot.Members {
		roles[member.Name] = member.Role
	}
	laborWorkbook, err := createLaborCalculationWorkbook(laborAdjustmentResult{People: people}, roles, snapshot.Rates)
	if err != nil {
		return view, err
	}
	workStudyWorkbook, err := createLaborWorkStudyConversionWorkbook(people, snapshot.Conditions.OutputMonth)
	if err != nil {
		return view, err
	}
	records, err := monthlyRecords(snapshot, distribution, csvContent, template)
	if err != nil {
		return view, err
	}
	names, err := monthlyOutputNames(view.Report.Month)
	if err != nil {
		return view, err
	}
	metadata, err := json.Marshal(names)
	if err != nil {
		return view, err
	}
	templateSHA := sha256.Sum256(template)
	tx, err := s.db.Begin()
	if err != nil {
		return view, err
	}
	defer tx.Rollback()
	var latest string
	if err = tx.QueryRow(`SELECT latest_version_id FROM monthly_reports WHERE id=?`, reportID).Scan(&latest); err != nil {
		return view, err
	}
	if latest != request.VersionID {
		return view, ErrMonthlyConflict
	}
	status := view.Version.Status
	if finalize {
		status = "finalized"
	}
	result, err := tx.Exec(`UPDATE monthly_report_versions SET status=?,issues_json='[]',files_json=?,package_blob=NULL,labor_xlsx_blob=?,work_study_xlsx_blob=?,records_zip_blob=?,template_hash=? WHERE id=? AND status IN ('draft','finalized')`, status, string(metadata), laborWorkbook, workStudyWorkbook, records, hex.EncodeToString(templateSHA[:]), request.VersionID)
	if err != nil {
		return view, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return view, err
	}
	if affected != 1 {
		return view, ErrMonthlyConflict
	}
	if finalize {
		if _, err = tx.Exec(`UPDATE monthly_reports SET final_version_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, request.VersionID, reportID); err != nil {
			return view, err
		}
	}
	finishCache, err := s.replaceMonthlyCache(reportID, names, map[string][]byte{"labor": laborWorkbook, "work-study": workStudyWorkbook, "records": records})
	if err != nil {
		return view, err
	}
	committed := false
	defer func() { finishCache(committed) }()
	if err = tx.Commit(); err != nil {
		return view, err
	}
	committed = true
	return s.GetMonthlyReport(reportID, request.VersionID)
}

type MonthlyValidationError struct{ Issues []string }

func (e MonthlyValidationError) Error() string { return strings.Join(e.Issues, "；") }
