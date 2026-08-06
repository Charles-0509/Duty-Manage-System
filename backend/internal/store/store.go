package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	allowedMonthStart = "2026-04"
	allowedMonthEnd   = "2050-12"
	dutyHourlyRate    = 25.0
	workOrderRate     = 50.0
)

var ErrMonthOutOfRange = errors.New("month out of allowed range")
var ErrInvalidDateRange = errors.New("invalid date range")

type workOrderAggregation struct {
	perOrderUsers map[string]map[string]float64
	userHours     map[string]float64
	userAmounts   map[string]float64
	orderTotals   map[string]float64
	detailsByUser map[string][]types.FinanceWorkOrderDetail
}

type dutyCSVEntry struct {
	Name       string
	Date       time.Time
	ShiftIndex int
	StartTime  string
	EndTime    string
	Hours      float64
}

type csvTimeBlock struct {
	Start int
	End   int
}

type csvScheduleAllocator struct {
	occupied map[string]map[string][]csvTimeBlock
}

type csvManagementPerson struct {
	Name string
	Role string
}

type Store struct {
	mu      sync.RWMutex
	db      *sql.DB
	control *sql.DB
	cfg     config.AppConfig
	active  types.SemesterSummary
}

func New(cfg config.AppConfig) (*Store, error) {
	return openManagedStore(cfg)
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	if s.db != nil {
		firstErr = s.db.Close()
	}
	if s.control != nil {
		if err := s.control.Close(); firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Store) initSchema() error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_uuid TEXT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			real_name TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'USER',
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			must_change_password INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`
		CREATE TABLE IF NOT EXISTS availability_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			real_name TEXT NOT NULL,
			week_type TEXT NOT NULL,
			shift_code TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(real_name, week_type, shift_code)
		);`,
		`
		CREATE TABLE IF NOT EXISTS schedule_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			shift_code TEXT NOT NULL,
			real_name TEXT NOT NULL,
			week_type TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(shift_code, real_name, week_type)
		);`,
		`
		CREATE TABLE IF NOT EXISTS final_schedules (
			week_number INTEGER PRIMARY KEY,
			selected_date TEXT NOT NULL,
			updated_by TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`
		CREATE TABLE IF NOT EXISTS final_schedule_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			week_number INTEGER NOT NULL,
			shift_code TEXT NOT NULL,
			real_name TEXT NOT NULL,
			UNIQUE(week_number, shift_code, real_name),
			FOREIGN KEY (week_number) REFERENCES final_schedules(week_number) ON DELETE CASCADE
		);`,
		`
		CREATE TABLE IF NOT EXISTS work_orders (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			belonging_month TEXT NOT NULL,
			created_time TEXT NOT NULL,
			created_by TEXT NOT NULL
		);`,
		`
		CREATE TABLE IF NOT EXISTS work_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_order_id TEXT NOT NULL,
			date TEXT NOT NULL,
			worker_name TEXT NOT NULL,
			duration REAL NOT NULL,
			FOREIGN KEY (work_order_id) REFERENCES work_orders(id) ON DELETE CASCADE
		);`,
		`
		CREATE TABLE IF NOT EXISTS labor_conversion_runs (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			input_filename TEXT NOT NULL,
			output_name TEXT NOT NULL,
			csv_name TEXT NOT NULL DEFAULT '',
			target_total_cents INTEGER NOT NULL,
			original_total_cents INTEGER NOT NULL,
			final_total_cents INTEGER NOT NULL,
			team_fund_cents INTEGER NOT NULL,
			seed INTEGER,
			csv_output_month TEXT NOT NULL DEFAULT '',
			source_finance_batch_id TEXT NOT NULL DEFAULT '',
			local_output_dir TEXT NOT NULL DEFAULT '',
			parent_run_id TEXT NOT NULL DEFAULT '',
			is_manual_adjust INTEGER NOT NULL DEFAULT 0,
			people_json TEXT NOT NULL DEFAULT '',
			result_json TEXT NOT NULL,
			workbook_blob BLOB NOT NULL,
			csv_blob BLOB
		);`,
	}

	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}

	if err := s.ensureLaborConversionRunColumns(); err != nil {
		return err
	}

	return nil
}

func (s *Store) ensureLaborConversionRunColumns() error {
	columns := map[string]string{
		"csv_name":                "TEXT NOT NULL DEFAULT ''",
		"csv_output_month":        "TEXT NOT NULL DEFAULT ''",
		"source_finance_batch_id": "TEXT NOT NULL DEFAULT ''",
		"local_output_dir":        "TEXT NOT NULL DEFAULT ''",
		"parent_run_id":           "TEXT NOT NULL DEFAULT ''",
		"is_manual_adjust":        "INTEGER NOT NULL DEFAULT 0",
		"people_json":             "TEXT NOT NULL DEFAULT ''",
		"csv_blob":                "BLOB",
	}

	existing := map[string]struct{}{}
	rows, err := s.db.Query(`PRAGMA table_info(labor_conversion_runs)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for column, definition := range columns {
		if _, ok := existing[column]; ok {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE labor_conversion_runs ADD COLUMN %s %s`, column, definition)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedUsers() error {
	var semesterCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&semesterCount); err != nil {
		return err
	}
	if semesterCount == 0 {
		for index, user := range config.DefaultUsers(s.cfg.AdminPassword) {
			passwordHash, err := hashPassword(user.Password)
			if err != nil {
				return err
			}
			mustChange := boolToInt(user.MustChangePassword)
			if _, err := s.db.Exec(`INSERT INTO users (username, password_hash, real_name, role, sort_order, is_active, must_change_password) VALUES (?, ?, ?, ?, ?, 1, ?)`, user.Username, passwordHash, user.RealName, user.Role, index+1, mustChange); err != nil {
				return err
			}
		}
	}

	type semesterAccount struct {
		id           int64
		accountUUID  sql.NullString
		username     string
		passwordHash string
		role         string
		active       int
		mustChange   int
		createdAt    string
	}
	rows, err := s.db.Query(`SELECT id, account_uuid, username, password_hash, role, is_active, must_change_password, created_at FROM users`)
	if err != nil {
		return err
	}
	accounts := make([]semesterAccount, 0)
	for rows.Next() {
		var item semesterAccount
		if err := rows.Scan(&item.id, &item.accountUUID, &item.username, &item.passwordHash, &item.role, &item.active, &item.mustChange, &item.createdAt); err != nil {
			rows.Close()
			return err
		}
		accounts = append(accounts, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range accounts {
		var globalUUID string
		err := s.control.QueryRow(`SELECT account_uuid FROM accounts WHERE username = ?`, item.username).Scan(&globalUUID)
		if err == sql.ErrNoRows {
			globalUUID = strings.TrimSpace(item.accountUUID.String)
			if _, parseErr := uuid.Parse(globalUUID); parseErr != nil {
				globalUUID = uuid.NewString()
			}
			passwordHash := strings.TrimSpace(item.passwordHash)
			if passwordHash == "" {
				passwordHash, err = hashPassword(item.username)
				if err != nil {
					return err
				}
			}
			if _, err := s.control.Exec(`INSERT INTO accounts (account_uuid, username, password_hash, is_active, must_change_password, is_system_admin, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, globalUUID, item.username, passwordHash, item.active, item.mustChange, boolToInt(item.role == "ADMIN"), item.createdAt); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if item.accountUUID.Valid && strings.TrimSpace(item.accountUUID.String) != "" && item.accountUUID.String != globalUUID {
			return fmt.Errorf("学期成员 %s 的全局账户 UUID 不一致", item.username)
		}
		if _, err := s.db.Exec(`UPDATE users SET account_uuid = ?, password_hash = '' WHERE id = ?`, globalUUID, item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Authenticate(username, password string) (*types.User, error) {
	_, release := s.AcquireSemesterRequest()
	defer release()
	row := s.control.QueryRow(`SELECT id, account_uuid, username, password_hash, is_active, must_change_password, created_at FROM accounts WHERE username = ?`, username)
	var accountID int64
	var accountUUID, accountUsername string
	var passwordHash string
	var isActive int
	var mustChange int
	var createdAt string
	if err := row.Scan(&accountID, &accountUUID, &accountUsername, &passwordHash, &isActive, &mustChange, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("用户名或密码错误")
		}
		return nil, err
	}

	if isActive == 0 {
		return nil, fmt.Errorf("账号已停用")
	}

	if !verifyPassword(password, passwordHash) {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	return s.userForAccount(accountID, accountUUID, accountUsername, isActive, mustChange, createdAt)
}

func (s *Store) GetUserByID(userID int64) (*types.User, error) {
	row := s.control.QueryRow(`SELECT account_uuid, username, is_active, must_change_password, created_at FROM accounts WHERE id = ?`, userID)
	var accountUUID, username, createdAt string
	var isActive int
	var mustChange int
	if err := row.Scan(&accountUUID, &username, &isActive, &mustChange, &createdAt); err != nil {
		return nil, err
	}
	return s.userForAccount(userID, accountUUID, username, isActive, mustChange, createdAt)
}

func (s *Store) GetGlobalUserByID(userID int64) (*types.User, error) {
	row := s.control.QueryRow(`SELECT username, is_active, must_change_password, is_system_admin, created_at FROM accounts WHERE id = ?`, userID)
	var username, createdAt string
	var active, mustChange, admin int
	if err := row.Scan(&username, &active, &mustChange, &admin, &createdAt); err != nil {
		return nil, err
	}
	if admin != 1 {
		return nil, fmt.Errorf("无权限执行该操作")
	}
	return &types.User{ID: userID, Username: username, RealName: "系统管理员", Role: "ADMIN", IsActive: active == 1, MustChangePassword: mustChange == 1, CreatedAt: createdAt, Permissions: config.PermissionsFor("ADMIN"), SemesterMember: true}, nil
}

func (s *Store) userForAccount(accountID int64, accountUUID, username string, accountActive, mustChange int, createdAt string) (*types.User, error) {
	var localID int64
	var realName, role string
	var memberActive int
	err := s.db.QueryRow(`SELECT id, real_name, role, is_active FROM users WHERE account_uuid = ?`, accountUUID).Scan(&localID, &realName, &role, &memberActive)
	if err != nil {
		return nil, fmt.Errorf("当前学期未包含该用户")
	}
	if memberActive != 1 {
		return nil, fmt.Errorf("当前学期成员已停用")
	}
	user := &types.User{ID: accountID, Username: username, RealName: realName, Role: role, IsActive: accountActive == 1, MustChangePassword: mustChange == 1, CreatedAt: createdAt, Permissions: config.PermissionsFor(role), SemesterMember: true}
	if !user.IsActive {
		return nil, fmt.Errorf("账号已停用")
	}
	_ = localID
	return user, nil
}

func (s *Store) GetUserByUsername(username string) (*types.User, error) {
	var accountID int64
	var accountUUID string
	var active, mustChange int
	var createdAt string
	if err := s.control.QueryRow(`SELECT id, account_uuid, is_active, must_change_password, created_at FROM accounts WHERE username = ?`, username).Scan(&accountID, &accountUUID, &active, &mustChange, &createdAt); err != nil {
		return nil, err
	}
	return s.userForAccount(accountID, accountUUID, username, active, mustChange, createdAt)
}

func (s *Store) GetUserByRealName(realName string) (*types.User, error) {
	var accountUUID string
	if err := s.db.QueryRow(`SELECT account_uuid FROM users WHERE real_name = ?`, realName).Scan(&accountUUID); err != nil {
		return nil, err
	}
	var accountID int64
	var username, createdAt string
	var active, mustChange int
	if err := s.control.QueryRow(`SELECT id, username, is_active, must_change_password, created_at FROM accounts WHERE account_uuid = ?`, accountUUID).Scan(&accountID, &username, &active, &mustChange, &createdAt); err != nil {
		return nil, err
	}
	return s.userForAccount(accountID, accountUUID, username, active, mustChange, createdAt)
}

func (s *Store) ListUsers() ([]types.User, error) {
	rows, err := s.db.Query(`
		SELECT id, account_uuid, username, real_name, role, sort_order, is_active, created_at
		FROM users
		ORDER BY CASE WHEN role = 'ADMIN' THEN 0 ELSE 1 END, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]types.User, 0)
	for rows.Next() {
		var user types.User
		var accountUUID string
		var isActive int
		if err := rows.Scan(
			&user.ID,
			&accountUUID,
			&user.Username,
			&user.RealName,
			&user.Role,
			&user.SortOrder,
			&isActive,
			&user.CreatedAt,
		); err != nil {
			return nil, err
		}
		var accountActive, mustChange int
		if err := s.control.QueryRow(`SELECT is_active, must_change_password FROM accounts WHERE account_uuid = ?`, accountUUID).Scan(&accountActive, &mustChange); err != nil {
			continue
		}
		user.IsActive = accountActive == 1
		user.MustChangePassword = mustChange == 1
		user.Permissions = config.PermissionsFor(user.Role)
		user.SemesterMember = isActive == 1
		users = append(users, user)
	}

	sort.SliceStable(users, func(i, j int) bool {
		if users[i].Role == "ADMIN" && users[j].Role != "ADMIN" {
			return true
		}
		if users[i].Role != "ADMIN" && users[j].Role == "ADMIN" {
			return false
		}
		if users[i].SortOrder != users[j].SortOrder {
			return users[i].SortOrder < users[j].SortOrder
		}
		return config.LessRealName(users[i].RealName, users[j].RealName)
	})

	return users, rows.Err()
}

func (s *Store) UpdateRole(userID int64, role string) error {
	if _, ok := config.AllUserRoles()[role]; !ok {
		return fmt.Errorf("非法角色")
	}

	_, err := s.db.Exec(`
		UPDATE users
		SET role = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND role != 'ADMIN'
	`, role, userID)
	return err
}

func (s *Store) UpdateUserStatus(userID int64, isActive bool) error {
	status := 0
	if isActive {
		status = 1
	}

	var accountUUID string
	if err := s.db.QueryRow(`SELECT account_uuid FROM users WHERE id = ?`, userID).Scan(&accountUUID); err != nil {
		return err
	}
	_, err := s.control.Exec(`UPDATE accounts SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE account_uuid = ?`, status, accountUUID)
	return err
}

func (s *Store) UpdateOwnPassword(userID int64, currentPassword, newPassword string) error {
	row := s.control.QueryRow(`SELECT password_hash FROM accounts WHERE id = ?`, userID)

	var passwordHash string
	if err := row.Scan(&passwordHash); err != nil {
		return err
	}

	if !verifyPassword(currentPassword, passwordHash) {
		return fmt.Errorf("当前密码不正确")
	}

	newHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	_, err = s.control.Exec(`
		UPDATE accounts
		SET password_hash = ?, must_change_password = 0, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, newHash, userID)
	return err
}

func (s *Store) ResetPassword(userID int64, newPassword string) error {
	newHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	var accountUUID string
	if err := s.db.QueryRow(`SELECT account_uuid FROM users WHERE id = ?`, userID).Scan(&accountUUID); err != nil {
		return err
	}
	_, err = s.control.Exec(`UPDATE accounts SET password_hash = ?, must_change_password = 1, updated_at = CURRENT_TIMESTAMP WHERE account_uuid = ?`, newHash, accountUUID)
	return err
}

func (s *Store) GetAvailabilityForUser(realName string) (types.AvailabilityPayload, error) {
	rows, err := s.db.Query(`
		SELECT week_type, shift_code
		FROM availability_entries
		WHERE real_name = ?
	`, realName)
	if err != nil {
		return types.AvailabilityPayload{}, err
	}
	defer rows.Close()

	payload := types.AvailabilityPayload{
		Single: []string{},
		Double: []string{},
	}

	for rows.Next() {
		var weekType string
		var shiftCode string
		if err := rows.Scan(&weekType, &shiftCode); err != nil {
			return payload, err
		}
		if weekType == "single" {
			payload.Single = append(payload.Single, shiftCode)
		}
		if weekType == "double" {
			payload.Double = append(payload.Double, shiftCode)
		}
	}

	sort.Strings(payload.Single)
	sort.Strings(payload.Double)
	return payload, rows.Err()
}

func (s *Store) SaveAvailability(realName string, payload types.SaveAvailabilityRequest) error {
	if err := s.validateCurrentSemesterMemberNames([]string{realName}); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var memberID int64
	if err := tx.QueryRow(`SELECT id FROM users WHERE real_name = ? AND is_active = 1 AND role != 'ADMIN'`, realName).Scan(&memberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("成员 %s 已不属于当前学期", realName)
		}
		return err
	}

	if _, err := tx.Exec(`DELETE FROM availability_entries WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`, memberID, realName); err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO availability_entries (real_name, member_id, week_type, shift_code, created_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for _, shiftCode := range uniqueStrings(payload.Single) {
		if _, err := insertStmt.Exec(realName, memberID, "single", shiftCode); err != nil {
			return err
		}
	}
	for _, shiftCode := range uniqueStrings(payload.Double) {
		if _, err := insertStmt.Exec(realName, memberID, "double", shiftCode); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) GetAvailabilityOverview() ([]types.AvailabilityOverviewItem, error) {
	rows, err := s.db.Query(`
		SELECT members.username, members.real_name, entries.week_type, entries.shift_code
		FROM users AS members
		LEFT JOIN availability_entries AS entries ON entries.member_id = members.id
		WHERE members.is_active = 1 AND members.role != 'ADMIN'
		ORDER BY members.sort_order ASC, members.id ASC, entries.week_type ASC, entries.shift_code ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]types.AvailabilityOverviewItem, 0)
	indexByName := map[string]int{}

	for rows.Next() {
		var username string
		var realName string
		var weekType sql.NullString
		var shiftCode sql.NullString
		if err := rows.Scan(&username, &realName, &weekType, &shiftCode); err != nil {
			return nil, err
		}

		index, ok := indexByName[realName]
		if !ok {
			index = len(items)
			indexByName[realName] = index
			items = append(items, types.AvailabilityOverviewItem{
				Username: username,
				RealName: realName,
				Availability: types.AvailabilityPayload{
					Single: []string{},
					Double: []string{},
				},
			})
		}

		if !weekType.Valid || !shiftCode.Valid {
			continue
		}
		if weekType.String == "single" {
			items[index].Availability.Single = append(items[index].Availability.Single, shiftCode.String)
		}
		if weekType.String == "double" {
			items[index].Availability.Double = append(items[index].Availability.Double, shiftCode.String)
		}
	}

	return items, rows.Err()
}

func (s *Store) GetSchedule() (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT entries.shift_code, entries.real_name, entries.week_type
		FROM schedule_entries AS entries
		JOIN users AS members ON members.id = entries.member_id
		WHERE members.is_active = 1 AND members.role != 'ADMIN'
		ORDER BY entries.shift_code ASC, entries.real_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedule := map[string][]string{}
	for rows.Next() {
		var shiftCode string
		var realName string
		var weekType string
		if err := rows.Scan(&shiftCode, &realName, &weekType); err != nil {
			return nil, err
		}

		label := realName
		if weekType == "single" {
			label += "(单)"
		} else if weekType == "double" {
			label += "(双)"
		} else if weekType == "both" {
			label += "(单双)"
		}
		schedule[shiftCode] = append(schedule[shiftCode], label)
	}

	for shiftCode, users := range schedule {
		sort.Strings(users)
		schedule[shiftCode] = users
	}
	return schedule, rows.Err()
}

func (s *Store) GetScheduleSummary() (types.ScheduleResponse, error) {
	schedule, err := s.GetSchedule()
	if err != nil {
		return types.ScheduleResponse{}, err
	}

	return types.ScheduleResponse{
		Schedule:          schedule,
		ShiftDistribution: buildShiftDistribution(schedule),
	}, nil
}

func (s *Store) SaveSchedule(schedule map[string][]string) error {
	memberNames := make([]string, 0)
	for _, assignedUsers := range schedule {
		for _, label := range uniqueStrings(assignedUsers) {
			realName, _ := parseScheduleLabel(label)
			if realName != "" {
				memberNames = append(memberNames, realName)
			}
		}
	}
	if err := s.validateCurrentSemesterMemberNames(memberNames); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM schedule_entries`); err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO schedule_entries (shift_code, real_name, member_id, week_type, created_at)
		SELECT ?, real_name, id, ?, CURRENT_TIMESTAMP
		FROM users
		WHERE real_name = ? AND is_active = 1 AND role != 'ADMIN'
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for shiftCode, assignedUsers := range schedule {
		for _, label := range uniqueStrings(assignedUsers) {
			realName, weekType := parseScheduleLabel(label)
			if realName == "" {
				continue
			}
			result, err := insertStmt.Exec(shiftCode, weekType, realName)
			if err != nil {
				return err
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				return fmt.Errorf("成员 %s 已不属于当前学期", realName)
			}
		}
	}

	return tx.Commit()
}

func (s *Store) GetFinalSchedule(weekNumber int, selectedDate string) (types.FinalScheduleResponse, error) {
	result := types.FinalScheduleResponse{
		WeekNumber:   weekNumber,
		SelectedDate: selectedDate,
		IsOddWeek:    weekNumber%2 == 1,
		Source:       "generated",
		Schedule:     map[string][]string{},
	}

	row := s.db.QueryRow(`
		SELECT selected_date
		FROM final_schedules
		WHERE week_number = ?
	`, weekNumber)

	var savedDate string
	switch err := row.Scan(&savedDate); err {
	case nil:
		result.SelectedDate = savedDate
		result.Source = "saved"
		entries, err := s.getFinalScheduleEntries(weekNumber)
		if err != nil {
			return result, err
		}
		result.Schedule = entries
		return result, nil
	case sql.ErrNoRows:
	default:
		return result, err
	}

	planned, err := s.getPlannedScheduleForWeek(result.IsOddWeek)
	if err != nil {
		return result, err
	}
	result.Schedule = planned
	return result, nil
}

func (s *Store) SaveFinalSchedule(weekNumber int, payload types.SaveFinalScheduleRequest, updatedBy string) error {
	memberNames := make([]string, 0)
	for _, names := range payload.Schedule {
		memberNames = append(memberNames, names...)
	}
	if err := s.validateCurrentSemesterMemberNames(memberNames); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO final_schedules (week_number, selected_date, updated_by, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(week_number) DO UPDATE SET
			selected_date = excluded.selected_date,
			updated_by = excluded.updated_by,
			updated_at = CURRENT_TIMESTAMP
	`, weekNumber, payload.SelectedDate, updatedBy); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM final_schedule_entries WHERE week_number = ?`, weekNumber); err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO final_schedule_entries (week_number, shift_code, real_name, member_id)
		SELECT ?, ?, real_name, id
		FROM users
		WHERE real_name = ? AND is_active = 1 AND role != 'ADMIN'
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for shiftCode, names := range payload.Schedule {
		for _, realName := range uniqueStrings(names) {
			if realName == "" {
				continue
			}
			result, err := insertStmt.Exec(weekNumber, shiftCode, realName)
			if err != nil {
				return err
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				return fmt.Errorf("成员 %s 已不属于当前学期", realName)
			}
		}
	}

	return tx.Commit()
}

func (s *Store) ListWorkOrders(month string) ([]types.WorkOrder, error) {
	if strings.TrimSpace(month) != "" && !isAllowedMonth(month) {
		return nil, ErrMonthOutOfRange
	}

	query := `
		SELECT id, title, belonging_month, created_time, created_by
		FROM work_orders
	`
	args := []any{}
	if month != "" {
		query += ` WHERE belonging_month = ?`
		args = append(args, month)
	}
	query += ` ORDER BY created_time DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}

	workOrders := make([]types.WorkOrder, 0)
	for rows.Next() {
		var order types.WorkOrder
		if err := rows.Scan(
			&order.ID,
			&order.Title,
			&order.BelongingMonth,
			&order.CreatedTime,
			&order.CreatedBy,
		); err != nil {
			rows.Close()
			return nil, err
		}
		workOrders = append(workOrders, order)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}

	if err := rows.Close(); err != nil {
		return nil, err
	}

	for index := range workOrders {
		sessions, err := s.getWorkSessions(workOrders[index].ID)
		if err != nil {
			return nil, err
		}
		workOrders[index].WorkSessions = sessions
	}

	return workOrders, nil
}

func (s *Store) ListWorkOrdersByIDs(ids []string) ([]types.WorkOrder, error) {
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return []types.WorkOrder{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for index, id := range ids {
		placeholders[index] = "?"
		args[index] = id
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, title, belonging_month, created_time, created_by
		FROM work_orders
		WHERE id IN (%s)
		ORDER BY belonging_month ASC, created_time ASC
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}

	workOrders := make([]types.WorkOrder, 0, len(ids))
	for rows.Next() {
		var order types.WorkOrder
		if err := rows.Scan(
			&order.ID,
			&order.Title,
			&order.BelongingMonth,
			&order.CreatedTime,
			&order.CreatedBy,
		); err != nil {
			rows.Close()
			return nil, err
		}
		workOrders = append(workOrders, order)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for index := range workOrders {
		sessions, err := s.getWorkSessions(workOrders[index].ID)
		if err != nil {
			return nil, err
		}
		workOrders[index].WorkSessions = sessions
	}

	return workOrders, nil
}

func (s *Store) CreateWorkOrder(request types.SaveWorkOrderRequest, createdBy string) (types.WorkOrder, error) {
	workOrder := types.WorkOrder{
		ID:             fmt.Sprintf("WO_%d", time.Now().UnixNano()),
		Title:          strings.TrimSpace(request.Title),
		BelongingMonth: strings.TrimSpace(request.BelongingMonth),
		CreatedTime:    time.Now().Format("2006-01-02 15:04:05"),
		CreatedBy:      createdBy,
		WorkSessions:   sanitizeSessions(request.WorkSessions),
	}

	if workOrder.Title == "" {
		return workOrder, fmt.Errorf("工单标题不能为空")
	}
	if !isAllowedMonth(workOrder.BelongingMonth) {
		return workOrder, ErrMonthOutOfRange
	}
	if len(workOrder.WorkSessions) == 0 {
		return workOrder, fmt.Errorf("请至少提供一条有效工时记录")
	}
	if err := s.validateCurrentSemesterMemberNames(workSessionMemberNames(workOrder.WorkSessions)); err != nil {
		return workOrder, err
	}

	if err := s.persistWorkOrder(workOrder); err != nil {
		return workOrder, err
	}
	return workOrder, nil
}

func (s *Store) UpdateWorkOrder(id string, request types.SaveWorkOrderRequest) (types.WorkOrder, error) {
	row := s.db.QueryRow(`
		SELECT id, created_time, created_by
		FROM work_orders
		WHERE id = ?
	`, id)

	var createdTime string
	var createdBy string
	var workOrderID string

	if err := row.Scan(&workOrderID, &createdTime, &createdBy); err != nil {
		return types.WorkOrder{}, err
	}

	workOrder := types.WorkOrder{
		ID:             id,
		Title:          strings.TrimSpace(request.Title),
		BelongingMonth: strings.TrimSpace(request.BelongingMonth),
		CreatedTime:    createdTime,
		CreatedBy:      createdBy,
		WorkSessions:   sanitizeSessions(request.WorkSessions),
	}

	if workOrder.Title == "" {
		return workOrder, fmt.Errorf("工单标题不能为空")
	}
	if !isAllowedMonth(workOrder.BelongingMonth) {
		return workOrder, ErrMonthOutOfRange
	}
	if len(workOrder.WorkSessions) == 0 {
		return workOrder, fmt.Errorf("请至少提供一条有效工时记录")
	}
	if err := s.validateCurrentSemesterMemberNames(workSessionMemberNames(workOrder.WorkSessions)); err != nil {
		return workOrder, err
	}

	if err := s.persistWorkOrder(workOrder); err != nil {
		return workOrder, err
	}
	return workOrder, nil
}

func (s *Store) DeleteWorkOrder(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM work_sessions WHERE work_order_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM work_orders WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetDashboard() (types.DashboardResponse, error) {
	availabilityCount := 0
	if err := s.db.QueryRow(`
		SELECT COUNT(DISTINCT entries.member_id)
		FROM availability_entries AS entries
		JOIN users AS members ON members.id = entries.member_id
		WHERE members.is_active = 1 AND members.role != 'ADMIN'
	`).Scan(&availabilityCount); err != nil {
		return types.DashboardResponse{}, err
	}

	schedule, err := s.GetSchedule()
	if err != nil {
		return types.DashboardResponse{}, err
	}

	workOrders, err := s.ListWorkOrders("")
	if err != nil {
		return types.DashboardResponse{}, err
	}
	memberNames, err := s.currentSemesterMemberNames()
	if err != nil {
		return types.DashboardResponse{}, err
	}
	workOrders = filterWorkOrdersByMemberNames(workOrders, memberNames)

	totalAssignedShifts := 0
	for _, labels := range schedule {
		totalAssignedShifts += len(labels)
	}

	workloadStats := map[string]float64{}
	for _, order := range workOrders {
		for _, session := range order.WorkSessions {
			workloadStats[session.WorkerName] += session.Duration
		}
	}

	return types.DashboardResponse{
		AvailabilityUserCount: availabilityCount,
		TotalAssignedShifts:   totalAssignedShifts,
		WorkOrderCount:        len(workOrders),
		Schedule:              schedule,
		ShiftDistribution:     buildShiftDistribution(schedule),
		WorkDurationShare:     sortedChartItems(workloadStats),
	}, nil
}

func (s *Store) GetFinanceSummary(month, realName, role string) (types.FinanceSummaryResponse, error) {
	if strings.TrimSpace(month) == "" {
		month = time.Now().Format("2006-01")
	}
	if !isAllowedMonth(month) {
		return types.FinanceSummaryResponse{}, ErrMonthOutOfRange
	}

	workOrders, err := s.ListWorkOrders(month)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	if strings.TrimSpace(realName) == "" {
		return s.getAggregateFinanceSummary(month, workOrders)
	}

	workOrderStats := summarizeWorkOrders(workOrders)
	details := workOrderStats.detailsByUser[realName]
	workOrderHours := workOrderStats.userHours[realName]

	dutyHours, err := s.getMonthlyDutyHours(month, realName)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	managementAmount, managementPending := calculateManagementAmount(month, role, time.Now())

	dutyAmount := dutyHours * dutyHourlyRate
	workOrderAmount := workOrderStats.userAmounts[realName]

	return types.FinanceSummaryResponse{
		Month:             month,
		DutyHours:         dutyHours,
		DutyAmount:        dutyAmount,
		WorkOrderHours:    workOrderHours,
		WorkOrderAmount:   workOrderAmount,
		ManagementAmount:  managementAmount,
		ManagementPending: managementPending,
		TotalAmount:       dutyAmount + workOrderAmount + managementAmount,
		WorkOrderDetails:  details,
	}, nil
}

func (s *Store) getAggregateFinanceSummary(month string, workOrders []types.WorkOrder) (types.FinanceSummaryResponse, error) {
	users, err := s.financeSummaryUsers()
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}
	targetNames := userRealNames(users)
	workOrders = filterWorkOrdersByMemberNames(workOrders, targetNames)
	workOrderStats := summarizeWorkOrders(workOrders)
	dutyHoursByUser, err := s.getMonthlyDutyHoursForUsers(month, targetNames)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	var dutyHours float64
	var workOrderHours float64
	var workOrderAmount float64
	var managementAmount float64
	managementPending := false
	now := time.Now()
	for _, user := range users {
		dutyHours += dutyHoursByUser[user.RealName]
		workOrderHours += workOrderStats.userHours[user.RealName]
		workOrderAmount += workOrderStats.userAmounts[user.RealName]
		amount, pending := calculateManagementAmount(month, user.Role, now)
		managementAmount += amount
		managementPending = managementPending || pending
	}
	dutyAmount := dutyHours * dutyHourlyRate

	return types.FinanceSummaryResponse{
		Month:             month,
		DutyHours:         dutyHours,
		DutyAmount:        dutyAmount,
		WorkOrderHours:    workOrderHours,
		WorkOrderAmount:   workOrderAmount,
		ManagementAmount:  managementAmount,
		ManagementPending: managementPending,
		TotalAmount:       dutyAmount + workOrderAmount + managementAmount,
		WorkOrderDetails:  aggregateFinanceWorkOrderDetails(workOrderStats.detailsByUser, targetNames),
	}, nil
}

func (s *Store) GetFinanceSummaryForRange(startDate, endDate string, workOrderIDs []string, includeManagement bool, managementMonths int, realName, role string) (types.FinanceSummaryResponse, error) {
	start, end, err := parseAllowedDateRange(startDate, endDate)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	workOrders, err := s.ListWorkOrdersByIDs(workOrderIDs)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}
	workOrders = filterWorkOrdersByDateRangeExportMonths(workOrders, start, end)

	if strings.TrimSpace(realName) == "" {
		return s.getAggregateFinanceSummaryForRange(startDate, endDate, start, end, workOrders, includeManagement, managementMonths)
	}

	workOrderStats := summarizeWorkOrders(workOrders)
	details := workOrderStats.detailsByUser[realName]
	workOrderHours := workOrderStats.userHours[realName]

	dutyHoursByUser, err := s.getDutyHoursForUsersInDateRange(start, end, []string{realName})
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}
	dutyHours := dutyHoursByUser[realName]
	dutyAmount := dutyHours * dutyHourlyRate
	workOrderAmount := workOrderStats.userAmounts[realName]

	managementAmount := 0.0
	if includeManagement {
		managementAmount = calculateManagementAmountForMonthCount(role, managementMonths)
	}

	return types.FinanceSummaryResponse{
		Month:             fmt.Sprintf("%s 至 %s", startDate, endDate),
		StartDate:         startDate,
		EndDate:           endDate,
		DutyHours:         dutyHours,
		DutyAmount:        dutyAmount,
		WorkOrderHours:    workOrderHours,
		WorkOrderAmount:   workOrderAmount,
		ManagementAmount:  managementAmount,
		ManagementPending: false,
		TotalAmount:       dutyAmount + workOrderAmount + managementAmount,
		WorkOrderDetails:  details,
	}, nil
}

func (s *Store) getAggregateFinanceSummaryForRange(startDate, endDate string, start, end time.Time, workOrders []types.WorkOrder, includeManagement bool, managementMonths int) (types.FinanceSummaryResponse, error) {
	users, err := s.financeSummaryUsers()
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}
	targetNames := userRealNames(users)
	workOrders = filterWorkOrdersByMemberNames(workOrders, targetNames)
	workOrderStats := summarizeWorkOrders(workOrders)
	dutyHoursByUser, err := s.getDutyHoursForUsersInDateRange(start, end, targetNames)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	var dutyHours float64
	var workOrderHours float64
	var workOrderAmount float64
	var managementAmount float64
	for _, user := range users {
		dutyHours += dutyHoursByUser[user.RealName]
		workOrderHours += workOrderStats.userHours[user.RealName]
		workOrderAmount += workOrderStats.userAmounts[user.RealName]
		if includeManagement {
			managementAmount += calculateManagementAmountForMonthCount(user.Role, managementMonths)
		}
	}
	dutyAmount := dutyHours * dutyHourlyRate

	return types.FinanceSummaryResponse{
		Month:             fmt.Sprintf("%s 至 %s", startDate, endDate),
		StartDate:         startDate,
		EndDate:           endDate,
		DutyHours:         dutyHours,
		DutyAmount:        dutyAmount,
		WorkOrderHours:    workOrderHours,
		WorkOrderAmount:   workOrderAmount,
		ManagementAmount:  managementAmount,
		ManagementPending: false,
		TotalAmount:       dutyAmount + workOrderAmount + managementAmount,
		WorkOrderDetails:  aggregateFinanceWorkOrderDetails(workOrderStats.detailsByUser, targetNames),
	}, nil
}

func (s *Store) financeSummaryUsers() ([]types.User, error) {
	users, err := s.ListUsers()
	if err != nil {
		return nil, err
	}
	targets := make([]types.User, 0, len(users))
	for _, user := range users {
		if !user.SemesterMember || user.Role == "ADMIN" {
			continue
		}
		targets = append(targets, user)
	}
	return targets, nil
}

func userRealNames(users []types.User) []string {
	names := make([]string, 0, len(users))
	for _, user := range users {
		names = append(names, user.RealName)
	}
	return names
}

func (s *Store) currentSemesterMemberNames() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT real_name
		FROM users
		WHERE is_active = 1 AND role != 'ADMIN'
		ORDER BY sort_order, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, strings.TrimSpace(name))
	}
	return names, rows.Err()
}

func (s *Store) validateCurrentSemesterMemberNames(names []string) error {
	activeNames, err := s.currentSemesterMemberNames()
	if err != nil {
		return err
	}
	activeSet := make(map[string]struct{}, len(activeNames))
	for _, name := range activeNames {
		activeSet[name] = struct{}{}
	}

	missing := make([]string, 0)
	for _, name := range uniqueStrings(names) {
		if _, ok := activeSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Slice(missing, func(i, j int) bool { return config.LessRealName(missing[i], missing[j]) })
		return fmt.Errorf("以下成员不属于当前学期：%s", strings.Join(missing, "、"))
	}
	return nil
}

func workSessionMemberNames(sessions []types.WorkSession) []string {
	names := make([]string, 0, len(sessions))
	for _, session := range sessions {
		names = append(names, session.WorkerName)
	}
	return names
}

func aggregateFinanceWorkOrderDetails(detailsByUser map[string][]types.FinanceWorkOrderDetail, names []string) []types.FinanceWorkOrderDetail {
	details := make([]types.FinanceWorkOrderDetail, 0)
	for _, name := range names {
		for _, detail := range detailsByUser[name] {
			detail.Title = fmt.Sprintf("%s - %s", name, detail.Title)
			details = append(details, detail)
		}
	}
	return details
}

func (s *Store) getMonthlyDutyHours(month, realName string) (float64, error) {
	hoursByUser, err := s.getMonthlyDutyHoursForUsers(month, []string{realName})
	if err != nil {
		return 0, err
	}

	return hoursByUser[realName], nil
}

func (s *Store) getMonthlyDutyHoursForUsers(month string, realNames []string) (map[string]float64, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("invalid month: %w", err)
	}
	end := start.AddDate(0, 1, -1)

	return s.getDutyHoursForUsersInDateRange(start, end, realNames)
}

func (s *Store) getDutyHoursForUsersInDateRange(start, end time.Time, realNames []string) (map[string]float64, error) {
	targetNames := uniqueStrings(realNames)
	result := make(map[string]float64, len(targetNames))
	if len(targetNames) == 0 {
		return result, nil
	}

	targetSet := make(map[string]struct{}, len(targetNames))
	for _, realName := range targetNames {
		targetSet[realName] = struct{}{}
		result[realName] = 0
	}

	scheduleCache := map[int]map[string][]string{}

	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dayCode, ok := weekdayCodeForDate(current)
		if !ok {
			continue
		}

		weekNumber := calculateWeekNumber(current, s.cfg.FirstMonday)
		schedule, ok := scheduleCache[weekNumber]
		if !ok {
			financeSchedule, err := s.GetFinalSchedule(weekNumber, current.Format("2006-01-02"))
			if err != nil {
				return nil, err
			}
			schedule = financeSchedule.Schedule
			scheduleCache[weekNumber] = schedule
		}

		for shiftCode, names := range schedule {
			if !strings.HasPrefix(shiftCode, dayCode+"-") {
				continue
			}

			duration := shiftDurationHours(shiftCode)
			if duration <= 0 {
				continue
			}

			for _, name := range names {
				if _, ok := targetSet[name]; !ok {
					continue
				}
				result[name] += duration
			}
		}
	}

	for name, total := range result {
		result[name] = math.Round(total*10) / 10
	}

	return result, nil
}

func (s *Store) getDutyCSVEntriesInDateRange(start, end time.Time) ([]dutyCSVEntry, error) {
	entries := []dutyCSVEntry{}
	scheduleCache := map[int]map[string][]string{}

	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dayCode, ok := weekdayCodeForDate(current)
		if !ok {
			continue
		}

		weekNumber := calculateWeekNumber(current, s.cfg.FirstMonday)
		schedule, ok := scheduleCache[weekNumber]
		if !ok {
			financeSchedule, err := s.GetFinalSchedule(weekNumber, current.Format("2006-01-02"))
			if err != nil {
				return nil, err
			}
			schedule = financeSchedule.Schedule
			scheduleCache[weekNumber] = schedule
		}

		for shiftCode, names := range schedule {
			if !strings.HasPrefix(shiftCode, dayCode+"-") {
				continue
			}

			shiftIndex := shiftIndexFromCode(shiftCode)
			startTime, endTime, ok := shiftTimeRange(shiftCode)
			if !ok {
				continue
			}

			for _, name := range uniqueStrings(names) {
				if strings.TrimSpace(name) == "" {
					continue
				}
				entries = append(entries, dutyCSVEntry{
					Name:       name,
					Date:       current,
					ShiftIndex: shiftIndex,
					StartTime:  startTime,
					EndTime:    endTime,
					Hours:      shiftDurationHours(shiftCode),
				})
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return config.LessRealName(entries[i].Name, entries[j].Name)
		}
		if !entries[i].Date.Equal(entries[j].Date) {
			return entries[i].Date.Before(entries[j].Date)
		}
		return entries[i].ShiftIndex < entries[j].ShiftIndex
	})

	return entries, nil
}

func calculateWeekNumber(date time.Time, firstMonday string) int {
	base, err := time.Parse("20060102", firstMonday)
	if err != nil {
		return 1
	}

	base = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location())
	current := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	delta := int(current.Sub(base).Hours() / 24)
	if delta < 0 {
		return 1
	}

	return delta/7 + 1
}

func weekdayCodeForDate(date time.Time) (string, bool) {
	switch date.Weekday() {
	case time.Monday:
		return "Mon", true
	case time.Tuesday:
		return "Tue", true
	case time.Wednesday:
		return "Wed", true
	case time.Thursday:
		return "Thu", true
	case time.Friday:
		return "Fri", true
	default:
		return "", false
	}
}

func shiftDurationHours(shiftCode string) float64 {
	index := shiftIndexFromCode(shiftCode)
	if index < 1 || index > len(config.TimeSlots) {
		return 0
	}

	return timeSlotDurationHours(config.TimeSlots[index-1])
}

func shiftIndexFromCode(shiftCode string) int {
	parts := strings.Split(shiftCode, "-")
	if len(parts) != 2 {
		return 0
	}

	index, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return index
}

func shiftTimeRange(shiftCode string) (string, string, bool) {
	index := shiftIndexFromCode(shiftCode)
	if index < 1 || index > len(config.TimeSlots) {
		return "", "", false
	}

	parts := strings.Split(config.TimeSlots[index-1], "-")
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func timeSlotDurationHours(timeSlot string) float64 {
	parts := strings.Split(timeSlot, "-")
	if len(parts) != 2 {
		return 0
	}

	start, err := time.Parse("15:04", strings.TrimSpace(parts[0]))
	if err != nil {
		return 0
	}
	end, err := time.Parse("15:04", strings.TrimSpace(parts[1]))
	if err != nil {
		return 0
	}

	duration := end.Sub(start).Hours()
	if duration < 0 {
		duration += 24
	}
	return duration
}

func stringSliceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func isAllowedMonth(month string) bool {
	selected, err := time.Parse("2006-01", month)
	if err != nil {
		return false
	}

	start, _ := time.Parse("2006-01", allowedMonthStart)
	end, _ := time.Parse("2006-01", allowedMonthEnd)
	selected = time.Date(selected.Year(), selected.Month(), 1, 0, 0, 0, 0, time.UTC)
	start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)

	return !selected.Before(start) && !selected.After(end)
}

func parseAllowedDateRange(startDate, endDate string) (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01-02", strings.TrimSpace(startDate))
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidDateRange
	}

	end, err := time.Parse("2006-01-02", strings.TrimSpace(endDate))
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidDateRange
	}

	if start.After(end) {
		return time.Time{}, time.Time{}, ErrInvalidDateRange
	}

	return start, end, nil
}

func isFutureMonth(month string, now time.Time) bool {
	selected, err := time.Parse("2006-01", month)
	if err != nil {
		return false
	}

	selected = time.Date(selected.Year(), selected.Month(), 1, 0, 0, 0, 0, time.UTC)
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return selected.After(current)
}

func calculateManagementAmount(month, role string, now time.Time) (float64, bool) {
	switch role {
	case "LEADER", "HR":
		if isFutureMonth(month, now) {
			return 0, true
		}
		return 800, false
	case "OWNER":
		if isFutureMonth(month, now) {
			return 0, true
		}
		return 1200, false
	default:
		return 0, false
	}
}

func calculateManagementAmountForMonths(months []string, role string, now time.Time) float64 {
	total := 0.0
	for _, month := range months {
		amount, pending := calculateManagementAmount(month, role, now)
		if pending {
			continue
		}
		total += amount
	}
	return total
}

func calculateManagementAmountForDateRange(start, end time.Time, role string, now time.Time) (float64, bool) {
	total := 0.0
	pending := false
	for _, month := range monthsInDateRange(start, end) {
		amount, monthPending := calculateManagementAmount(month, role, now)
		if monthPending {
			pending = true
			continue
		}
		total += amount
	}
	return total, pending
}

func calculateManagementAmountForMonthCount(role string, months int) float64 {
	if months <= 0 {
		return 0
	}

	switch role {
	case "LEADER", "HR":
		return float64(months) * 800
	case "OWNER":
		return float64(months) * 1200
	default:
		return 0
	}
}

func monthsInDateRange(start, end time.Time) []string {
	months := []string{}
	for current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); !current.After(end); current = current.AddDate(0, 1, 0) {
		months = append(months, current.Format("2006-01"))
	}
	return months
}

func summarizeWorkOrdersByUser(workOrders []types.WorkOrder) (map[string]float64, map[string]float64) {
	stats := summarizeWorkOrders(workOrders)
	return stats.userHours, stats.userAmounts
}

func summarizeWorkOrders(workOrders []types.WorkOrder) workOrderAggregation {
	stats := workOrderAggregation{
		perOrderUsers: map[string]map[string]float64{},
		userHours:     map[string]float64{},
		userAmounts:   map[string]float64{},
		orderTotals:   map[string]float64{},
		detailsByUser: map[string][]types.FinanceWorkOrderDetail{},
	}

	for _, workOrder := range workOrders {
		perUser := map[string]float64{}
		datesByUser := map[string][]string{}

		for _, session := range workOrder.WorkSessions {
			perUser[session.WorkerName] += session.Duration
			datesByUser[session.WorkerName] = append(datesByUser[session.WorkerName], session.Date)
			stats.userHours[session.WorkerName] += session.Duration
			stats.orderTotals[workOrder.ID] += session.Duration
		}

		stats.perOrderUsers[workOrder.ID] = perUser

		for workerName, hours := range perUser {
			if hours <= 0 {
				continue
			}

			amount := hours * workOrderRate
			stats.userAmounts[workerName] += amount
			stats.detailsByUser[workerName] = append(stats.detailsByUser[workerName], types.FinanceWorkOrderDetail{
				Title:  workOrder.Title,
				Dates:  strings.Join(datesByUser[workerName], ", "),
				Hours:  hours,
				Amount: amount,
			})
		}
	}

	return stats
}

func filterWorkOrdersByDateRangeExportMonths(workOrders []types.WorkOrder, start, end time.Time) []types.WorkOrder {
	allowedMonths := allowedWorkOrderMonthsForDateRange(start, end)
	filtered := make([]types.WorkOrder, 0, len(workOrders))
	for _, workOrder := range workOrders {
		if _, ok := allowedMonths[workOrder.BelongingMonth]; ok {
			filtered = append(filtered, workOrder)
		}
	}
	return filtered
}

func filterWorkOrdersByMemberNames(workOrders []types.WorkOrder, names []string) []types.WorkOrder {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range uniqueStrings(names) {
		allowed[name] = struct{}{}
	}

	filtered := make([]types.WorkOrder, 0, len(workOrders))
	for _, workOrder := range workOrders {
		next := workOrder
		next.WorkSessions = make([]types.WorkSession, 0, len(workOrder.WorkSessions))
		for _, session := range workOrder.WorkSessions {
			if _, ok := allowed[strings.TrimSpace(session.WorkerName)]; ok {
				next.WorkSessions = append(next.WorkSessions, session)
			}
		}
		filtered = append(filtered, next)
	}
	return filtered
}

func allowedWorkOrderMonthsForDateRange(start, end time.Time) map[string]struct{} {
	allowedMonths := map[string]struct{}{}
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	for current := first; !current.After(last); current = current.AddDate(0, 1, 0) {
		allowedMonths[current.Format("2006-01")] = struct{}{}
	}
	return allowedMonths
}

func includedWorkOrderTitles(workOrders []types.WorkOrder) []string {
	titles := make([]string, 0, len(workOrders))
	for _, workOrder := range workOrders {
		title := strings.TrimSpace(workOrder.Title)
		if title == "" {
			title = workOrder.ID
		}
		titles = append(titles, title)
	}
	return titles
}

func (s *Store) ExportScheduleWorkbook() ([]byte, error) {
	schedule, err := s.GetSchedule()
	if err != nil {
		return nil, err
	}

	file := excelize.NewFile()
	defer file.Close()

	sheets := []struct {
		Name     string
		Resolver func(string) string
	}{
		{
			Name: "总览",
			Resolver: func(shiftCode string) string {
				users := schedule[shiftCode]
				if len(users) == 0 {
					return "-"
				}
				return strings.Join(users, ", ")
			},
		},
		{
			Name: "单周",
			Resolver: func(shiftCode string) string {
				names := make([]string, 0)
				for _, label := range schedule[shiftCode] {
					if strings.HasSuffix(label, "(单)") || strings.HasSuffix(label, "(单双)") {
						names = append(names, baseName(label))
					}
				}
				if len(names) == 0 {
					return "-"
				}
				return strings.Join(names, ", ")
			},
		},
		{
			Name: "双周",
			Resolver: func(shiftCode string) string {
				names := make([]string, 0)
				for _, label := range schedule[shiftCode] {
					if strings.HasSuffix(label, "(双)") || strings.HasSuffix(label, "(单双)") {
						names = append(names, baseName(label))
					}
				}
				if len(names) == 0 {
					return "-"
				}
				return strings.Join(names, ", ")
			},
		},
	}

	file.SetSheetName("Sheet1", sheets[0].Name)
	for index, sheet := range sheets {
		if index > 0 {
			file.NewSheet(sheet.Name)
		}

		headers := append([]string{"时间段"}, config.WeekdaysDisplay...)
		for colIndex, header := range headers {
			cell, _ := excelize.CoordinatesToCellName(colIndex+1, 1)
			file.SetCellValue(sheet.Name, cell, header)
		}

		for shiftIndex, timeSlot := range config.TimeSlots {
			row := shiftIndex + 2
			cell, _ := excelize.CoordinatesToCellName(1, row)
			file.SetCellValue(sheet.Name, cell, timeSlot)

			for dayIndex, dayCode := range config.WeekdaysCode {
				shiftCode := fmt.Sprintf("%s-%d", dayCode, shiftIndex+1)
				value := sheet.Resolver(shiftCode)
				targetCell, _ := excelize.CoordinatesToCellName(dayIndex+2, row)
				file.SetCellValue(sheet.Name, targetCell, value)
			}
		}
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Store) ExportWorkOrdersWorkbook(month string) ([]byte, error) {
	workOrders, err := s.ListWorkOrders(month)
	if err != nil {
		return nil, err
	}
	memberNames, err := s.currentSemesterMemberNames()
	if err != nil {
		return nil, err
	}
	workOrders = filterWorkOrdersByMemberNames(workOrders, memberNames)

	file := excelize.NewFile()
	defer file.Close()

	sheetName := month
	if sheetName == "" {
		sheetName = "工单统计"
	}
	file.SetSheetName("Sheet1", sheetName)

	headers := []string{"姓名"}
	for _, workOrder := range workOrders {
		headers = append(headers, workOrder.Title)
	}
	headers = append(headers, "总时长", "总金额")

	for colIndex, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIndex+1, 1)
		file.SetCellValue(sheetName, cell, header)
	}

	stats := summarizeWorkOrders(workOrders)

	for userIndex, realName := range memberNames {
		row := userIndex + 2
		nameCell, _ := excelize.CoordinatesToCellName(1, row)
		file.SetCellValue(sheetName, nameCell, realName)

		for orderIndex := range workOrders {
			value := stats.perOrderUsers[workOrders[orderIndex].ID][realName]
			if value <= 0 {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(orderIndex+2, row)
			file.SetCellValue(sheetName, cell, value)
		}

		totalHours := stats.userHours[realName]
		hoursCell, _ := excelize.CoordinatesToCellName(len(workOrders)+2, row)
		amountCell, _ := excelize.CoordinatesToCellName(len(workOrders)+3, row)
		file.SetCellValue(sheetName, hoursCell, totalHours)
		file.SetCellValue(sheetName, amountCell, stats.userAmounts[realName])
	}

	summaryRow := len(memberNames) + 2
	totalHours := 0.0
	labelCell, _ := excelize.CoordinatesToCellName(1, summaryRow)
	file.SetCellValue(sheetName, labelCell, "总计")
	for orderIndex, workOrder := range workOrders {
		orderTotal := stats.orderTotals[workOrder.ID]
		cell, _ := excelize.CoordinatesToCellName(orderIndex+2, summaryRow)
		file.SetCellValue(sheetName, cell, orderTotal)
		totalHours += orderTotal
	}
	hoursCell, _ := excelize.CoordinatesToCellName(len(workOrders)+2, summaryRow)
	amountCell, _ := excelize.CoordinatesToCellName(len(workOrders)+3, summaryRow)
	file.SetCellValue(sheetName, hoursCell, totalHours)
	file.SetCellValue(sheetName, amountCell, totalHours*workOrderRate)

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Store) ExportFinanceWorkbook(month string) ([]byte, error) {
	if strings.TrimSpace(month) == "" {
		month = time.Now().Format("2006-01")
	}
	if !isAllowedMonth(month) {
		return nil, ErrMonthOutOfRange
	}

	targetUsers, err := s.financeSummaryUsers()
	if err != nil {
		return nil, err
	}

	type financeUserRow struct {
		Name    string
		Summary types.FinanceSummaryResponse
	}

	targetNames := userRealNames(targetUsers)

	workOrders, err := s.ListWorkOrders(month)
	if err != nil {
		return nil, err
	}
	workOrders = filterWorkOrdersByMemberNames(workOrders, targetNames)

	workOrderHoursByUser, workOrderAmountByUser := summarizeWorkOrdersByUser(workOrders)
	dutyHoursByUser, err := s.getMonthlyDutyHoursForUsers(month, targetNames)
	if err != nil {
		return nil, err
	}

	rows := make([]financeUserRow, 0, len(targetUsers))
	now := time.Now()
	for _, user := range targetUsers {
		dutyHours := dutyHoursByUser[user.RealName]
		dutyAmount := dutyHours * dutyHourlyRate
		workOrderHours := workOrderHoursByUser[user.RealName]
		workOrderAmount := workOrderAmountByUser[user.RealName]
		managementAmount, managementPending := calculateManagementAmount(month, user.Role, now)
		rows = append(rows, financeUserRow{
			Name: user.RealName,
			Summary: types.FinanceSummaryResponse{
				Month:             month,
				DutyHours:         dutyHours,
				DutyAmount:        dutyAmount,
				WorkOrderHours:    workOrderHours,
				WorkOrderAmount:   workOrderAmount,
				ManagementAmount:  managementAmount,
				ManagementPending: managementPending,
				TotalAmount:       dutyAmount + workOrderAmount + managementAmount,
			},
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return config.LessRealName(rows[i].Name, rows[j].Name)
	})

	file := excelize.NewFile()
	defer file.Close()

	sheetName := month
	file.SetSheetName("Sheet1", sheetName)

	headers := []string{"姓名", "值班时长", "值班酬劳", "工单时长", "工单酬劳", "项目管理薪酬", "总酬劳"}
	for colIndex, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIndex+1, 1)
		file.SetCellValue(sheetName, cell, header)
	}

	dutyHoursTotal := 0.0
	dutyAmountTotal := 0.0
	workOrderHoursTotal := 0.0
	workOrderAmountTotal := 0.0
	managementAmountTotal := 0.0
	totalAmountTotal := 0.0

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2

		nameCell, _ := excelize.CoordinatesToCellName(1, rowNumber)
		dutyHoursCell, _ := excelize.CoordinatesToCellName(2, rowNumber)
		dutyAmountCell, _ := excelize.CoordinatesToCellName(3, rowNumber)
		workOrderHoursCell, _ := excelize.CoordinatesToCellName(4, rowNumber)
		workOrderAmountCell, _ := excelize.CoordinatesToCellName(5, rowNumber)
		managementCell, _ := excelize.CoordinatesToCellName(6, rowNumber)
		totalAmountCell, _ := excelize.CoordinatesToCellName(7, rowNumber)

		file.SetCellValue(sheetName, nameCell, row.Name)
		file.SetCellValue(sheetName, dutyHoursCell, row.Summary.DutyHours)
		file.SetCellValue(sheetName, dutyAmountCell, row.Summary.DutyAmount)
		file.SetCellValue(sheetName, workOrderHoursCell, row.Summary.WorkOrderHours)
		file.SetCellValue(sheetName, workOrderAmountCell, row.Summary.WorkOrderAmount)
		if row.Summary.ManagementPending {
			file.SetCellValue(sheetName, managementCell, "未计算")
		} else {
			file.SetCellValue(sheetName, managementCell, row.Summary.ManagementAmount)
		}
		file.SetCellValue(sheetName, totalAmountCell, row.Summary.TotalAmount)

		dutyHoursTotal += row.Summary.DutyHours
		dutyAmountTotal += row.Summary.DutyAmount
		workOrderHoursTotal += row.Summary.WorkOrderHours
		workOrderAmountTotal += row.Summary.WorkOrderAmount
		managementAmountTotal += row.Summary.ManagementAmount
		totalAmountTotal += row.Summary.TotalAmount
	}

	summaryRow := len(rows) + 2
	summaryLabelCell, _ := excelize.CoordinatesToCellName(1, summaryRow)
	dutyHoursTotalCell, _ := excelize.CoordinatesToCellName(2, summaryRow)
	dutyAmountTotalCell, _ := excelize.CoordinatesToCellName(3, summaryRow)
	workOrderHoursTotalCell, _ := excelize.CoordinatesToCellName(4, summaryRow)
	workOrderAmountTotalCell, _ := excelize.CoordinatesToCellName(5, summaryRow)
	managementAmountTotalCell, _ := excelize.CoordinatesToCellName(6, summaryRow)
	totalAmountTotalCell, _ := excelize.CoordinatesToCellName(7, summaryRow)

	file.SetCellValue(sheetName, summaryLabelCell, "合计")
	file.SetCellValue(sheetName, dutyHoursTotalCell, dutyHoursTotal)
	file.SetCellValue(sheetName, dutyAmountTotalCell, dutyAmountTotal)
	file.SetCellValue(sheetName, workOrderHoursTotalCell, workOrderHoursTotal)
	file.SetCellValue(sheetName, workOrderAmountTotalCell, workOrderAmountTotal)
	file.SetCellValue(sheetName, managementAmountTotalCell, managementAmountTotal)
	file.SetCellValue(sheetName, totalAmountTotalCell, totalAmountTotal)

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Store) ExportFinanceWorkbookForRange(startDate, endDate string, workOrderIDs []string, includeManagement bool, managementMonths int) ([]byte, error) {
	start, end, err := parseAllowedDateRange(startDate, endDate)
	if err != nil {
		return nil, err
	}

	targetUsers, err := s.financeSummaryUsers()
	if err != nil {
		return nil, err
	}

	type financeUserRow struct {
		Name              string
		DutyHours         float64
		DutyAmount        float64
		WorkOrderHours    float64
		WorkOrderAmount   float64
		ManagementAmount  float64
		ManagementPending bool
		TotalAmount       float64
	}

	targetNames := userRealNames(targetUsers)

	workOrders, err := s.ListWorkOrdersByIDs(workOrderIDs)
	if err != nil {
		return nil, err
	}
	workOrders = filterWorkOrdersByDateRangeExportMonths(workOrders, start, end)
	workOrders = filterWorkOrdersByMemberNames(workOrders, targetNames)

	workOrderHoursByUser, workOrderAmountByUser := summarizeWorkOrdersByUser(workOrders)
	dutyHoursByUser, err := s.getDutyHoursForUsersInDateRange(start, end, targetNames)
	if err != nil {
		return nil, err
	}

	rows := make([]financeUserRow, 0, len(targetUsers))
	for _, user := range targetUsers {
		dutyHours := dutyHoursByUser[user.RealName]
		dutyAmount := dutyHours * dutyHourlyRate
		workOrderHours := workOrderHoursByUser[user.RealName]
		workOrderAmount := workOrderAmountByUser[user.RealName]
		managementAmount := 0.0
		if includeManagement {
			managementAmount = calculateManagementAmountForMonthCount(user.Role, managementMonths)
		}
		rows = append(rows, financeUserRow{
			Name:             user.RealName,
			DutyHours:        dutyHours,
			DutyAmount:       dutyAmount,
			WorkOrderHours:   workOrderHours,
			WorkOrderAmount:  workOrderAmount,
			ManagementAmount: managementAmount,
			TotalAmount:      dutyAmount + workOrderAmount + managementAmount,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return config.LessRealName(rows[i].Name, rows[j].Name)
	})

	file := excelize.NewFile()
	defer file.Close()

	sheetName := "财务统计"
	file.SetSheetName("Sheet1", sheetName)

	headers := []string{"姓名", "值班时长", "值班酬劳", "工单时长", "工单酬劳", "项目管理薪酬", "总酬劳"}
	for colIndex, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIndex+1, 1)
		file.SetCellValue(sheetName, cell, header)
	}

	dutyHoursTotal := 0.0
	dutyAmountTotal := 0.0
	workOrderHoursTotal := 0.0
	workOrderAmountTotal := 0.0
	managementAmountTotal := 0.0
	totalAmountTotal := 0.0

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2

		values := []any{
			row.Name,
			row.DutyHours,
			row.DutyAmount,
			row.WorkOrderHours,
			row.WorkOrderAmount,
			row.ManagementAmount,
			row.TotalAmount,
		}
		for colIndex, value := range values {
			cell, _ := excelize.CoordinatesToCellName(colIndex+1, rowNumber)
			file.SetCellValue(sheetName, cell, value)
		}

		dutyHoursTotal += row.DutyHours
		dutyAmountTotal += row.DutyAmount
		workOrderHoursTotal += row.WorkOrderHours
		workOrderAmountTotal += row.WorkOrderAmount
		managementAmountTotal += row.ManagementAmount
		totalAmountTotal += row.TotalAmount
	}

	summaryRow := len(rows) + 2
	summaryValues := []any{
		"合计",
		dutyHoursTotal,
		dutyAmountTotal,
		workOrderHoursTotal,
		workOrderAmountTotal,
		managementAmountTotal,
		totalAmountTotal,
	}
	for colIndex, value := range summaryValues {
		cell, _ := excelize.CoordinatesToCellName(colIndex+1, summaryRow)
		file.SetCellValue(sheetName, cell, value)
	}

	metaRow := summaryRow + 2
	file.SetCellValue(sheetName, "A"+strconv.Itoa(metaRow), "统计范围")
	file.SetCellValue(sheetName, "B"+strconv.Itoa(metaRow), fmt.Sprintf("%s 至 %s", startDate, endDate))
	file.SetCellValue(sheetName, "A"+strconv.Itoa(metaRow+1), "工单数")
	file.SetCellValue(sheetName, "B"+strconv.Itoa(metaRow+1), len(workOrders))
	file.SetCellValue(sheetName, "A"+strconv.Itoa(metaRow+2), "包含工单")

	includedOrders := includedWorkOrderTitles(workOrders)
	if len(includedOrders) == 0 {
		file.SetCellValue(sheetName, "B"+strconv.Itoa(metaRow+2), "无")
	} else {
		for index, title := range includedOrders {
			cell, _ := excelize.CoordinatesToCellName(index+2, metaRow+2)
			file.SetCellValue(sheetName, cell, title)
		}
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Store) ExportDutyCSVForRange(startDate, endDate, outputMonth string, workOrderIDs []string, includeManagement bool, managementMonths int) ([]byte, error) {
	start, end, err := parseAllowedDateRange(startDate, endDate)
	if err != nil {
		return nil, err
	}

	outputMonthStart, err := parseCSVOutputMonth(outputMonth, start)
	if err != nil {
		return nil, err
	}
	users, err := s.financeSummaryUsers()
	if err != nil {
		return nil, err
	}
	targetNames := userRealNames(users)

	dutyEntries, err := s.getDutyCSVEntriesInDateRange(start, end)
	if err != nil {
		return nil, err
	}

	workOrders, err := s.ListWorkOrdersByIDs(workOrderIDs)
	if err != nil {
		return nil, err
	}
	workOrders = filterWorkOrdersByDateRangeExportMonths(workOrders, start, end)
	workOrders = filterWorkOrdersByMemberNames(workOrders, targetNames)

	managementPeople := []csvManagementPerson{}
	if includeManagement && managementMonths > 0 {
		for _, user := range users {
			if calculateManagementAmountForMonthCount(user.Role, managementMonths) <= 0 {
				continue
			}
			managementPeople = append(managementPeople, csvManagementPerson{Name: user.RealName, Role: user.Role})
		}
	}

	entries, err := buildFinanceCSVEntries(outputMonthStart, dutyEntries, workOrders, managementPeople, managementMonths)
	if err != nil {
		return nil, err
	}

	return writeDutyCSVEntries(entries)
}

func (s *Store) SaveFinanceExportsLocal(request types.FinanceSaveLocalRequest) (types.FinanceSaveLocalResponse, error) {
	workOrderIDs := cleanedStringSlice(request.WorkOrderIDs)
	managementMonths := request.ManagementMonths
	if !request.IncludeManagement || managementMonths < 0 {
		managementMonths = 0
	}

	excelContent, err := s.ExportFinanceWorkbookForRange(request.StartDate, request.EndDate, workOrderIDs, request.IncludeManagement, managementMonths)
	if err != nil {
		return types.FinanceSaveLocalResponse{}, err
	}
	csvContent, err := s.ExportDutyCSVForRange(request.StartDate, request.EndDate, request.OutputMonth, workOrderIDs, request.IncludeManagement, managementMonths)
	if err != nil {
		return types.FinanceSaveLocalResponse{}, err
	}

	batchID, err := newFinanceBatchID()
	if err != nil {
		return types.FinanceSaveLocalResponse{}, err
	}
	outputMonth := strings.TrimSpace(request.OutputMonth)
	if outputMonth == "" {
		outputMonth = request.StartDate
		if len(outputMonth) >= len("2006-01") {
			outputMonth = outputMonth[:len("2006-01")]
		}
	}
	excelName := fmt.Sprintf("%s-%s-财务统计.xlsx", compactStoreDate(request.StartDate), compactStoreDate(request.EndDate))
	csvName := fmt.Sprintf("%s-%s-%s-duty_by_person.csv", compactStoreDate(request.StartDate), compactStoreDate(request.EndDate), strings.ReplaceAll(outputMonth, "-", ""))

	batch := types.FinanceLocalBatch{
		ID:                batchID,
		CreatedAt:         time.Now().Format("2006-01-02 15:04:05"),
		StartDate:         request.StartDate,
		EndDate:           request.EndDate,
		OutputMonth:       outputMonth,
		WorkOrderIDs:      workOrderIDs,
		IncludeManagement: request.IncludeManagement,
		ManagementMonths:  managementMonths,
		ExcelFilename:     excelName,
		CSVFilename:       csvName,
		RelativeDir:       "database:" + batchID,
	}
	if err := s.insertFinanceBatch(batch, excelContent, csvContent); err != nil {
		return types.FinanceSaveLocalResponse{}, err
	}

	return types.FinanceSaveLocalResponse{Message: "财务 Excel 和 CSV 已保存", Batch: batch}, nil
}

func (s *Store) ListFinanceLocalBatches() ([]types.FinanceLocalBatch, error) {
	rows, err := s.db.Query(`
		SELECT id, created_at, start_date, end_date, output_month, work_order_ids_json,
		       include_management, management_months, excel_filename, csv_filename
		FROM finance_batches ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batches := make([]types.FinanceLocalBatch, 0)
	for rows.Next() {
		var batch types.FinanceLocalBatch
		var workOrderIDs string
		var includeManagement int
		if err := rows.Scan(&batch.ID, &batch.CreatedAt, &batch.StartDate, &batch.EndDate, &batch.OutputMonth, &workOrderIDs, &includeManagement, &batch.ManagementMonths, &batch.ExcelFilename, &batch.CSVFilename); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(workOrderIDs), &batch.WorkOrderIDs)
		batch.IncludeManagement = includeManagement == 1
		batch.RelativeDir = "database:" + batch.ID
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s *Store) GetFinanceLocalBatchWorkbook(batchID string) (types.FinanceLocalBatch, []byte, error) {
	batch, err := s.readFinanceLocalBatch(batchID)
	if err != nil {
		return types.FinanceLocalBatch{}, nil, err
	}
	var content []byte
	err = s.db.QueryRow(`SELECT excel_blob FROM finance_batches WHERE id = ?`, batchID).Scan(&content)
	if err != nil {
		return types.FinanceLocalBatch{}, nil, err
	}
	return batch, content, nil
}

func (s *Store) readFinanceLocalBatch(batchID string) (types.FinanceLocalBatch, error) {
	if !isSafeLocalID(batchID) {
		return types.FinanceLocalBatch{}, os.ErrNotExist
	}
	var batch types.FinanceLocalBatch
	var workOrderIDs string
	var includeManagement int
	err := s.db.QueryRow(`
		SELECT id, created_at, start_date, end_date, output_month, work_order_ids_json,
		       include_management, management_months, excel_filename, csv_filename
		FROM finance_batches WHERE id = ?
	`, batchID).Scan(&batch.ID, &batch.CreatedAt, &batch.StartDate, &batch.EndDate, &batch.OutputMonth, &workOrderIDs, &includeManagement, &batch.ManagementMonths, &batch.ExcelFilename, &batch.CSVFilename)
	if err != nil {
		return types.FinanceLocalBatch{}, err
	}
	_ = json.Unmarshal([]byte(workOrderIDs), &batch.WorkOrderIDs)
	batch.IncludeManagement = includeManagement == 1
	batch.RelativeDir = "database:" + batch.ID
	return batch, nil
}

func (s *Store) financeRootDir() (string, error) {
	databasePath, err := filepath.Abs(s.cfg.DatabasePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(databasePath), "finance"), nil
}

func newFinanceBatchID() (string, error) {
	runID, err := newLaborRunID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", time.Now().Format("20060102T150405"), strings.Split(runID, "-")[0]), nil
}

func cleanedStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func compactStoreDate(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "-", "")
}

func isSafeLocalID(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func writeDutyCSVEntries(entries []dutyCSVEntry) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)

	if err := writer.Write([]string{"姓名", "年", "月", "日", "起", "讫", "时数"}); err != nil {
		return nil, err
	}

	currentName := ""
	totalHours := 0.0
	writeTotal := func() error {
		if currentName == "" {
			return nil
		}
		return writer.Write([]string{
			currentName,
			"合计",
			"",
			"",
			"",
			"",
			fmt.Sprintf("%.1f", math.Round(totalHours*10)/10),
		})
	}

	for _, entry := range entries {
		if entry.Name != currentName {
			if err := writeTotal(); err != nil {
				return nil, err
			}
			currentName = entry.Name
			totalHours = 0
		}

		if err := writer.Write([]string{
			entry.Name,
			strconv.Itoa(entry.Date.Year()),
			strconv.Itoa(int(entry.Date.Month())),
			strconv.Itoa(entry.Date.Day()),
			entry.StartTime,
			entry.EndTime,
			fmt.Sprintf("%.1f", math.Round(entry.Hours*10)/10),
		}); err != nil {
			return nil, err
		}
		totalHours += entry.Hours
	}

	if err := writeTotal(); err != nil {
		return nil, err
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func buildFinanceCSVEntries(outputMonthStart time.Time, dutyEntries []dutyCSVEntry, workOrders []types.WorkOrder, managementPeople []csvManagementPerson, managementMonths int) ([]dutyCSVEntry, error) {
	allocator := newCSVScheduleAllocator()
	entries := make([]dutyCSVEntry, 0, len(dutyEntries))

	for _, entry := range dutyEntries {
		mappedDate := mapDateToOutputMonth(entry.Date, outputMonthStart)
		startMinute, endMinute, ok := parseCSVTimeRange(entry.StartTime, entry.EndTime)
		if ok && !allocator.hasOverlap(entry.Name, mappedDate, csvTimeBlock{Start: startMinute, End: endMinute}) {
			next := entry
			next.Date = mappedDate
			next.ShiftIndex = 0
			allocator.occupy(next.Name, next.Date, csvTimeBlock{Start: startMinute, End: endMinute})
			entries = append(entries, next)
			continue
		}

		allocated, err := allocator.allocate(entry.Name, mappedDate, hoursToMinutes(entry.Hours), dateOrderFrom(outputMonthStart, mappedDate), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
		if err != nil {
			return nil, err
		}
		entries = append(entries, allocated...)
	}

	for _, workOrder := range workOrders {
		for _, session := range workOrder.WorkSessions {
			name := strings.TrimSpace(session.WorkerName)
			if name == "" || session.Duration <= 0 {
				continue
			}
			sessionDate, err := time.Parse("2006-01-02", strings.TrimSpace(session.Date))
			if err != nil {
				return nil, ErrInvalidDateRange
			}
			mappedDate := mapDateToOutputMonth(sessionDate, outputMonthStart)
			allocated, err := allocator.allocate(name, mappedDate, hoursToMinutes(session.Duration*2), dateOrderFrom(outputMonthStart, mappedDate), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
			if err != nil {
				return nil, err
			}
			entries = append(entries, allocated...)
		}
	}

	if managementMonths > 0 {
		for _, person := range managementPeople {
			amount := calculateManagementAmountForMonthCount(person.Role, managementMonths)
			if amount <= 0 {
				continue
			}
			allocated, err := allocator.allocate(person.Name, firstSaturday(outputMonthStart), hoursToMinutes(amount/dutyHourlyRate), managementDateOrder(outputMonthStart), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
			if err != nil {
				return nil, err
			}
			entries = append(entries, allocated...)
		}
	}

	sortDutyCSVEntries(entries)
	return entries, nil
}

func parseCSVOutputMonth(value string, fallback time.Time) (time.Time, error) {
	month := strings.TrimSpace(value)
	if month == "" {
		month = fallback.Format("2006-01")
	}
	outputMonthStart, err := time.Parse("2006-01", month)
	if err != nil {
		return time.Time{}, ErrInvalidDateRange
	}
	return time.Date(outputMonthStart.Year(), outputMonthStart.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

func newCSVScheduleAllocator() *csvScheduleAllocator {
	return &csvScheduleAllocator{occupied: map[string]map[string][]csvTimeBlock{}}
}

func (a *csvScheduleAllocator) allocate(name string, preferredDate time.Time, minutes int, dateOrder []time.Time, primaryBlocks []csvTimeBlock, fallbackBlocks []csvTimeBlock) ([]dutyCSVEntry, error) {
	if minutes <= 0 {
		return nil, nil
	}

	entries, remaining := a.allocateFromBlocks(name, minutes, dateOrder, primaryBlocks)
	if remaining <= 0 {
		return entries, nil
	}

	fallbackEntries, remaining := a.allocateFromBlocks(name, remaining, dateOrder, fallbackBlocks)
	entries = append(entries, fallbackEntries...)
	if remaining > 0 {
		return nil, fmt.Errorf("CSV 时间段空间不足：%s 在 %s 起仍剩余 %.1f 小时无法排入", name, preferredDate.Format("2006-01-02"), float64(remaining)/60)
	}
	return entries, nil
}

func (a *csvScheduleAllocator) allocateFromBlocks(name string, minutes int, dateOrder []time.Time, blocks []csvTimeBlock) ([]dutyCSVEntry, int) {
	entries := []dutyCSVEntry{}
	remaining := minutes
	for _, date := range dateOrder {
		if remaining <= 0 {
			break
		}
		for _, block := range blocks {
			if remaining <= 0 {
				break
			}
			for _, free := range a.freeSegments(name, date, block) {
				if remaining <= 0 {
					break
				}
				length := free.End - free.Start
				if length <= 0 {
					continue
				}
				chunk := minInt(length, remaining)
				occupied := csvTimeBlock{Start: free.Start, End: free.Start + chunk}
				a.occupy(name, date, occupied)
				entries = append(entries, dutyCSVEntry{
					Name:      name,
					Date:      date,
					StartTime: formatCSVMinute(occupied.Start),
					EndTime:   formatCSVMinute(occupied.End),
					Hours:     float64(chunk) / 60,
				})
				remaining -= chunk
			}
		}
	}
	return entries, remaining
}

func (a *csvScheduleAllocator) hasOverlap(name string, date time.Time, block csvTimeBlock) bool {
	key := date.Format("2006-01-02")
	for _, existing := range a.occupied[name][key] {
		if block.Start < existing.End && existing.Start < block.End {
			return true
		}
	}
	return false
}

func (a *csvScheduleAllocator) occupy(name string, date time.Time, block csvTimeBlock) {
	if block.End <= block.Start {
		return
	}
	if _, ok := a.occupied[name]; !ok {
		a.occupied[name] = map[string][]csvTimeBlock{}
	}
	key := date.Format("2006-01-02")
	a.occupied[name][key] = append(a.occupied[name][key], block)
	sort.Slice(a.occupied[name][key], func(i, j int) bool {
		return a.occupied[name][key][i].Start < a.occupied[name][key][j].Start
	})
}

func (a *csvScheduleAllocator) freeSegments(name string, date time.Time, block csvTimeBlock) []csvTimeBlock {
	if block.End <= block.Start {
		return nil
	}
	segments := []csvTimeBlock{}
	cursor := block.Start
	key := date.Format("2006-01-02")
	for _, occupied := range a.occupied[name][key] {
		if occupied.End <= block.Start || occupied.Start >= block.End {
			continue
		}
		if occupied.Start > cursor {
			segments = append(segments, csvTimeBlock{Start: cursor, End: minInt(occupied.Start, block.End)})
		}
		if occupied.End > cursor {
			cursor = occupied.End
		}
	}
	if cursor < block.End {
		segments = append(segments, csvTimeBlock{Start: cursor, End: block.End})
	}
	return segments
}

func parseCSVTimeRange(startText string, endText string) (int, int, bool) {
	start, ok := parseCSVMinute(startText)
	if !ok {
		return 0, 0, false
	}
	end, ok := parseCSVMinute(endText)
	if !ok || end <= start {
		return 0, 0, false
	}
	return start, end, true
}

func parseCSVMinute(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	if hour < 0 || hour > 24 || minute < 0 || minute >= 60 || (hour == 24 && minute != 0) {
		return 0, false
	}
	return hour*60 + minute, true
}

func formatCSVMinute(value int) string {
	if value < 0 {
		value = 0
	}
	if value > 1440 {
		value = 1440
	}
	return fmt.Sprintf("%d:%02d", value/60, value%60)
}

func hoursToMinutes(hours float64) int {
	return int(math.Round(hours * 60))
}

func csvNormalWorkBlocks() []csvTimeBlock {
	return []csvTimeBlock{{Start: 8 * 60, End: 12 * 60}, {Start: 14 * 60, End: 18 * 60}}
}

func csvExtendedWorkBlocks() []csvTimeBlock {
	return []csvTimeBlock{{Start: 18 * 60, End: 24 * 60}, {Start: 0, End: 8 * 60}, {Start: 12 * 60, End: 14 * 60}}
}

func mapDateToOutputMonth(date time.Time, outputMonthStart time.Time) time.Time {
	day := minInt(date.Day(), daysInMonth(outputMonthStart))
	return time.Date(outputMonthStart.Year(), outputMonthStart.Month(), day, 0, 0, 0, 0, time.UTC)
}

func daysInMonth(monthStart time.Time) int {
	return time.Date(monthStart.Year(), monthStart.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func dateOrderFrom(outputMonthStart time.Time, startDate time.Time) []time.Time {
	days := daysInMonth(outputMonthStart)
	startDay := minInt(maxInt(startDate.Day(), 1), days)
	result := make([]time.Time, 0, days)
	for day := startDay; day <= days; day++ {
		result = append(result, time.Date(outputMonthStart.Year(), outputMonthStart.Month(), day, 0, 0, 0, 0, time.UTC))
	}
	for day := 1; day < startDay; day++ {
		result = append(result, time.Date(outputMonthStart.Year(), outputMonthStart.Month(), day, 0, 0, 0, 0, time.UTC))
	}
	return result
}

func firstSaturday(outputMonthStart time.Time) time.Time {
	for day := 1; day <= 7; day++ {
		current := time.Date(outputMonthStart.Year(), outputMonthStart.Month(), day, 0, 0, 0, 0, time.UTC)
		if current.Weekday() == time.Saturday {
			return current
		}
	}
	return outputMonthStart
}

func managementDateOrder(outputMonthStart time.Time) []time.Time {
	days := daysInMonth(outputMonthStart)
	weekends := []time.Time{}
	weekdays := []time.Time{}
	firstSaturdayDay := firstSaturday(outputMonthStart).Day()
	for day := firstSaturdayDay; day <= days; day++ {
		current := time.Date(outputMonthStart.Year(), outputMonthStart.Month(), day, 0, 0, 0, 0, time.UTC)
		if current.Weekday() == time.Saturday || current.Weekday() == time.Sunday {
			weekends = append(weekends, current)
		} else {
			weekdays = append(weekdays, current)
		}
	}
	for day := 1; day < firstSaturdayDay; day++ {
		current := time.Date(outputMonthStart.Year(), outputMonthStart.Month(), day, 0, 0, 0, 0, time.UTC)
		if current.Weekday() == time.Saturday || current.Weekday() == time.Sunday {
			weekends = append(weekends, current)
		} else {
			weekdays = append(weekdays, current)
		}
	}
	return append(weekends, weekdays...)
}

func sortDutyCSVEntries(entries []dutyCSVEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return config.LessRealName(entries[i].Name, entries[j].Name)
		}
		if !entries[i].Date.Equal(entries[j].Date) {
			return entries[i].Date.Before(entries[j].Date)
		}
		leftStart, _, leftOK := parseCSVTimeRange(entries[i].StartTime, entries[i].EndTime)
		rightStart, _, rightOK := parseCSVTimeRange(entries[j].StartTime, entries[j].EndTime)
		if leftOK && rightOK && leftStart != rightStart {
			return leftStart < rightStart
		}
		if entries[i].StartTime != entries[j].StartTime {
			return entries[i].StartTime < entries[j].StartTime
		}
		return entries[i].EndTime < entries[j].EndTime
	})
}

func (s *Store) getFinalScheduleEntries(weekNumber int) (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT entries.shift_code, entries.real_name
		FROM final_schedule_entries AS entries
		JOIN users AS members ON members.id = entries.member_id
		WHERE entries.week_number = ? AND members.is_active = 1 AND members.role != 'ADMIN'
		ORDER BY entries.shift_code ASC, entries.real_name ASC
	`, weekNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedule := map[string][]string{}
	for rows.Next() {
		var shiftCode string
		var realName string
		if err := rows.Scan(&shiftCode, &realName); err != nil {
			return nil, err
		}
		schedule[shiftCode] = append(schedule[shiftCode], realName)
	}
	return schedule, rows.Err()
}

func (s *Store) getPlannedScheduleForWeek(isOddWeek bool) (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT entries.shift_code, entries.real_name, entries.week_type
		FROM schedule_entries AS entries
		JOIN users AS members ON members.id = entries.member_id
		WHERE members.is_active = 1 AND members.role != 'ADMIN'
		ORDER BY entries.shift_code ASC, entries.real_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedule := map[string][]string{}
	for rows.Next() {
		var shiftCode string
		var realName string
		var weekType string
		if err := rows.Scan(&shiftCode, &realName, &weekType); err != nil {
			return nil, err
		}

		if weekType == "both" || (isOddWeek && weekType == "single") || (!isOddWeek && weekType == "double") {
			schedule[shiftCode] = append(schedule[shiftCode], realName)
		}
	}
	return schedule, rows.Err()
}

func (s *Store) getWorkSessions(workOrderID string) ([]types.WorkSession, error) {
	rows, err := s.db.Query(`
		SELECT id, date, worker_name, duration
		FROM work_sessions
		WHERE work_order_id = ?
		ORDER BY date ASC, id ASC
	`, workOrderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]types.WorkSession, 0)
	for rows.Next() {
		var session types.WorkSession
		if err := rows.Scan(&session.ID, &session.Date, &session.WorkerName, &session.Duration); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) persistWorkOrder(workOrder types.WorkOrder) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO work_orders (id, title, belonging_month, created_time, created_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			belonging_month = excluded.belonging_month
	`, workOrder.ID, workOrder.Title, workOrder.BelongingMonth, workOrder.CreatedTime, workOrder.CreatedBy); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM work_sessions WHERE work_order_id = ?`, workOrder.ID); err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO work_sessions (work_order_id, date, worker_name, member_id, duration)
		SELECT ?, ?, real_name, id, ?
		FROM users
		WHERE real_name = ? AND is_active = 1 AND role != 'ADMIN'
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for _, session := range workOrder.WorkSessions {
		result, err := insertStmt.Exec(workOrder.ID, session.Date, session.Duration, session.WorkerName)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("成员 %s 已不属于当前学期", session.WorkerName)
		}
	}

	return tx.Commit()
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func verifyPassword(password, passwordHash string) bool {
	if strings.HasPrefix(passwordHash, "$2a$") || strings.HasPrefix(passwordHash, "$2b$") || strings.HasPrefix(passwordHash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
	}

	legacyHash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(legacyHash[:]) == passwordHash
}

func parseScheduleLabel(label string) (string, string) {
	switch {
	case strings.HasSuffix(label, "(单双)"):
		return strings.TrimSuffix(label, "(单双)"), "both"
	case strings.HasSuffix(label, "(单)"):
		return strings.TrimSuffix(label, "(单)"), "single"
	case strings.HasSuffix(label, "(双)"):
		return strings.TrimSuffix(label, "(双)"), "double"
	default:
		return strings.TrimSpace(label), "both"
	}
}

func baseName(label string) string {
	realName, _ := parseScheduleLabel(label)
	return realName
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sanitizeSessions(sessions []types.WorkSession) []types.WorkSession {
	result := make([]types.WorkSession, 0, len(sessions))
	for _, session := range sessions {
		session.Date = strings.TrimSpace(session.Date)
		session.WorkerName = strings.TrimSpace(session.WorkerName)
		session.Duration = math.Round(session.Duration*100) / 100
		if session.Date == "" || session.WorkerName == "" || session.Duration <= 0 {
			continue
		}
		result = append(result, session)
	}
	return result
}

func buildShiftDistribution(schedule map[string][]string) []types.ChartItem {
	shiftStats := map[string]float64{}

	for _, labels := range schedule {
		for _, label := range labels {
			name := baseName(label)
			if name == "" {
				continue
			}

			switch {
			case strings.HasSuffix(label, "(单双)"):
				shiftStats[name] += 1
			case strings.HasSuffix(label, "(单)"), strings.HasSuffix(label, "(双)"):
				shiftStats[name] += 0.5
			default:
				shiftStats[name] += 1
			}
		}
	}

	return sortedChartItems(shiftStats)
}

func sortedChartItems(source map[string]float64) []types.ChartItem {
	items := make([]types.ChartItem, 0, len(source))
	for name, value := range source {
		items = append(items, types.ChartItem{Name: name, Value: value})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Value == items[j].Value {
			return items[i].Name < items[j].Name
		}
		return items[i].Value > items[j].Value
	})

	return items
}
