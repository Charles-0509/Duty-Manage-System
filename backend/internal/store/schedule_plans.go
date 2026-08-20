package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

var ErrSchedulePlanNameConflict = errors.New("排班表名称已存在")
var ErrPublishedSchedulePlan = errors.New("已发布的排班表不能删除，请先发布另一张排班表")
var ErrInvalidScheduleWorkbook = errors.New("不是有效的新版 DMS 排班表")

func (s *Store) ListSchedulePlans() ([]types.SchedulePlanSummary, error) {
	rows, err := s.db.Query(`
		SELECT id, name, is_published, created_at, updated_at
		FROM schedule_plans
		ORDER BY is_published DESC, updated_at DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]types.SchedulePlanSummary, 0)
	for rows.Next() {
		var item types.SchedulePlanSummary
		var published int
		if err := rows.Scan(&item.ID, &item.Name, &published, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IsPublished = published == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetSchedulePlan(id string) (types.SchedulePlanResponse, error) {
	var result types.SchedulePlanResponse
	var published int
	if err := s.db.QueryRow(`
		SELECT id, name, is_published, created_at, updated_at
		FROM schedule_plans WHERE id = ?
	`, id).Scan(&result.Plan.ID, &result.Plan.Name, &published, &result.Plan.CreatedAt, &result.Plan.UpdatedAt); err != nil {
		return result, err
	}
	result.Plan.IsPublished = published == 1

	schedule, err := s.getScheduleByPlanID(id)
	if err != nil {
		return result, err
	}
	result.Schedule = schedule
	result.ShiftDistribution = buildShiftDistribution(schedule)
	return result, nil
}

func (s *Store) CreateSchedulePlan(name string, schedule map[string][]string) (types.SchedulePlanSummary, error) {
	name, err := validateSchedulePlanInput(name, schedule)
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := s.validateScheduleMembers(schedule); err != nil {
		return types.SchedulePlanSummary{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	defer tx.Rollback()
	if exists, err := schedulePlanNameExists(tx, name, ""); err != nil {
		return types.SchedulePlanSummary{}, err
	} else if exists {
		return types.SchedulePlanSummary{}, ErrSchedulePlanNameConflict
	}

	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO schedule_plans (id, name) VALUES (?, ?)`, id, name); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := writeScheduleEntries(tx, id, schedule); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	result, err := s.GetSchedulePlan(id)
	return result.Plan, err
}

func (s *Store) UpdateSchedulePlan(id, name string, schedule map[string][]string) (types.SchedulePlanSummary, error) {
	name, err := validateSchedulePlanInput(name, schedule)
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := s.validateScheduleMembers(schedule); err != nil {
		return types.SchedulePlanSummary{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	defer tx.Rollback()
	if exists, err := schedulePlanNameExists(tx, name, id); err != nil {
		return types.SchedulePlanSummary{}, err
	} else if exists {
		return types.SchedulePlanSummary{}, ErrSchedulePlanNameConflict
	}
	result, err := tx.Exec(`UPDATE schedule_plans SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, id)
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return types.SchedulePlanSummary{}, sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM schedule_entries WHERE schedule_plan_id = ?`, id); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := writeScheduleEntries(tx, id, schedule); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	updated, err := s.GetSchedulePlan(id)
	return updated.Plan, err
}

func (s *Store) RenameSchedulePlan(id, name string) (types.SchedulePlanSummary, error) {
	name, err := validateSchedulePlanName(name)
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	defer tx.Rollback()
	if exists, err := schedulePlanNameExists(tx, name, id); err != nil {
		return types.SchedulePlanSummary{}, err
	} else if exists {
		return types.SchedulePlanSummary{}, ErrSchedulePlanNameConflict
	}
	result, err := tx.Exec(`UPDATE schedule_plans SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, id)
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return types.SchedulePlanSummary{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	updated, err := s.GetSchedulePlan(id)
	return updated.Plan, err
}

func (s *Store) DeleteSchedulePlan(id string) error {
	var published int
	if err := s.db.QueryRow(`SELECT is_published FROM schedule_plans WHERE id = ?`, id).Scan(&published); err != nil {
		return err
	}
	if published == 1 {
		return ErrPublishedSchedulePlan
	}
	_, err := s.db.Exec(`DELETE FROM schedule_plans WHERE id = ?`, id)
	return err
}

func (s *Store) PublishSchedulePlan(id string) (types.SchedulePlanSummary, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return types.SchedulePlanSummary{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM schedule_plans WHERE id = ?`, id).Scan(&exists); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if exists == 0 {
		return types.SchedulePlanSummary{}, sql.ErrNoRows
	}
	if _, err := tx.Exec(`UPDATE schedule_plans SET is_published = 0 WHERE is_published = 1`); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if _, err := tx.Exec(`UPDATE schedule_plans SET is_published = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return types.SchedulePlanSummary{}, err
	}
	result, err := s.GetSchedulePlan(id)
	return result.Plan, err
}

func (s *Store) ExportSchedulePlanWorkbook(id string) (string, []byte, error) {
	plan, err := s.GetSchedulePlan(id)
	if err != nil {
		return "", nil, err
	}
	content, err := buildScheduleWorkbook(plan.Schedule, plan.Plan.Name)
	if err != nil {
		return "", nil, err
	}
	return safeSchedulePlanFilename(plan.Plan.Name) + ".xlsx", content, nil
}

func (s *Store) ImportSchedulePlanWorkbook(name string, content []byte) (types.SchedulePlanSummary, error) {
	file, err := excelize.OpenReader(bytes.NewReader(content), excelize.Options{
		UnzipSizeLimit:    20 * 1024 * 1024,
		UnzipXMLSizeLimit: 5 * 1024 * 1024,
	})
	if err != nil {
		return types.SchedulePlanSummary{}, ErrInvalidScheduleWorkbook
	}
	defer file.Close()
	marker, err := file.GetCellValue("_DMS", "A1")
	if err != nil || marker != "DMS_SCHEDULE_PLAN" {
		return types.SchedulePlanSummary{}, ErrInvalidScheduleWorkbook
	}
	versionRaw, err := file.GetCellValue("_DMS", "B1")
	if err != nil {
		return types.SchedulePlanSummary{}, ErrInvalidScheduleWorkbook
	}
	version, err := strconv.Atoi(versionRaw)
	if err != nil || version != 1 {
		return types.SchedulePlanSummary{}, ErrInvalidScheduleWorkbook
	}
	rows, err := file.GetRows("_DMS")
	if err != nil || len(rows) < 4 {
		return types.SchedulePlanSummary{}, ErrInvalidScheduleWorkbook
	}

	schedule := map[string][]string{}
	for rowIndex, row := range rows[4:] {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		if len(row) < 3 {
			return types.SchedulePlanSummary{}, fmt.Errorf("%w：数据页第 %d 行不完整", ErrInvalidScheduleWorkbook, rowIndex+5)
		}
		shiftCode := strings.TrimSpace(row[0])
		realName := strings.TrimSpace(row[1])
		weekType := strings.TrimSpace(row[2])
		if !validScheduleShiftCode(shiftCode) || realName == "" {
			return types.SchedulePlanSummary{}, fmt.Errorf("%w：数据页第 %d 行无效", ErrInvalidScheduleWorkbook, rowIndex+5)
		}
		label := realName
		switch weekType {
		case "single":
			label += "(单)"
		case "double":
			label += "(双)"
		case "both":
			label += "(单双)"
		default:
			return types.SchedulePlanSummary{}, fmt.Errorf("%w：数据页第 %d 行单双周类型无效", ErrInvalidScheduleWorkbook, rowIndex+5)
		}
		schedule[shiftCode] = append(schedule[shiftCode], label)
	}
	return s.CreateSchedulePlan(name, schedule)
}

func (s *Store) getScheduleByPlanID(id string) (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT entries.shift_code, entries.real_name, entries.week_type
		FROM schedule_entries AS entries
		JOIN users AS members ON members.id = entries.member_id
		WHERE entries.schedule_plan_id = ? AND members.is_active = 1 AND members.role != 'ADMIN'
		ORDER BY entries.shift_code ASC, entries.real_name ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schedule := map[string][]string{}
	for rows.Next() {
		var shiftCode, realName, weekType string
		if err := rows.Scan(&shiftCode, &realName, &weekType); err != nil {
			return nil, err
		}
		label := realName
		switch weekType {
		case "single":
			label += "(单)"
		case "double":
			label += "(双)"
		case "both":
			label += "(单双)"
		}
		schedule[shiftCode] = append(schedule[shiftCode], label)
	}
	return schedule, rows.Err()
}

func (s *Store) validateScheduleMembers(schedule map[string][]string) error {
	names := make([]string, 0)
	for _, labels := range schedule {
		for _, label := range uniqueStrings(labels) {
			name, _ := parseScheduleLabel(label)
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return s.validateCurrentSemesterMemberNames(names)
}

func validateSchedulePlanInput(name string, schedule map[string][]string) (string, error) {
	name, err := validateSchedulePlanName(name)
	if err != nil {
		return "", err
	}
	for shiftCode := range schedule {
		if !validScheduleShiftCode(shiftCode) {
			return "", fmt.Errorf("无效班次：%s", shiftCode)
		}
	}
	return name, nil
}

func validateSchedulePlanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("排班表名称不能为空")
	}
	if len([]rune(name)) > 100 {
		return "", fmt.Errorf("排班表名称不能超过 100 个字符")
	}
	return name, nil
}

func validScheduleShiftCode(shiftCode string) bool {
	for _, dayCode := range config.WeekdaysCode {
		for shiftIndex := range config.TimeSlots {
			if shiftCode == fmt.Sprintf("%s-%d", dayCode, shiftIndex+1) {
				return true
			}
		}
	}
	return false
}

func schedulePlanNameExists(tx *sql.Tx, name, excludeID string) (bool, error) {
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM schedule_plans WHERE name = ? AND id != ?`, name, excludeID).Scan(&count)
	return count > 0, err
}

func writeScheduleEntries(tx *sql.Tx, planID string, schedule map[string][]string) error {
	stmt, err := tx.Prepare(`
		INSERT INTO schedule_entries (schedule_plan_id, shift_code, real_name, member_id, week_type, created_at)
		SELECT ?, ?, real_name, id, ?, CURRENT_TIMESTAMP
		FROM users
		WHERE real_name = ? AND is_active = 1 AND role != 'ADMIN'
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for shiftCode, labels := range schedule {
		for _, label := range uniqueStrings(labels) {
			realName, weekType := parseScheduleLabel(label)
			if realName == "" {
				continue
			}
			result, err := stmt.Exec(planID, shiftCode, weekType, realName)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected == 0 {
				return fmt.Errorf("成员 %s 已不属于当前学期", realName)
			}
		}
	}
	return nil
}

func safeSchedulePlanFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\\/:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "" {
		return "排班表"
	}
	return name
}
