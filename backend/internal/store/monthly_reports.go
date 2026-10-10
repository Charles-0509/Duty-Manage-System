package store

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"personnel-management-go/internal/types"
)

var ErrMonthlyConflict = errors.New("报表来源或版本已变化，请刷新后重试")
var ErrMonthlyReadOnly = errors.New("该历史版本只读，请创建新修订")

type MonthlyInputError struct{ Message string }

func (e MonthlyInputError) Error() string { return e.Message }

func monthlyInputErrorf(format string, args ...any) error {
	return MonthlyInputError{Message: fmt.Sprintf(format, args...)}
}

type monthlyExecer interface {
	Exec(string, ...any) (sql.Result, error)
}

func initMonthlySchema(db monthlyExecer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS monthly_reports (id TEXT PRIMARY KEY, output_month TEXT NOT NULL UNIQUE, final_version_id TEXT NOT NULL DEFAULT '', latest_version_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS monthly_report_sources (id TEXT PRIMARY KEY, report_id TEXT NOT NULL, snapshot_json TEXT NOT NULL, source_hash TEXT NOT NULL, finance_xlsx_blob BLOB, finance_csv_blob BLOB, legacy_metadata_json TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(report_id) REFERENCES monthly_reports(id))`,
		`CREATE TABLE IF NOT EXISTS monthly_report_versions (id TEXT PRIMARY KEY, report_id TEXT NOT NULL, source_id TEXT NOT NULL, version_number INTEGER NOT NULL, parent_version_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL CHECK(status IN ('draft','finalized','legacy')), allocation_json TEXT NOT NULL, target_cents INTEGER NOT NULL, original_cents INTEGER NOT NULL, declared_cents INTEGER NOT NULL, issues_json TEXT NOT NULL DEFAULT '[]', files_json TEXT NOT NULL DEFAULT '{}', package_blob BLOB, labor_xlsx_blob BLOB, work_study_xlsx_blob BLOB, records_zip_blob BLOB, template_hash TEXT NOT NULL DEFAULT '', legacy_note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(report_id,version_number), FOREIGN KEY(report_id) REFERENCES monthly_reports(id), FOREIGN KEY(source_id) REFERENCES monthly_report_sources(id))`,
		`CREATE INDEX IF NOT EXISTS idx_monthly_versions_report ON monthly_report_versions(report_id,version_number)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) readMonthlySnapshot(conditions types.MonthlyConditions) (MonthlySnapshot, error) {
	snapshot := MonthlySnapshot{Conditions: conditions, Members: []MonthlyMember{}, Duty: []MonthlyDuty{}, WorkOrders: []types.WorkOrder{}}
	if _, err := time.Parse("2006-01", conditions.OutputMonth); err != nil {
		return snapshot, monthlyInputErrorf("请选择有效的申报月份")
	}
	start, end, err := parseAllowedDateRange(conditions.StartDate, conditions.EndDate)
	if err != nil {
		return snapshot, MonthlyInputError{Message: err.Error()}
	}
	if !conditions.IncludeManagement {
		snapshot.Conditions.ManagementMonths = 0
	}
	if snapshot.Conditions.ManagementMonths < 0 || snapshot.Conditions.ManagementMonths > 12 {
		return snapshot, monthlyInputErrorf("管理薪酬月份数必须在0至12之间")
	}
	snapshot.Conditions.WorkOrderIDs = cleanedStringSlice(conditions.WorkOrderIDs)
	sort.Strings(snapshot.Conditions.WorkOrderIDs)
	type profile struct {
		name, number string
		active       bool
	}
	profiles := map[string]profile{}
	rows, err := s.control.Query(`SELECT account_uuid,real_name,student_number,is_active FROM accounts`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var id, name, number string
		var active int
		if err = rows.Scan(&id, &name, &number, &active); err != nil {
			rows.Close()
			return snapshot, err
		}
		profiles[id] = profile{name, number, active == 1}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	tx, err := s.db.Begin()
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow(`SELECT first_monday,work_study_content,COALESCE(duty_rate_cents,2500),COALESCE(work_order_rate_cents,5000),COALESCE(mgmt_leader_rate_cents,80000),COALESCE(mgmt_owner_rate_cents,120000) FROM semester_settings WHERE id=1`).Scan(&snapshot.FirstMonday, &snapshot.WorkContent, &snapshot.Rates.DutyCents, &snapshot.Rates.WorkOrderCents, &snapshot.Rates.MgmtLeaderCents, &snapshot.Rates.MgmtOwnerCents); err != nil {
		return snapshot, err
	}
	rows, err = tx.Query(`SELECT id,account_uuid,role,sort_order FROM users WHERE is_active=1 AND role!='ADMIN' ORDER BY sort_order,id`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var member MonthlyMember
		if err = rows.Scan(&member.ID, &member.AccountUUID, &member.Role, &member.SortOrder); err != nil {
			rows.Close()
			return snapshot, err
		}
		p, ok := profiles[member.AccountUUID]
		if !ok {
			rows.Close()
			return snapshot, fmt.Errorf("成员%d的全局资料不存在", member.ID)
		}
		member.Name, member.StudentNumber = p.name, p.number
		snapshot.Members = append(snapshot.Members, member)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	ranks := map[string]int{"OWNER": 0, "LEADER": 1, "HR": 2, "FINANCE": 3, "USER": 4}
	sort.SliceStable(snapshot.Members, func(i, j int) bool {
		a, b := snapshot.Members[i], snapshot.Members[j]
		pa, pb := profiles[a.AccountUUID], profiles[b.AccountUUID]
		if pa.active != pb.active {
			return pa.active
		}
		if ranks[a.Role] != ranks[b.Role] {
			return ranks[a.Role] < ranks[b.Role]
		}
		if a.SortOrder != b.SortOrder {
			return a.SortOrder < b.SortOrder
		}
		return a.ID < b.ID
	})
	memberIndex := map[int64]int{}
	for i, m := range snapshot.Members {
		memberIndex[m.ID] = i
	}
	type assignment struct {
		member       int64
		code, parity string
	}
	finalAssignments := map[int][]assignment{}
	savedWeeks := map[int]bool{}
	rows, err = tx.Query(`SELECT week_number FROM final_schedules`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var week int
		if err = rows.Scan(&week); err != nil {
			rows.Close()
			return snapshot, err
		}
		savedWeeks[week] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	rows, err = tx.Query(`SELECT week_number,shift_code,member_id FROM final_schedule_entries ORDER BY week_number,shift_code,member_id`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var week int
		var code string
		var id sql.NullInt64
		if err = rows.Scan(&week, &code, &id); err != nil {
			rows.Close()
			return snapshot, err
		}
		if _, ok := memberIndex[id.Int64]; id.Valid && ok {
			finalAssignments[week] = append(finalAssignments[week], assignment{member: id.Int64, code: code})
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	planned := []assignment{}
	err = tx.QueryRow(`SELECT id,name FROM schedule_plans WHERE is_published=1`).Scan(&snapshot.PublishedPlanID, &snapshot.PublishedPlanName)
	if err != nil && err != sql.ErrNoRows {
		return snapshot, err
	}
	rows, err = tx.Query(`SELECT e.shift_code,e.member_id,e.week_type FROM schedule_entries e JOIN schedule_plans p ON p.id=e.schedule_plan_id WHERE p.is_published=1 ORDER BY e.shift_code,e.member_id`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var a assignment
		var id sql.NullInt64
		if err = rows.Scan(&a.code, &id, &a.parity); err != nil {
			rows.Close()
			return snapshot, err
		}
		if _, ok := memberIndex[id.Int64]; id.Valid && ok {
			a.member = id.Int64
			planned = append(planned, a)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	seenDuty := map[string]bool{}
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		day, ok := weekdayCodeForDate(date)
		if !ok {
			continue
		}
		week := calculateWeekNumber(date, snapshot.FirstMonday)
		assignments := planned
		if savedWeeks[week] {
			assignments = finalAssignments[week]
		}
		for _, a := range assignments {
			if !strings.HasPrefix(a.code, day+"-") {
				continue
			}
			if !savedWeeks[week] && a.parity != "both" && ((week%2 == 1 && a.parity != "single") || (week%2 == 0 && a.parity != "double")) {
				continue
			}
			key := fmt.Sprintf("%s/%s/%d", date.Format("2006-01-02"), a.code, a.member)
			if seenDuty[key] {
				continue
			}
			seenDuty[key] = true
			startTime, endTime, valid := shiftTimeRange(a.code)
			if !valid {
				return snapshot, fmt.Errorf("实际值班包含无效班次%s", a.code)
			}
			source := "planned"
			if savedWeeks[week] {
				source = "saved"
			}
			hours := shiftDurationHours(a.code)
			snapshot.Duty = append(snapshot.Duty, MonthlyDuty{MemberID: a.member, Date: date.Format("2006-01-02"), Start: startTime, End: endTime, Hours: hours, Source: source})
			snapshot.Members[memberIndex[a.member]].DutyHours += hours
		}
	}
	for _, id := range snapshot.Conditions.WorkOrderIDs {
		var order types.WorkOrder
		if err = tx.QueryRow(`SELECT id,title,belonging_month,created_time,created_by FROM work_orders WHERE id=?`, id).Scan(&order.ID, &order.Title, &order.BelongingMonth, &order.CreatedTime, &order.CreatedBy); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return snapshot, monthlyInputErrorf("所选工单%s不存在或已删除", id)
			}
			return snapshot, err
		}
		if _, ok := allowedWorkOrderMonthsForDateRange(start, end)[order.BelongingMonth]; !ok {
			return snapshot, monthlyInputErrorf("工单%s不属于统计期间附近月份", order.Title)
		}
		order.WorkSessions = []types.WorkSession{}
		rows, err = tx.Query(`SELECT id,date,member_id,duration FROM work_sessions WHERE work_order_id=? ORDER BY date,id`, id)
		if err != nil {
			return snapshot, err
		}
		for rows.Next() {
			var session types.WorkSession
			var member sql.NullInt64
			if err = rows.Scan(&session.ID, &session.Date, &member, &session.Duration); err != nil {
				rows.Close()
				return snapshot, err
			}
			index, ok := memberIndex[member.Int64]
			if !member.Valid || !ok {
				continue
			}
			if _, err = time.Parse("2006-01-02", session.Date); err != nil || session.Duration <= 0 || math.IsNaN(session.Duration) || math.IsInf(session.Duration, 0) {
				rows.Close()
				return snapshot, fmt.Errorf("工单%s含无效日期或时长", order.Title)
			}
			session.WorkerName = snapshot.Members[index].Name
			snapshot.Members[index].WorkHours += session.Duration
			order.WorkSessions = append(order.WorkSessions, session)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return snapshot, err
		}
		rows.Close()
		snapshot.WorkOrders = append(snapshot.WorkOrders, order)
	}
	for i := range snapshot.Members {
		m := &snapshot.Members[i]
		m.DutyAmount = int64(math.Round(m.DutyHours * float64(snapshot.Rates.DutyCents)))
		m.WorkAmount = int64(math.Round(m.WorkHours * float64(snapshot.Rates.WorkOrderCents)))
		if snapshot.Conditions.IncludeManagement {
			m.Management = int64(math.Round((&Store{rates: snapshot.Rates}).calculateManagementAmountForMonthCount(m.Role, snapshot.Conditions.ManagementMonths) * 100))
		}
		m.Original = m.DutyAmount + m.WorkAmount + m.Management
	}
	return snapshot, tx.Commit()
}

func monthlyHash(snapshot MonthlySnapshot) (string, []byte, error) {
	data, err := json.Marshal(snapshot)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), data, err
}
func monthlyDefaultProxies(snapshot MonthlySnapshot, ids *[]int64) []int64 {
	if ids != nil {
		return *ids
	}
	result := []int64{}
	for _, m := range snapshot.Members {
		result = append(result, m.ID)
	}
	return result
}

func (s *Store) PreviewMonthlyReport(conditions types.MonthlyConditions) (types.MonthlyReportView, error) {
	snapshot, err := s.readMonthlySnapshot(conditions)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	hash, _, err := monthlyHash(snapshot)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	distribution := MonthlyDistribution{Rows: []MonthlyAllocatedMember{}, Transfers: []MonthlyTransfer{}}
	for _, m := range snapshot.Members {
		distribution.Original += m.Original
		distribution.Rows = append(distribution.Rows, MonthlyAllocatedMember{MonthlyMember: m, Eligible: true})
	}
	return monthlyView(snapshot, distribution, hash), nil
}

func (s *Store) CreateMonthlyReport(request types.MonthlyReportRequest) (types.MonthlyReportView, error) {
	if s.active.Archived {
		return types.MonthlyReportView{}, ErrArchivedSemester
	}
	snapshot, err := s.readMonthlySnapshot(request.MonthlyConditions)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	hash, data, err := monthlyHash(snapshot)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	if request.SourceHash != "" && hash != request.SourceHash {
		return types.MonthlyReportView{}, ErrMonthlyConflict
	}
	target, err := ParseMonthlyMoney(request.TargetTotal)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	distribution, err := AllocateMonthlyReport(snapshot, target, monthlyDefaultProxies(snapshot, request.ProxyMemberIDs))
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	defer tx.Rollback()
	var reportID, parent string
	err = tx.QueryRow(`SELECT id,latest_version_id FROM monthly_reports WHERE output_month=?`, snapshot.Conditions.OutputMonth).Scan(&reportID, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		reportID = uuid.NewString()
		_, err = tx.Exec(`INSERT INTO monthly_reports(id,output_month) VALUES(?,?)`, reportID, snapshot.Conditions.OutputMonth)
	} else if err == nil && parent != "" {
		var status string
		err = tx.QueryRow(`SELECT status FROM monthly_report_versions WHERE id=?`, parent).Scan(&status)
		if err == nil && status != "legacy" {
			return types.MonthlyReportView{}, ErrMonthlyConflict
		}
	}
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	sourceID := uuid.NewString()
	if _, err = tx.Exec(`INSERT INTO monthly_report_sources(id,report_id,snapshot_json,source_hash) VALUES(?,?,?,?)`, sourceID, reportID, string(data), hash); err != nil {
		return types.MonthlyReportView{}, err
	}
	versionID, err := insertMonthlyVersion(tx, reportID, sourceID, parent, distribution)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	if err = tx.Commit(); err != nil {
		return types.MonthlyReportView{}, err
	}
	return s.prepareMonthlyDraft(reportID, versionID)
}

func insertMonthlyVersion(tx *sql.Tx, reportID, sourceID, parent string, distribution MonthlyDistribution) (string, error) {
	var number int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version_number),0)+1 FROM monthly_report_versions WHERE report_id=?`, reportID).Scan(&number); err != nil {
		return "", err
	}
	data, err := json.Marshal(distribution)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	if _, err = tx.Exec(`INSERT INTO monthly_report_versions(id,report_id,source_id,version_number,parent_version_id,status,allocation_json,target_cents,original_cents,declared_cents) VALUES(?,?,?,?,?,'draft',?,?,?,?)`, id, reportID, sourceID, number, parent, string(data), distribution.Target, distribution.Original, distribution.Declared); err != nil {
		return "", err
	}
	_, err = tx.Exec(`UPDATE monthly_reports SET latest_version_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, id, reportID)
	return id, err
}

func (s *Store) ListMonthlyReports() ([]types.MonthlyReportSummary, error) {
	rows, err := s.db.Query(`SELECT id,output_month,final_version_id,latest_version_id,created_at,updated_at FROM monthly_reports ORDER BY output_month DESC,created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []types.MonthlyReportSummary{}
	for rows.Next() {
		var r types.MonthlyReportSummary
		if err = rows.Scan(&r.ID, &r.Month, &r.FinalVersionID, &r.LatestVersionID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func (s *Store) readMonthlyVersion(reportID, versionID string) (MonthlySnapshot, MonthlyDistribution, string, string, error) {
	var snapshot MonthlySnapshot
	var distribution MonthlyDistribution
	var sourceID, status, data, allocation string
	err := s.db.QueryRow(`SELECT s.snapshot_json,v.allocation_json,v.source_id,v.status FROM monthly_report_versions v JOIN monthly_report_sources s ON s.id=v.source_id WHERE v.report_id=? AND v.id=?`, reportID, versionID).Scan(&data, &allocation, &sourceID, &status)
	if err != nil {
		return snapshot, distribution, "", "", err
	}
	if err = json.Unmarshal([]byte(data), &snapshot); err != nil {
		return snapshot, distribution, "", "", err
	}
	err = json.Unmarshal([]byte(allocation), &distribution)
	return snapshot, distribution, sourceID, status, err
}

func (s *Store) NewMonthlyVersion(reportID string, request types.MonthlyVersionRequest) (types.MonthlyReportView, error) {
	if s.active.Archived {
		return types.MonthlyReportView{}, ErrArchivedSemester
	}
	snapshot, old, sourceID, status, err := s.readMonthlyVersion(reportID, request.BaseVersionID)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	if status == "legacy" {
		return types.MonthlyReportView{}, ErrMonthlyReadOnly
	}
	proxies := []int64{}
	for _, r := range old.Rows {
		if r.Eligible {
			proxies = append(proxies, r.ID)
		}
	}
	if request.ProxyMemberIDs != nil {
		proxies = *request.ProxyMemberIDs
	}
	target := old.Target
	if request.TargetTotal != "" {
		target, err = ParseMonthlyMoney(request.TargetTotal)
		if err != nil {
			return types.MonthlyReportView{}, err
		}
	}
	var distribution MonthlyDistribution
	var sourceData []byte
	var sourceHash string
	switch request.Mode {
	case "refresh":
		conditions := snapshot.Conditions
		if request.Conditions != nil {
			conditions = *request.Conditions
		}
		if conditions.OutputMonth != snapshot.Conditions.OutputMonth {
			return types.MonthlyReportView{}, monthlyInputErrorf("修订不能改变申报月份")
		}
		snapshot, err = s.readMonthlySnapshot(conditions)
		if err != nil {
			return types.MonthlyReportView{}, err
		}
		if request.ProxyMemberIDs == nil {
			proxies = monthlyDefaultProxies(snapshot, nil)
		}
		sourceHash, sourceData, err = monthlyHash(snapshot)
		if err != nil {
			return types.MonthlyReportView{}, err
		}
		distribution, err = AllocateMonthlyReport(snapshot, target, proxies)
	case "allocate":
		distribution, err = AllocateMonthlyReport(snapshot, target, proxies)
	case "copy":
		distribution = old
	case "manual":
		amounts := map[int64]int64{}
		for _, r := range request.Rows {
			if _, ok := amounts[r.MemberID]; ok {
				return types.MonthlyReportView{}, monthlyInputErrorf("调额成员重复")
			}
			amount, e := ParseMonthlyMoney(r.Amount)
			if e != nil {
				return types.MonthlyReportView{}, e
			}
			if amount > monthlyAutoCap && !request.ConfirmHighAmounts {
				return types.MonthlyReportView{}, monthlyInputErrorf("超过1900元的手调需明确确认")
			}
			amounts[r.MemberID] = amount
		}
		distribution, err = AdjustMonthlyReport(snapshot, amounts, proxies)
	default:
		return types.MonthlyReportView{}, monthlyInputErrorf("无效的修订操作")
	}
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	defer tx.Rollback()
	var latest string
	if err = tx.QueryRow(`SELECT latest_version_id FROM monthly_reports WHERE id=?`, reportID).Scan(&latest); err != nil {
		return types.MonthlyReportView{}, err
	}
	if latest != request.BaseVersionID {
		return types.MonthlyReportView{}, ErrMonthlyConflict
	}
	if request.Mode == "refresh" {
		sourceID = uuid.NewString()
		if _, err = tx.Exec(`INSERT INTO monthly_report_sources(id,report_id,snapshot_json,source_hash) VALUES(?,?,?,?)`, sourceID, reportID, string(sourceData), sourceHash); err != nil {
			return types.MonthlyReportView{}, err
		}
	}
	versionID, err := insertMonthlyVersion(tx, reportID, sourceID, request.BaseVersionID, distribution)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	if err = tx.Commit(); err != nil {
		return types.MonthlyReportView{}, err
	}
	return s.prepareMonthlyDraft(reportID, versionID)
}

func monthlyView(snapshot MonthlySnapshot, d MonthlyDistribution, hash string) types.MonthlyReportView {
	v := types.MonthlyReportView{Conditions: snapshot.Conditions, Rows: []types.MonthlyRow{}, Transfers: []types.MonthlyTransferRow{}, OriginalTotal: formatLaborMoney(d.Original), TargetTotal: formatLaborMoney(d.Target), DeclaredTotal: formatLaborMoney(d.Declared), TeamFund: formatLaborMoney(d.TeamFund), Unallocated: formatLaborMoney(d.Unallocated), SourceHash: hash, Issues: []string{}, Files: []string{}, Versions: []types.MonthlyVersionSummary{}}
	v.Notes = []string{}
	for _, r := range d.Rows {
		v.Rows = append(v.Rows, types.MonthlyRow{MemberID: r.ID, Name: r.Name, StudentNumber: r.StudentNumber, Role: r.Role, DutyHours: r.DutyHours, WorkHours: r.WorkHours, DutyAmount: formatLaborMoney(r.DutyAmount), WorkAmount: formatLaborMoney(r.WorkAmount), Management: formatLaborMoney(r.Management), Original: formatLaborMoney(r.Original), Declared: formatLaborMoney(r.Declared), Eligible: r.Eligible, SalaryOutgoing: formatLaborMoney(r.SalaryOutgoing), SalaryIncoming: formatLaborMoney(r.SalaryIncoming), SalaryOutstanding: formatLaborMoney(r.SalaryOutstanding), TeamFund: formatLaborMoney(r.TeamFund)})
	}
	for _, t := range d.Transfers {
		v.Transfers = append(v.Transfers, types.MonthlyTransferRow{SourceName: t.SourceName, ReceiverName: t.ReceiverName, Kind: t.Kind, Amount: formatLaborMoney(t.Amount)})
	}
	if d.Unallocated > 0 {
		v.Issues = append(v.Issues, "尚有"+formatLaborMoney(d.Unallocated)+"元未分配")
	}
	return v
}

func (s *Store) GetMonthlyReport(reportID, versionID string) (types.MonthlyReportView, error) {
	var report types.MonthlyReportSummary
	err := s.db.QueryRow(`SELECT id,output_month,final_version_id,latest_version_id,created_at,updated_at FROM monthly_reports WHERE id=?`, reportID).Scan(&report.ID, &report.Month, &report.FinalVersionID, &report.LatestVersionID, &report.CreatedAt, &report.UpdatedAt)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	if versionID == "" {
		versionID = report.LatestVersionID
	}
	if versionID == "" {
		view := monthlyView(MonthlySnapshot{}, MonthlyDistribution{}, "")
		view.Report = report
		view.Version.LegacyNote = "仅保留历史财务原件"
		view.Sources, err = s.monthlySources(reportID)
		return view, err
	}
	snapshot, d, _, _, err := s.readMonthlyVersion(reportID, versionID)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	var hash, issues, files, template string
	err = s.db.QueryRow(`SELECT s.source_hash,v.issues_json,v.files_json,v.template_hash FROM monthly_report_versions v JOIN monthly_report_sources s ON s.id=v.source_id WHERE v.id=?`, versionID).Scan(&hash, &issues, &files, &template)
	if err != nil {
		return types.MonthlyReportView{}, err
	}
	v := monthlyView(snapshot, d, hash)
	v.Report = report
	v.TemplateHash = template
	rows, err := s.db.Query(`SELECT id,version_number,status,created_at,target_cents,original_cents,declared_cents,legacy_note FROM monthly_report_versions WHERE report_id=? ORDER BY version_number DESC`, reportID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var item types.MonthlyVersionSummary
		var target, original, declared int64
		if err = rows.Scan(&item.ID, &item.Number, &item.Status, &item.CreatedAt, &target, &original, &declared, &item.LegacyNote); err != nil {
			return v, err
		}
		item.TargetTotal = formatLaborMoney(target)
		item.OriginalTotal = formatLaborMoney(original)
		item.DeclaredTotal = formatLaborMoney(declared)
		v.Versions = append(v.Versions, item)
		if item.ID == versionID {
			v.Version = item
		}
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	var storedIssues []string
	if err = json.Unmarshal([]byte(issues), &storedIssues); err != nil {
		return v, err
	}
	v.Issues = append(v.Issues, storedIssues...)
	var names map[string]string
	if err = json.Unmarshal([]byte(files), &names); err != nil {
		return v, err
	}
	for kind := range names {
		v.Files = append(v.Files, kind)
	}
	sort.Strings(v.Files)
	if v.Version.Status == "legacy" {
		v.Notes = append(v.Notes, "旧历史只展示原保存的金额、工时和代发关系；未保存的各项计价与代发汇总以—表示，不作为零，不重新推算。")
	}
	rows.Close()
	v.Conditions.OutputMonth = report.Month
	v.Sources, err = s.monthlySources(reportID)
	if err != nil {
		return v, err
	}
	return v, nil
}

func (s *Store) monthlySources(reportID string) ([]types.MonthlySourceSummary, error) {
	rows, err := s.db.Query(`SELECT id,snapshot_json,created_at,legacy_metadata_json,COALESCE(length(finance_xlsx_blob),0),COALESCE(length(finance_csv_blob),0) FROM monthly_report_sources WHERE report_id=? AND (finance_xlsx_blob IS NOT NULL OR finance_csv_blob IS NOT NULL) ORDER BY created_at DESC`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []types.MonthlySourceSummary{}
	for rows.Next() {
		var item types.MonthlySourceSummary
		var data, legacy string
		var excel, csv int
		if err = rows.Scan(&item.ID, &data, &item.CreatedAt, &legacy, &excel, &csv); err != nil {
			return nil, err
		}
		var snapshot MonthlySnapshot
		if err = json.Unmarshal([]byte(data), &snapshot); err != nil {
			return nil, err
		}
		item.StartDate = snapshot.Conditions.StartDate
		item.EndDate = snapshot.Conditions.EndDate
		item.Legacy = legacy != ""
		item.HasExcel = excel > 0
		item.HasCSV = csv > 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DownloadMonthlySource(reportID, sourceID, kind string) (string, []byte, error) {
	var metadata string
	var excel, csv []byte
	var downloadable int
	err := s.db.QueryRow(`SELECT s.legacy_metadata_json,s.finance_xlsx_blob,s.finance_csv_blob,(SELECT COUNT(*) FROM monthly_report_versions v WHERE v.source_id=s.id AND v.status IN ('finalized','legacy')) FROM monthly_report_sources s WHERE s.id=? AND s.report_id=?`, sourceID, reportID).Scan(&metadata, &excel, &csv, &downloadable)
	if err != nil {
		return "", nil, err
	}
	if metadata == "" && downloadable == 0 {
		return "", nil, ErrMonthlyReadOnly
	}
	var names struct {
		ExcelName string `json:"excelName"`
		CSVName   string `json:"csvName"`
	}
	if metadata != "" {
		if err = json.Unmarshal([]byte(metadata), &names); err != nil {
			return "", nil, err
		}
	}
	if kind == "finance" && len(excel) > 0 {
		name := names.ExcelName
		if name == "" {
			name = "财务统计.xlsx"
		}
		return name, excel, nil
	}
	if kind == "finance-csv" && len(csv) > 0 {
		name := names.CSVName
		if name == "" {
			name = "财务统计.csv"
		}
		return name, csv, nil
	}
	return "", nil, sql.ErrNoRows
}

func (s *Store) DeleteMonthlyDraft(reportID, versionID string) error {
	if s.active.Archived {
		return ErrArchivedSemester
	}
	s.monthlyFilesMu.Lock()
	defer s.monthlyFilesMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, sourceID, latest string
	if err = tx.QueryRow(`SELECT v.status,v.source_id,r.latest_version_id FROM monthly_report_versions v JOIN monthly_reports r ON r.id=v.report_id WHERE v.id=? AND v.report_id=?`, versionID, reportID).Scan(&status, &sourceID, &latest); err != nil {
		return err
	}
	if status != "draft" {
		return ErrMonthlyReadOnly
	}
	if versionID != latest {
		return ErrMonthlyConflict
	}
	if _, err = tx.Exec(`DELETE FROM monthly_report_versions WHERE id=?`, versionID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM monthly_report_sources WHERE id=? AND legacy_metadata_json='' AND NOT EXISTS(SELECT 1 FROM monthly_report_versions WHERE source_id=?)`, sourceID, sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE monthly_reports SET latest_version_id=COALESCE((SELECT id FROM monthly_report_versions WHERE report_id=? ORDER BY version_number DESC LIMIT 1),''),updated_at=CURRENT_TIMESTAMP WHERE id=?`, reportID, reportID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM monthly_reports WHERE id=? AND NOT EXISTS(SELECT 1 FROM monthly_report_sources WHERE report_id=?)`, reportID, reportID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.restoreMonthlyCache(reportID)
}

func ParseMonthlyMoney(value string) (int64, error) {
	value = strings.TrimSpace(value)
	whole, fraction, has := strings.Cut(value, ".")
	if !has {
		fraction = ""
	}
	if whole == "" || len(fraction) > 2 || len(whole) > 9 {
		return 0, monthlyInputErrorf("金额必须为非负数，最多两位小数")
	}
	var cents int64
	for _, r := range whole {
		if r < '0' || r > '9' {
			return 0, monthlyInputErrorf("金额格式无效")
		}
		cents = cents*10 + int64(r-'0')
	}
	cents *= 100
	for len(fraction) < 2 {
		fraction += "0"
	}
	var tail int64
	for _, r := range fraction {
		if r < '0' || r > '9' {
			return 0, monthlyInputErrorf("金额格式无效")
		}
		tail = tail*10 + int64(r-'0')
	}
	return cents + tail, nil
}

func monthlyPackage(files map[string][]byte, names map[string]string) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	kinds := []string{}
	for kind := range files {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		entry, err := writer.Create(names[kind])
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(files[kind]); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Store) DownloadMonthlyFile(reportID, versionID, kind string) (string, []byte, error) {
	var status, files, month string
	var content, labor, workStudy, records []byte
	err := s.db.QueryRow(`SELECT v.status,v.files_json,v.package_blob,r.output_month,v.labor_xlsx_blob,v.work_study_xlsx_blob,v.records_zip_blob FROM monthly_report_versions v JOIN monthly_reports r ON r.id=v.report_id WHERE v.id=? AND v.report_id=?`, versionID, reportID).Scan(&status, &files, &content, &month, &labor, &workStudy, &records)
	if err != nil {
		return "", nil, err
	}
	var names map[string]string
	if err = json.Unmarshal([]byte(files), &names); err != nil {
		return "", nil, err
	}
	name, ok := names[kind]
	if !ok {
		if status == "draft" && (kind == "labor" || kind == "work-study" || kind == "records") {
			return "", nil, MonthlyValidationError{Issues: []string{"报表尚未生成，请先核对草稿并点击生成/更新报表"}}
		}
		return "", nil, sql.ErrNoRows
	}
	if status != "legacy" {
		data := map[string][]byte{"labor": labor, "work-study": workStudy, "records": records}[kind]
		if len(data) == 0 {
			return "", nil, sql.ErrNoRows
		}
		return name, data, nil
	}
	downloadName := name
	if month != "" && (kind == "labor" || kind == "work-study" || kind == "records") {
		if currentNames, e := monthlyOutputNames(month); e == nil {
			downloadName = currentNames[kind]
		}
	}
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", nil, err
	}
	for _, entry := range archive.File {
		if entry.Name == name {
			r, err := entry.Open()
			if err != nil {
				return "", nil, err
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			return downloadName, data, err
		}
	}
	return "", nil, sql.ErrNoRows
}
