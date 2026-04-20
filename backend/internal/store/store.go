package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	allowedMonthStart = "2026-04"
	allowedMonthEnd   = "2050-12"
)

type Store struct {
	db  *sql.DB
	cfg config.AppConfig
}

func New(cfg config.AppConfig) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, statement := range []string{
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA synchronous = NORMAL;`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}

	store := &Store{db: db, cfg: cfg}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	if err := store.seedUsers(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			real_name TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'USER',
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
	}

	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) seedUsers() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	for _, user := range config.DefaultUsers(s.cfg.AdminPassword) {
		passwordHash, err := hashPassword(user.Password)
		if err != nil {
			return err
		}

		mustChange := 0
		if user.MustChangePassword {
			mustChange = 1
		}

		if _, err := s.db.Exec(`
			INSERT INTO users (username, password_hash, real_name, role, is_active, must_change_password)
			VALUES (?, ?, ?, ?, 1, ?)
		`, user.Username, passwordHash, user.RealName, user.Role, mustChange); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) Authenticate(username, password string) (*types.User, error) {
	row := s.db.QueryRow(`
		SELECT id, username, password_hash, real_name, role, is_active, must_change_password, created_at
		FROM users
		WHERE username = ?
	`, username)

	var user types.User
	var passwordHash string
	var isActive int
	var mustChange int

	if err := row.Scan(
		&user.ID,
		&user.Username,
		&passwordHash,
		&user.RealName,
		&user.Role,
		&isActive,
		&mustChange,
		&user.CreatedAt,
	); err != nil {
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

	user.IsActive = isActive == 1
	user.MustChangePassword = mustChange == 1
	user.Permissions = config.PermissionsFor(user.Role)
	return &user, nil
}

func (s *Store) GetUserByID(userID int64) (*types.User, error) {
	row := s.db.QueryRow(`
		SELECT id, username, real_name, role, is_active, must_change_password, created_at
		FROM users
		WHERE id = ?
	`, userID)

	var user types.User
	var isActive int
	var mustChange int

	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.RealName,
		&user.Role,
		&isActive,
		&mustChange,
		&user.CreatedAt,
	); err != nil {
		return nil, err
	}

	user.IsActive = isActive == 1
	user.MustChangePassword = mustChange == 1
	user.Permissions = config.PermissionsFor(user.Role)
	return &user, nil
}

func (s *Store) GetUserByUsername(username string) (*types.User, error) {
	row := s.db.QueryRow(`
		SELECT id, username, real_name, role, is_active, must_change_password, created_at
		FROM users
		WHERE username = ?
	`, username)

	var user types.User
	var isActive int
	var mustChange int

	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.RealName,
		&user.Role,
		&isActive,
		&mustChange,
		&user.CreatedAt,
	); err != nil {
		return nil, err
	}

	user.IsActive = isActive == 1
	user.MustChangePassword = mustChange == 1
	user.Permissions = config.PermissionsFor(user.Role)
	return &user, nil
}

func (s *Store) GetUserByRealName(realName string) (*types.User, error) {
	row := s.db.QueryRow(`
		SELECT id, username, real_name, role, is_active, must_change_password, created_at
		FROM users
		WHERE real_name = ?
	`, realName)

	var user types.User
	var isActive int
	var mustChange int

	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.RealName,
		&user.Role,
		&isActive,
		&mustChange,
		&user.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}

	user.IsActive = isActive == 1
	user.MustChangePassword = mustChange == 1
	user.Permissions = config.PermissionsFor(user.Role)
	return &user, nil
}

func (s *Store) ListUsers() ([]types.User, error) {
	rows, err := s.db.Query(`
		SELECT id, username, real_name, role, is_active, must_change_password, created_at
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
		var isActive int
		var mustChange int
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.RealName,
			&user.Role,
			&isActive,
			&mustChange,
			&user.CreatedAt,
		); err != nil {
			return nil, err
		}

		user.IsActive = isActive == 1
		user.MustChangePassword = mustChange == 1
		user.Permissions = config.PermissionsFor(user.Role)
		users = append(users, user)
	}

	sort.SliceStable(users, func(i, j int) bool {
		if users[i].Role == "ADMIN" && users[j].Role != "ADMIN" {
			return true
		}
		if users[i].Role != "ADMIN" && users[j].Role == "ADMIN" {
			return false
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

	_, err := s.db.Exec(`
		UPDATE users
		SET is_active = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, userID)
	return err
}

func (s *Store) UpdateOwnPassword(userID int64, currentPassword, newPassword string) error {
	row := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID)

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

	_, err = s.db.Exec(`
		UPDATE users
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

	_, err = s.db.Exec(`
		UPDATE users
		SET password_hash = ?, must_change_password = 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, newHash, userID)
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM availability_entries WHERE real_name = ?`, realName); err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO availability_entries (real_name, week_type, shift_code, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for _, shiftCode := range uniqueStrings(payload.Single) {
		if _, err := insertStmt.Exec(realName, "single", shiftCode); err != nil {
			return err
		}
	}
	for _, shiftCode := range uniqueStrings(payload.Double) {
		if _, err := insertStmt.Exec(realName, "double", shiftCode); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) GetAvailabilityOverview() ([]types.AvailabilityOverviewItem, error) {
	rows, err := s.db.Query(`
		SELECT real_name, week_type, shift_code
		FROM availability_entries
		ORDER BY real_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lookup := map[string]types.AvailabilityPayload{}
	for _, realName := range config.UserNames {
		lookup[realName] = types.AvailabilityPayload{
			Single: []string{},
			Double: []string{},
		}
	}

	for rows.Next() {
		var realName string
		var weekType string
		var shiftCode string
		if err := rows.Scan(&realName, &weekType, &shiftCode); err != nil {
			return nil, err
		}

		payload := lookup[realName]
		if weekType == "single" {
			payload.Single = append(payload.Single, shiftCode)
		}
		if weekType == "double" {
			payload.Double = append(payload.Double, shiftCode)
		}
		lookup[realName] = payload
	}

	items := make([]types.AvailabilityOverviewItem, 0, len(config.UserNames))
	for _, realName := range config.UserNames {
		payload := lookup[realName]
		sort.Strings(payload.Single)
		sort.Strings(payload.Double)
		items = append(items, types.AvailabilityOverviewItem{
			Username:     config.UsernameByRealName[realName],
			RealName:     realName,
			Availability: payload,
		})
	}

	return items, nil
}

func (s *Store) GetSchedule() (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT shift_code, real_name, week_type
		FROM schedule_entries
		ORDER BY shift_code ASC, real_name ASC
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM schedule_entries`); err != nil {
		return err
	}

	insertStmt, err := tx.Prepare(`
		INSERT INTO schedule_entries (shift_code, real_name, week_type, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
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
			if _, err := insertStmt.Exec(shiftCode, realName, weekType); err != nil {
				return err
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
		INSERT INTO final_schedule_entries (week_number, shift_code, real_name)
		VALUES (?, ?, ?)
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
			if _, err := insertStmt.Exec(weekNumber, shiftCode, realName); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (s *Store) ListWorkOrders(month string) ([]types.WorkOrder, error) {
	if strings.TrimSpace(month) != "" && !isAllowedMonth(month) {
		return nil, fmt.Errorf("month out of allowed range")
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
	defer rows.Close()

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
			return nil, err
		}

		sessions, err := s.getWorkSessions(order.ID)
		if err != nil {
			return nil, err
		}
		order.WorkSessions = sessions
		workOrders = append(workOrders, order)
	}

	return workOrders, rows.Err()
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
		return workOrder, fmt.Errorf("month out of allowed range")
	}
	if !isAllowedMonth(workOrder.BelongingMonth) {
		return workOrder, fmt.Errorf("month out of allowed range")
	}
	if len(workOrder.WorkSessions) == 0 {
		return workOrder, fmt.Errorf("请至少提供一条有效工时记录")
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
		return workOrder, fmt.Errorf("month out of allowed range")
	}
	if !isAllowedMonth(workOrder.BelongingMonth) {
		return workOrder, fmt.Errorf("month out of allowed range")
	}
	if len(workOrder.WorkSessions) == 0 {
		return workOrder, fmt.Errorf("请至少提供一条有效工时记录")
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
		SELECT COUNT(DISTINCT real_name)
		FROM availability_entries
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
		return types.FinanceSummaryResponse{}, fmt.Errorf("month out of allowed range")
	}

	workOrders, err := s.ListWorkOrders(month)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	details := make([]types.FinanceWorkOrderDetail, 0)
	workOrderHours := 0.0
	for _, order := range workOrders {
		dates := make([]string, 0)
		total := 0.0
		for _, session := range order.WorkSessions {
			if session.WorkerName != realName {
				continue
			}
			total += session.Duration
			dates = append(dates, session.Date)
		}

		if total <= 0 {
			continue
		}

		workOrderHours += total
		details = append(details, types.FinanceWorkOrderDetail{
			Title:  order.Title,
			Dates:  strings.Join(dates, ", "),
			Hours:  total,
			Amount: total * 50,
		})
	}

	dutyHours, err := s.getMonthlyDutyHours(month, realName)
	if err != nil {
		return types.FinanceSummaryResponse{}, err
	}

	managementAmount := 0.0
	managementPending := false
	switch role {
	case "LEADER", "HR":
		if isFutureMonth(month, time.Now()) {
			managementPending = true
		} else {
			managementAmount = 800
		}
	case "OWNER":
		if isFutureMonth(month, time.Now()) {
			managementPending = true
		} else {
			managementAmount = 1200
		}
	}

	dutyAmount := dutyHours * 25
	workOrderAmount := workOrderHours * 50

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

func (s *Store) getMonthlyDutyHours(month, realName string) (float64, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return 0, fmt.Errorf("invalid month: %w", err)
	}

	scheduleCache := map[int]map[string][]string{}
	total := 0.0

	for current := start; current.Month() == start.Month(); current = current.AddDate(0, 0, 1) {
		dayCode, ok := weekdayCodeForDate(current)
		if !ok {
			continue
		}

		weekNumber := calculateWeekNumber(current, s.cfg.FirstMonday)
		schedule, ok := scheduleCache[weekNumber]
		if !ok {
			financeSchedule, err := s.GetFinalSchedule(weekNumber, current.Format("2006-01-02"))
			if err != nil {
				return 0, err
			}
			schedule = financeSchedule.Schedule
			scheduleCache[weekNumber] = schedule
		}

		for shiftCode, names := range schedule {
			if !strings.HasPrefix(shiftCode, dayCode+"-") {
				continue
			}
			if !stringSliceContains(names, realName) {
				continue
			}
			total += shiftDurationHours(shiftCode)
		}
	}

	return math.Round(total*10) / 10, nil
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
	parts := strings.Split(shiftCode, "-")
	if len(parts) != 2 {
		return 0
	}

	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 1 || index > len(config.TimeSlots) {
		return 0
	}

	return timeSlotDurationHours(config.TimeSlots[index-1])
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

func isFutureMonth(month string, now time.Time) bool {
	selected, err := time.Parse("2006-01", month)
	if err != nil {
		return false
	}

	selected = time.Date(selected.Year(), selected.Month(), 1, 0, 0, 0, 0, time.UTC)
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return selected.After(current)
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

	hourlyRate := 50.0
	userTotals := map[string]float64{}
	orderTotals := make([]float64, len(workOrders))
	perOrderUsers := make([]map[string]float64, len(workOrders))

	for orderIndex, workOrder := range workOrders {
		perUser := map[string]float64{}
		for _, session := range workOrder.WorkSessions {
			perUser[session.WorkerName] += session.Duration
			userTotals[session.WorkerName] += session.Duration
			orderTotals[orderIndex] += session.Duration
		}
		perOrderUsers[orderIndex] = perUser
	}

	for userIndex, realName := range config.UserNames {
		row := userIndex + 2
		nameCell, _ := excelize.CoordinatesToCellName(1, row)
		file.SetCellValue(sheetName, nameCell, realName)

		for orderIndex := range workOrders {
			value := perOrderUsers[orderIndex][realName]
			if value <= 0 {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(orderIndex+2, row)
			file.SetCellValue(sheetName, cell, value)
		}

		totalHours := userTotals[realName]
		hoursCell, _ := excelize.CoordinatesToCellName(len(workOrders)+2, row)
		amountCell, _ := excelize.CoordinatesToCellName(len(workOrders)+3, row)
		file.SetCellValue(sheetName, hoursCell, totalHours)
		file.SetCellValue(sheetName, amountCell, totalHours*hourlyRate)
	}

	summaryRow := len(config.UserNames) + 2
	totalHours := 0.0
	labelCell, _ := excelize.CoordinatesToCellName(1, summaryRow)
	file.SetCellValue(sheetName, labelCell, "总计")
	for orderIndex, orderTotal := range orderTotals {
		cell, _ := excelize.CoordinatesToCellName(orderIndex+2, summaryRow)
		file.SetCellValue(sheetName, cell, orderTotal)
		totalHours += orderTotal
	}
	hoursCell, _ := excelize.CoordinatesToCellName(len(workOrders)+2, summaryRow)
	amountCell, _ := excelize.CoordinatesToCellName(len(workOrders)+3, summaryRow)
	file.SetCellValue(sheetName, hoursCell, totalHours)
	file.SetCellValue(sheetName, amountCell, totalHours*hourlyRate)

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
		return nil, fmt.Errorf("month out of allowed range")
	}

	users, err := s.ListUsers()
	if err != nil {
		return nil, err
	}

	type financeUserRow struct {
		Name    string
		Summary types.FinanceSummaryResponse
	}

	rows := make([]financeUserRow, 0)
	for _, user := range users {
		if !user.IsActive || user.Role == "ADMIN" {
			continue
		}

		summary, err := s.GetFinanceSummary(month, user.RealName, user.Role)
		if err != nil {
			return nil, err
		}

		rows = append(rows, financeUserRow{
			Name:    user.RealName,
			Summary: summary,
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
func (s *Store) getFinalScheduleEntries(weekNumber int) (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT shift_code, real_name
		FROM final_schedule_entries
		WHERE week_number = ?
		ORDER BY shift_code ASC, real_name ASC
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
		SELECT shift_code, real_name, week_type
		FROM schedule_entries
		ORDER BY shift_code ASC, real_name ASC
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
		INSERT INTO work_sessions (work_order_id, date, worker_name, duration)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for _, session := range workOrder.WorkSessions {
		if _, err := insertStmt.Exec(workOrder.ID, session.Date, session.WorkerName, session.Duration); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) CreateSnapshot(destinationPath string) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}

	if err := os.Remove(destinationPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	statement := fmt.Sprintf("VACUUM INTO %s", sqliteStringLiteral(filepath.Clean(destinationPath)))
	_, err := s.db.Exec(statement)
	return err
}

func (s *Store) ImportSnapshot(snapshotPath string) error {
	if _, err := os.Stat(snapshotPath); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	attachStatement := fmt.Sprintf("ATTACH DATABASE %s AS syncsrc", sqliteStringLiteral(filepath.Clean(snapshotPath)))
	if _, err := tx.Exec(attachStatement); err != nil {
		return err
	}

	statements := []string{
		`DELETE FROM final_schedule_entries;`,
		`DELETE FROM final_schedules;`,
		`DELETE FROM work_sessions;`,
		`DELETE FROM work_orders;`,
		`DELETE FROM availability_entries;`,
		`DELETE FROM schedule_entries;`,
		`DELETE FROM users;`,
		`INSERT INTO users (id, username, password_hash, real_name, role, is_active, must_change_password, created_at, updated_at)
		 SELECT id, username, password_hash, real_name, role, is_active, must_change_password, created_at, updated_at
		 FROM syncsrc.users;`,
		`INSERT INTO availability_entries (id, real_name, week_type, shift_code, created_at)
		 SELECT id, real_name, week_type, shift_code, created_at
		 FROM syncsrc.availability_entries;`,
		`INSERT INTO schedule_entries (id, shift_code, real_name, week_type, created_at)
		 SELECT id, shift_code, real_name, week_type, created_at
		 FROM syncsrc.schedule_entries;`,
		`INSERT INTO final_schedules (week_number, selected_date, updated_by, updated_at)
		 SELECT week_number, selected_date, updated_by, updated_at
		 FROM syncsrc.final_schedules;`,
		`INSERT INTO final_schedule_entries (id, week_number, shift_code, real_name)
		 SELECT id, week_number, shift_code, real_name
		 FROM syncsrc.final_schedule_entries;`,
		`INSERT INTO work_orders (id, title, belonging_month, created_time, created_by)
		 SELECT id, title, belonging_month, created_time, created_by
		 FROM syncsrc.work_orders;`,
		`INSERT INTO work_sessions (id, work_order_id, date, worker_name, duration)
		 SELECT id, work_order_id, date, worker_name, duration
		 FROM syncsrc.work_sessions;`,
	}

	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DETACH DATABASE syncsrc`); err != nil {
		return err
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

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
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
