package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/google/uuid"
)

const semesterSchemaVersion = 3

var ErrArchivedSemester = errors.New("当前学期已归档，不能修改")

func openManagedStore(cfg config.AppConfig) (*Store, error) {
	var err error
	cfg.ControlDatabasePath, err = resolveConfigPath(cfg, cfg.ControlDatabasePath)
	if err != nil {
		return nil, err
	}
	cfg.SemesterDatabaseDir, err = resolveConfigPath(cfg, cfg.SemesterDatabaseDir)
	if err != nil {
		return nil, err
	}
	cfg.WorkStudyTemplateDir, err = resolveConfigPath(cfg, cfg.WorkStudyTemplateDir)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(cfg.ControlDatabasePath), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.SemesterDatabaseDir, 0o755); err != nil {
		return nil, err
	}

	control, err := sql.Open("sqlite", cfg.ControlDatabasePath)
	if err != nil {
		return nil, err
	}
	if err := configureSQLite(control); err != nil {
		control.Close()
		return nil, err
	}
	if err := initControlSchema(control); err != nil {
		control.Close()
		return nil, err
	}
	if err := ensureInitialSemester(control, cfg); err != nil {
		control.Close()
		return nil, err
	}

	active, err := readActiveSemester(control, cfg.SemesterDatabaseDir)
	if err != nil {
		control.Close()
		return nil, err
	}
	db, err := sql.Open("sqlite", active.Database)
	if err != nil {
		control.Close()
		return nil, err
	}
	if err := configureSQLite(db); err != nil {
		db.Close()
		control.Close()
		return nil, err
	}

	store := &Store{db: db, control: control, cfg: cfg, active: active}
	if err := store.initSchema(); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.bootstrapAdmin(); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.ensureSemesterSettings(); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.reloadSemesterRuntime(); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func resolveConfigPath(cfg config.AppConfig, value string) (string, error) {
	value = os.ExpandEnv(strings.TrimSpace(value))
	if value == "" {
		return "", fmt.Errorf("配置路径不能为空")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	base := "."
	if cfg.EnvFilePath != "" {
		base = filepath.Dir(cfg.EnvFilePath)
	}
	return filepath.Abs(filepath.Join(base, value))
}

func configureSQLite(db *sql.DB) error {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, statement := range []string{
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA synchronous = NORMAL;`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func initControlSchema(db *sql.DB) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_uuid TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL UNIQUE,
			real_name TEXT NOT NULL DEFAULT '',
			student_number TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			must_change_password INTEGER NOT NULL DEFAULT 1,
			is_system_admin INTEGER NOT NULL DEFAULT 0,
			session_version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS semesters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			database_filename TEXT NOT NULL UNIQUE,
			first_monday TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0,
			draft INTEGER NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS system_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			context_version INTEGER NOT NULL DEFAULT 1
		);`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TEXT NOT NULL,
			revoked_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			username TEXT NOT NULL,
			real_name TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			status INTEGER NOT NULL DEFAULT 0,
			semester_id TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_account ON refresh_tokens(account_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(id DESC);`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_real_name ON accounts(real_name) WHERE TRIM(real_name) != ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_student_number ON accounts(student_number) WHERE TRIM(student_number) != ''`); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO system_state (id, context_version) VALUES (1, 1)`)
	return err
}

func ensureInitialSemester(control *sql.DB, cfg config.AppConfig) error {
	var count int
	if err := control.QueryRow(`SELECT COUNT(*) FROM semesters`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	id := uuid.NewString()
	filename := id + ".db"
	_, err := control.Exec(`
		INSERT INTO semesters (id, name, database_filename, first_monday, archived, draft, active)
		VALUES (?, ?, ?, ?, 0, 0, 1)
	`, id, "初始学期", filename, cfg.FirstMonday)
	return err
}

func readActiveSemester(control *sql.DB, semesterDir string) (types.SemesterSummary, error) {
	var item types.SemesterSummary
	var filename string
	var archived, draft, active int
	err := control.QueryRow(`
		SELECT id, name, database_filename, first_monday, archived, draft, active, created_at, updated_at
		FROM semesters WHERE active = 1 LIMIT 1
	`).Scan(&item.ID, &item.Name, &filename, &item.FirstMonday, &archived, &draft, &active, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	item.Database = filepath.Join(semesterDir, filename)
	item.Archived = archived == 1
	item.Draft = draft == 1
	item.Active = active == 1
	if err := control.QueryRow(`SELECT context_version FROM system_state WHERE id = 1`).Scan(&item.ContextVersion); err != nil {
		return item, err
	}
	return item, nil
}

func (s *Store) ensureSemesterSettings() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM semester_settings`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	rates := DefaultRateConfig()
	_, err := s.db.Exec(`
		INSERT INTO semester_settings (id, semester_id, name, first_monday, work_study_content, schema_version,
			duty_rate_cents, work_order_rate_cents, mgmt_leader_rate_cents, mgmt_owner_rate_cents)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.active.ID, s.active.Name, s.active.FirstMonday, s.cfg.WorkStudyContent, semesterSchemaVersion,
		rates.DutyCents, rates.WorkOrderCents, rates.MgmtLeaderCents, rates.MgmtOwnerCents)
	return err
}

func (s *Store) reloadSemesterRuntime() error {
	if err := s.loadSemesterSettings(); err != nil {
		return err
	}
	return s.refreshMemberOrdering()
}

func (s *Store) loadSemesterSettings() error {
	var dutyRate, workOrderRate, mgmtLeaderRate, mgmtOwnerRate sql.NullInt64
	err := s.db.QueryRow(`
		SELECT name, first_monday, work_study_content,
			duty_rate_cents, work_order_rate_cents, mgmt_leader_rate_cents, mgmt_owner_rate_cents
		FROM semester_settings WHERE id = 1
	`).Scan(&s.active.Name, &s.active.FirstMonday, &s.cfg.WorkStudyContent,
		&dutyRate, &workOrderRate, &mgmtLeaderRate, &mgmtOwnerRate)
	if err != nil {
		return err
	}
	s.cfg.FirstMonday = s.active.FirstMonday

	defaults := DefaultRateConfig()
	s.rates = RateConfig{
		DutyCents:       nullInt64Or(dutyRate, defaults.DutyCents),
		WorkOrderCents:  nullInt64Or(workOrderRate, defaults.WorkOrderCents),
		MgmtLeaderCents: nullInt64Or(mgmtLeaderRate, defaults.MgmtLeaderCents),
		MgmtOwnerCents:  nullInt64Or(mgmtOwnerRate, defaults.MgmtOwnerCents),
	}
	return nil
}

func nullInt64Or(value sql.NullInt64, fallback int64) int64 {
	if value.Valid {
		return value.Int64
	}
	return fallback
}

func (s *Store) refreshMemberOrdering() error {
	rows, err := s.db.Query(`SELECT real_name FROM users WHERE role != 'ADMIN' AND is_active = 1 ORDER BY sort_order, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var realName string
		if err := rows.Scan(&realName); err != nil {
			return err
		}
		names = append(names, realName)
	}
	config.ApplyMemberDirectory(names)
	return rows.Err()
}

// AcquireRequest pins one semester database for the lifetime of an HTTP request.
// Semester activation takes the write lock and therefore waits for in-flight
// requests to finish before replacing and closing the old database.
func (s *Store) AcquireRequest() (*Store, func(), error) {
	s.mu.RLock()
	requestStore := &Store{db: s.db, control: s.control, cfg: s.cfg, active: s.active, rates: s.rates}
	if err := requestStore.loadSemesterSettings(); err != nil {
		s.mu.RUnlock()
		return nil, nil, err
	}
	return requestStore, s.mu.RUnlock, nil
}

func (s *Store) ActiveSemester() types.SemesterSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func incrementContextVersion(tx *sql.Tx) (int64, error) {
	if _, err := tx.Exec(`UPDATE system_state SET context_version = context_version + 1 WHERE id = 1`); err != nil {
		return 0, err
	}
	var version int64
	if err := tx.QueryRow(`SELECT context_version FROM system_state WHERE id = 1`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) ListSemesters() ([]types.SemesterSummary, error) {
	var contextVersion int64
	if err := s.control.QueryRow(`SELECT context_version FROM system_state WHERE id = 1`).Scan(&contextVersion); err != nil {
		return nil, err
	}
	rows, err := s.control.Query(`
		SELECT id, name, database_filename, first_monday, archived, draft, active, created_at, updated_at
		FROM semesters ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.SemesterSummary, 0)
	for rows.Next() {
		var item types.SemesterSummary
		var filename string
		var archived, draft, active int
		if err := rows.Scan(&item.ID, &item.Name, &filename, &item.FirstMonday, &archived, &draft, &active, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Database = filepath.Join(s.cfg.SemesterDatabaseDir, filename)
		item.Archived = archived == 1
		item.Draft = draft == 1
		item.Active = active == 1
		if item.Active {
			item.ContextVersion = contextVersion
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateSemester(request types.CreateSemesterRequest) (types.SemesterSummary, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return types.SemesterSummary{}, fmt.Errorf("学期名称不能为空")
	}
	if !validFirstMonday(request.FirstMonday) {
		return types.SemesterSummary{}, fmt.Errorf("FIRST_MONDAY 必须是周一，格式为 YYYYMMDD")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.NewString()
	filename := id + ".db"
	path := filepath.Join(s.cfg.SemesterDatabaseDir, filename)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return types.SemesterSummary{}, err
	}
	defer db.Close()
	if err := configureSQLite(db); err != nil {
		return types.SemesterSummary{}, err
	}
	temp := &Store{db: db, control: s.control, cfg: s.cfg, active: types.SemesterSummary{ID: id, Name: name, FirstMonday: request.FirstMonday}}
	if err := temp.initSchema(); err != nil {
		return types.SemesterSummary{}, err
	}
	source := s.db
	if strings.TrimSpace(request.CloneFromID) != "" && request.CloneFromID != s.active.ID {
		var sourceFilename string
		if err := s.control.QueryRow(`SELECT database_filename FROM semesters WHERE id = ?`, request.CloneFromID).Scan(&sourceFilename); err != nil {
			return types.SemesterSummary{}, fmt.Errorf("复制来源学期不存在")
		}
		source, err = sql.Open("sqlite", filepath.Join(s.cfg.SemesterDatabaseDir, sourceFilename))
		if err != nil {
			return types.SemesterSummary{}, err
		}
		defer source.Close()
		if err := configureSQLite(source); err != nil {
			return types.SemesterSummary{}, err
		}
	}
	rows, err := source.Query(`SELECT account_uuid, username, role, sort_order, is_active, created_at FROM users ORDER BY sort_order, id`)
	if err != nil {
		return types.SemesterSummary{}, err
	}
	for rows.Next() {
		var accountUUID, username, role, createdAt string
		var sortOrder, isActive int
		if err := rows.Scan(&accountUUID, &username, &role, &sortOrder, &isActive, &createdAt); err != nil {
			rows.Close()
			return types.SemesterSummary{}, err
		}
		var realName, studentNumber string
		if err := s.control.QueryRow(`SELECT real_name, student_number FROM accounts WHERE account_uuid = ?`, accountUUID).Scan(&realName, &studentNumber); err != nil {
			rows.Close()
			return types.SemesterSummary{}, err
		}
		if _, err := db.Exec(`INSERT INTO users (account_uuid, username, password_hash, real_name, student_number, role, sort_order, is_active, must_change_password, created_at) VALUES (?, ?, '', ?, ?, ?, ?, ?, 0, ?)`, accountUUID, username, realName, studentNumber, role, sortOrder, isActive, createdAt); err != nil {
			rows.Close()
			return types.SemesterSummary{}, err
		}
	}
	if err := rows.Close(); err != nil {
		return types.SemesterSummary{}, err
	}
	sourceSettings := &Store{db: source}
	if err := sourceSettings.loadSemesterSettings(); err != nil {
		return types.SemesterSummary{}, err
	}
	rates := sourceSettings.rates
	_, err = db.Exec(`INSERT INTO semester_settings (id, semester_id, name, first_monday, work_study_content, schema_version,
		duty_rate_cents, work_order_rate_cents, mgmt_leader_rate_cents, mgmt_owner_rate_cents)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, name, request.FirstMonday, sourceSettings.cfg.WorkStudyContent, semesterSchemaVersion,
		rates.DutyCents, rates.WorkOrderCents, rates.MgmtLeaderCents, rates.MgmtOwnerCents)
	if err != nil {
		return types.SemesterSummary{}, err
	}
	_, err = s.control.Exec(`INSERT INTO semesters (id, name, database_filename, first_monday, archived, draft, active) VALUES (?, ?, ?, ?, 0, 1, 0)`, id, name, filename, request.FirstMonday)
	if err != nil {
		return types.SemesterSummary{}, err
	}
	return types.SemesterSummary{ID: id, Name: name, Database: path, Draft: true, FirstMonday: request.FirstMonday}, nil
}

func (s *Store) ActivateSemester(id string) (types.SemesterSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var target types.SemesterSummary
	var filename string
	var archived, draft int
	if err := s.control.QueryRow(`SELECT id, name, database_filename, first_monday, archived, draft, created_at, updated_at FROM semesters WHERE id = ?`, id).Scan(&target.ID, &target.Name, &filename, &target.FirstMonday, &archived, &draft, &target.CreatedAt, &target.UpdatedAt); err != nil {
		return target, err
	}
	target.Database = filepath.Join(s.cfg.SemesterDatabaseDir, filename)
	target.Archived = archived == 1
	target.Draft = draft == 1
	newDB, err := sql.Open("sqlite", target.Database)
	if err != nil {
		return target, err
	}
	if err := configureSQLite(newDB); err != nil {
		newDB.Close()
		return target, err
	}
	var version int
	if err := newDB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != semesterSchemaVersion {
		newDB.Close()
		return target, fmt.Errorf("目标学期数据库版本不兼容")
	}
	var quickCheck string
	if err := newDB.QueryRow(`PRAGMA quick_check`).Scan(&quickCheck); err != nil || quickCheck != "ok" {
		newDB.Close()
		return target, fmt.Errorf("目标学期数据库完整性检查失败")
	}
	tx, err := s.control.Begin()
	if err != nil {
		newDB.Close()
		return target, err
	}
	if target.Draft && s.active.ID != target.ID {
		if _, err := tx.Exec(`UPDATE semesters SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, s.active.ID); err != nil {
			tx.Rollback()
			newDB.Close()
			return target, err
		}
	}
	if _, err := tx.Exec(`UPDATE semesters SET active = 0`); err != nil {
		tx.Rollback()
		newDB.Close()
		return target, err
	}
	if _, err := tx.Exec(`UPDATE semesters SET active = 1, draft = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		tx.Rollback()
		newDB.Close()
		return target, err
	}
	contextVersion, err := incrementContextVersion(tx)
	if err != nil {
		tx.Rollback()
		newDB.Close()
		return target, err
	}
	if err := tx.Commit(); err != nil {
		newDB.Close()
		return target, err
	}
	oldDB := s.db
	s.db = newDB
	target.Active = true
	target.Draft = false
	target.ContextVersion = contextVersion
	s.active = target
	if err := s.reloadSemesterRuntime(); err != nil {
		s.db = oldDB
		newDB.Close()
		return target, err
	}
	if oldDB != nil && oldDB != newDB {
		_ = oldDB.Close()
	}
	return s.active, nil
}

func (s *Store) SetSemesterArchived(id string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := 0
	if archived {
		value = 1
	}
	tx, err := s.control.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE semesters SET archived = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, value, id); err != nil {
		tx.Rollback()
		return err
	}
	var contextVersion int64
	if s.active.ID == id {
		contextVersion, err = incrementContextVersion(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.active.ID == id {
		s.active.Archived = archived
		s.active.ContextVersion = contextVersion
	}
	return nil
}

func (s *Store) UpdateSemesterName(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("学期名称不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.control.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE semesters SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, id); err != nil {
		tx.Rollback()
		return err
	}
	var contextVersion int64
	if s.active.ID == id {
		contextVersion, err = incrementContextVersion(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.active.ID == id {
		if _, err := s.db.Exec(`UPDATE semester_settings SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, name); err != nil {
			return err
		}
		s.active.Name = name
		s.active.ContextVersion = contextVersion
	}
	return nil
}

func (s *Store) DeleteDraftSemester(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var filename string
	var draft, active int
	if err := s.control.QueryRow(`SELECT database_filename, draft, active FROM semesters WHERE id = ?`, id).Scan(&filename, &draft, &active); err != nil {
		return err
	}
	if draft != 1 || active == 1 {
		return fmt.Errorf("只能删除未启用的草稿学期")
	}
	if _, err := s.control.Exec(`DELETE FROM semesters WHERE id = ?`, id); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.cfg.SemesterDatabaseDir, filename))
}

func (s *Store) UpdateSemesterSettings(firstMonday, content string, rates RateConfig) error {
	if !validFirstMonday(firstMonday) {
		return fmt.Errorf("FIRST_MONDAY 必须是周一，格式为 YYYYMMDD")
	}
	if s.active.Archived {
		return ErrArchivedSemester
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("记录表工作内容不能为空")
	}
	if err := rates.validate(); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE semester_settings SET first_monday = ?, work_study_content = ?,
		duty_rate_cents = ?, work_order_rate_cents = ?, mgmt_leader_rate_cents = ?, mgmt_owner_rate_cents = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`,
		firstMonday, content, rates.DutyCents, rates.WorkOrderCents, rates.MgmtLeaderCents, rates.MgmtOwnerCents); err != nil {
		return err
	}
	if _, err := s.control.Exec(`UPDATE semesters SET first_monday = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, firstMonday, s.active.ID); err != nil {
		return err
	}
	return s.loadSemesterSettings()
}

func (s *Store) GetSemesterSettings() (types.SystemSettingsResponse, error) {
	var response types.SystemSettingsResponse
	if err := s.db.QueryRow(`SELECT first_monday, work_study_content FROM semester_settings WHERE id = 1`).Scan(&response.FirstMonday, &response.WorkStudyContent); err != nil {
		return response, err
	}
	rates := s.rates
	response.DutyRate = rates.DutyYuan()
	response.WorkOrderRate = rates.WorkOrderYuan()
	response.MgmtLeaderRate = float64(rates.MgmtLeaderCents) / 100
	response.MgmtOwnerRate = float64(rates.MgmtOwnerCents) / 100
	response.Semester = s.active
	return response, nil
}

// validRealName rejects names that could escape the template directory when
// embedded into file paths, plus control characters and oversized names.
func validRealName(name string) bool {
	if name == "" || len([]rune(name)) > 32 {
		return false
	}
	if name != strings.TrimSpace(name) {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return false
		}
	}
	return true
}

func (s *Store) CreateSemesterMember(request types.CreateMemberRequest) error {
	if s.active.Archived {
		return ErrArchivedSemester
	}
	username := strings.TrimSpace(request.Username)
	realName := strings.TrimSpace(request.RealName)
	studentNumber := strings.TrimSpace(request.StudentNumber)
	role := strings.TrimSpace(request.Role)
	if username == "" || realName == "" {
		return fmt.Errorf("用户名和姓名不能为空")
	}
	if len(username) > config.UsernameMaxBytes {
		return fmt.Errorf("用户名不能超过 %d 字节", config.UsernameMaxBytes)
	}
	if !validRealName(realName) {
		return fmt.Errorf("姓名不能包含 / \\ : * ? \" < > | 、连续点号或首尾空格，且不超过 32 个字符")
	}
	if err := validateStudentNumber(studentNumber); err != nil {
		return err
	}
	for _, r := range username {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r)) {
			return fmt.Errorf("用户名只能包含字母、数字、点、下划线和短横线")
		}
	}
	if role == "" {
		role = "USER"
	}
	if role == "ADMIN" {
		return fmt.Errorf("不能创建额外系统管理员")
	}
	if _, ok := config.UserRoles[role]; !ok {
		return fmt.Errorf("非法角色")
	}
	var accountUUID, globalNumber string
	var systemAdmin int
	err := s.control.QueryRow(`SELECT account_uuid, student_number, is_system_admin FROM accounts WHERE username = ?`, username).Scan(&accountUUID, &globalNumber, &systemAdmin)
	newAccount := err == sql.ErrNoRows
	if err != nil && !newAccount {
		return err
	}
	if systemAdmin == 1 {
		return fmt.Errorf("不能把系统管理员加入值班成员")
	}
	if studentNumber == "" && !newAccount {
		studentNumber = strings.TrimSpace(globalNumber)
	}
	if err := validateStudentNumber(studentNumber); err != nil {
		return err
	}
	var membershipExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ? OR real_name = ? OR (? != '' AND student_number = ?)`, username, realName, studentNumber, studentNumber).Scan(&membershipExists); err != nil {
		return err
	}
	if membershipExists > 0 {
		return fmt.Errorf("用户名、姓名或学号已存在于当前学期")
	}
	var duplicate int
	if err := s.control.QueryRow(`SELECT COUNT(*) FROM accounts WHERE account_uuid != ? AND (real_name = ? OR (? != '' AND student_number = ?))`, accountUUID, realName, studentNumber, studentNumber).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return fmt.Errorf("姓名或学号已存在于全局账户")
	}
	controlTx, err := s.control.Begin()
	if err != nil {
		return err
	}
	defer controlTx.Rollback()
	if newAccount {
		password := request.InitialPassword
		if err := config.ValidatePassword(username, password); err != nil {
			return err
		}
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		accountUUID = uuid.NewString()
		if _, err := controlTx.Exec(`INSERT INTO accounts (account_uuid, username, real_name, student_number, password_hash, is_active, must_change_password, is_system_admin) VALUES (?, ?, ?, ?, ?, 1, 1, 0)`, accountUUID, username, realName, studentNumber, hash); err != nil {
			return err
		}
	} else {
		if _, err := controlTx.Exec(`UPDATE accounts SET real_name = ?, student_number = ?, updated_at = CURRENT_TIMESTAMP WHERE account_uuid = ?`, realName, studentNumber, accountUUID); err != nil {
			return err
		}
	}
	var sortOrder int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM users`).Scan(&sortOrder); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO users (account_uuid, username, password_hash, real_name, student_number, role, sort_order, is_active, must_change_password) VALUES (?, ?, '', ?, ?, ?, ?, 1, 0)`, accountUUID, username, realName, studentNumber, role, sortOrder); err != nil {
		return err
	}
	if err := controlTx.Commit(); err != nil {
		return err
	}
	return s.refreshMemberOrdering()
}

func (s *Store) UpdateSemesterMember(id int64, request types.UpdateMemberRequest) error {
	if s.active.Archived {
		return ErrArchivedSemester
	}
	realName := strings.TrimSpace(request.RealName)
	role := strings.TrimSpace(request.Role)
	if realName == "" || role == "" {
		return fmt.Errorf("姓名和角色不能为空")
	}
	if !validRealName(realName) {
		return fmt.Errorf("姓名不能包含 / \\ : * ? \" < > | 、连续点号或首尾空格，且不超过 32 个字符")
	}
	var oldName, oldStudentNumber, oldRole, accountUUID string
	if err := s.db.QueryRow(`SELECT real_name, student_number, role, account_uuid FROM users WHERE id = ?`, id).Scan(&oldName, &oldStudentNumber, &oldRole, &accountUUID); err != nil {
		return err
	}
	studentNumber := oldStudentNumber
	if request.StudentNumber != nil {
		studentNumber = strings.TrimSpace(*request.StudentNumber)
	}
	if err := validateStudentNumber(studentNumber); err != nil {
		return err
	}
	if role == "ADMIN" {
		return fmt.Errorf("不能修改系统管理员")
	}
	if _, ok := config.UserRoles[role]; !ok {
		return fmt.Errorf("非法角色")
	}
	if oldRole == "ADMIN" {
		return fmt.Errorf("不能修改系统管理员")
	}
	var duplicate int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id != ? AND (real_name = ? OR (? != '' AND student_number = ?))`, id, realName, studentNumber, studentNumber).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return fmt.Errorf("姓名或学号已存在于当前学期")
	}
	if err := s.control.QueryRow(`SELECT COUNT(*) FROM accounts WHERE account_uuid != ? AND (real_name = ? OR (? != '' AND student_number = ?))`, accountUUID, realName, studentNumber, studentNumber).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return fmt.Errorf("姓名或学号已存在于全局账户")
	}
	var sortOrder any
	if request.SortOrder != nil {
		if *request.SortOrder < 1 {
			return fmt.Errorf("成员排序必须大于 0")
		}
		sortOrder = *request.SortOrder
	}
	controlTx, err := s.control.Begin()
	if err != nil {
		return err
	}
	defer controlTx.Rollback()
	if _, err := controlTx.Exec(`UPDATE accounts SET real_name = ?, student_number = ?, updated_at = CURRENT_TIMESTAMP WHERE account_uuid = ?`, realName, studentNumber, accountUUID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET real_name = ?, student_number = ?, role = ?, sort_order = COALESCE(?, sort_order), updated_at = CURRENT_TIMESTAMP WHERE id = ?`, realName, studentNumber, role, sortOrder, id); err != nil {
		return err
	}
	for _, statement := range []string{
		`UPDATE availability_entries SET real_name = ? WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`,
		`UPDATE schedule_entries SET real_name = ? WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`,
		`UPDATE final_schedule_entries SET real_name = ? WHERE member_id = ? OR (member_id IS NULL AND real_name = ?)`,
		`UPDATE work_sessions SET worker_name = ? WHERE member_id = ? OR (member_id IS NULL AND worker_name = ?)`,
	} {
		if _, err := tx.Exec(statement, realName, id, oldName); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := controlTx.Commit(); err != nil {
		return err
	}
	return s.refreshMemberOrdering()
}

func validateStudentNumber(value string) error {
	if value == "" {
		return nil
	}
	if len(value) < 6 || len(value) > 32 {
		return fmt.Errorf("学号长度必须为 6 到 32 位")
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return fmt.Errorf("学号只能包含数字")
		}
	}
	return nil
}

func (s *Store) RemoveSemesterMember(id int64) error {
	if s.active.Archived {
		return ErrArchivedSemester
	}
	result, err := s.db.Exec(`UPDATE users SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND role != 'ADMIN'`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("成员不存在或不能移除")
	}
	return s.refreshMemberOrdering()
}

func (s *Store) RestoreSemesterMember(id int64) error {
	if s.active.Archived {
		return ErrArchivedSemester
	}
	result, err := s.db.Exec(`UPDATE users SET is_active = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND role != 'ADMIN'`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("成员不存在或不能恢复")
	}
	return s.refreshMemberOrdering()
}

func validFirstMonday(value string) bool {
	parsed, err := time.Parse("20060102", strings.TrimSpace(value))
	return err == nil && parsed.Weekday() == time.Monday
}

func (s *Store) insertFinanceBatch(batch types.FinanceLocalBatch, excelContent, csvContent []byte) error {
	workOrderIDs, err := json.Marshal(batch.WorkOrderIDs)
	if err != nil {
		return err
	}
	excelHash := sha256.Sum256(excelContent)
	csvHash := sha256.Sum256(csvContent)
	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO finance_batches
		(id, created_at, start_date, end_date, output_month, work_order_ids_json, include_management, management_months,
		 excel_filename, csv_filename, excel_blob, csv_blob, excel_sha256, csv_sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, batch.ID, batch.CreatedAt, batch.StartDate, batch.EndDate, batch.OutputMonth, string(workOrderIDs), boolToInt(batch.IncludeManagement), batch.ManagementMonths,
		batch.ExcelFilename, batch.CSVFilename, excelContent, csvContent, hex.EncodeToString(excelHash[:]), hex.EncodeToString(csvHash[:]))
	return err
}

func (s *Store) semesterDatabasePath(id string) (string, error) {
	var filename string
	if err := s.control.QueryRow(`SELECT database_filename FROM semesters WHERE id = ?`, id).Scan(&filename); err != nil {
		return "", err
	}
	return filepath.Join(s.cfg.SemesterDatabaseDir, filename), nil
}

func (s *Store) ExportSemester(id string) (string, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, err := s.semesterDatabasePath(id)
	if err != nil {
		return "", nil, err
	}
	db := s.db
	if id != s.active.ID {
		db, err = sql.Open("sqlite", path)
		if err != nil {
			return "", nil, err
		}
		defer db.Close()
		if err := configureSQLite(db); err != nil {
			return "", nil, err
		}
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		return "", nil, err
	}
	temp, err := os.CreateTemp(s.cfg.SemesterDatabaseDir, ".semester-export-*.db")
	if err != nil {
		return "", nil, err
	}
	tempPath := temp.Name()
	temp.Close()
	os.Remove(tempPath)
	defer os.Remove(tempPath)
	quoted := strings.ReplaceAll(tempPath, "'", "''")
	if _, err := db.Exec(`VACUUM INTO '` + quoted + `'`); err != nil {
		return "", nil, err
	}
	content, err := os.ReadFile(tempPath)
	if err != nil {
		return "", nil, err
	}
	var name string
	if err := s.control.QueryRow(`SELECT name FROM semesters WHERE id = ?`, id).Scan(&name); err != nil {
		return "", nil, err
	}
	return safeSemesterFilename(name) + ".db", content, nil
}

func (s *Store) ImportSemester(content []byte) (types.SemesterSummary, error) {
	if len(content) == 0 {
		return types.SemesterSummary{}, fmt.Errorf("数据库文件为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	temp, err := os.CreateTemp(s.cfg.SemesterDatabaseDir, ".semester-import-*.db")
	if err != nil {
		return types.SemesterSummary{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return types.SemesterSummary{}, err
	}
	if err := temp.Close(); err != nil {
		return types.SemesterSummary{}, err
	}
	db, err := sql.Open("sqlite", tempPath)
	if err != nil {
		return types.SemesterSummary{}, err
	}
	defer db.Close()
	if err := configureSQLite(db); err != nil {
		return types.SemesterSummary{}, fmt.Errorf("无法打开学期数据库: %w", err)
	}
	var check string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		return types.SemesterSummary{}, fmt.Errorf("数据库完整性检查失败")
	}
	var id, name, firstMonday string
	var version int
	if err := db.QueryRow(`SELECT semester_id, name, first_monday, schema_version FROM semester_settings WHERE id = 1`).Scan(&id, &name, &firstMonday, &version); err != nil {
		return types.SemesterSummary{}, fmt.Errorf("不是有效的 DMS 学期数据库")
	}
	if _, err := uuid.Parse(id); err != nil || version != semesterSchemaVersion || !validFirstMonday(firstMonday) {
		return types.SemesterSummary{}, fmt.Errorf("学期数据库版本或元数据不兼容")
	}
	var exists int
	if err := s.control.QueryRow(`SELECT COUNT(*) FROM semesters WHERE id = ?`, id).Scan(&exists); err != nil {
		return types.SemesterSummary{}, err
	}
	if exists > 0 {
		return types.SemesterSummary{}, fmt.Errorf("该学期数据库已经存在")
	}

	rows, err := db.Query(`SELECT account_uuid, username, real_name, student_number FROM users WHERE role != 'ADMIN'`)
	if err != nil {
		return types.SemesterSummary{}, fmt.Errorf("学期成员表无效")
	}
	seenAccountUUIDs := map[string]struct{}{}
	for rows.Next() {
		var accountUUID, username, realName, studentNumber string
		if err := rows.Scan(&accountUUID, &username, &realName, &studentNumber); err != nil {
			rows.Close()
			return types.SemesterSummary{}, err
		}
		if _, err := uuid.Parse(accountUUID); err != nil {
			rows.Close()
			return types.SemesterSummary{}, fmt.Errorf("学期成员 %s 缺少有效的全局账户 UUID", username)
		}
		if _, exists := seenAccountUUIDs[accountUUID]; exists {
			rows.Close()
			return types.SemesterSummary{}, fmt.Errorf("学期成员存在重复的全局账户 UUID")
		}
		seenAccountUUIDs[accountUUID] = struct{}{}
		var existingUsername string
		err := s.control.QueryRow(`SELECT username FROM accounts WHERE account_uuid = ?`, accountUUID).Scan(&existingUsername)
		if err == nil {
			if existingUsername != username {
				rows.Close()
				return types.SemesterSummary{}, fmt.Errorf("全局账户 UUID 与用户名不一致")
			}
			continue
		}
		if err != sql.ErrNoRows {
			rows.Close()
			return types.SemesterSummary{}, err
		}
		var existingUUID string
		if err := s.control.QueryRow(`SELECT account_uuid FROM accounts WHERE username = ?`, username).Scan(&existingUUID); err == nil {
			rows.Close()
			return types.SemesterSummary{}, fmt.Errorf("用户名 %s 已关联其他全局账户 UUID", username)
		} else if err != sql.ErrNoRows {
			rows.Close()
			return types.SemesterSummary{}, err
		}
		randomHash, err := hashPassword(uuid.NewString())
		if err != nil {
			rows.Close()
			return types.SemesterSummary{}, err
		}
		if _, err := s.control.Exec(`INSERT INTO accounts (account_uuid, username, real_name, student_number, password_hash, is_active, must_change_password, is_system_admin) VALUES (?, ?, ?, ?, ?, 0, 1, 0)`, accountUUID, username, realName, studentNumber, randomHash); err != nil {
			rows.Close()
			return types.SemesterSummary{}, err
		}
	}
	if err := rows.Close(); err != nil {
		return types.SemesterSummary{}, err
	}
	var adminAccountUUID, adminUsername string
	if err := s.control.QueryRow(`SELECT account_uuid, username FROM accounts WHERE is_system_admin = 1 LIMIT 1`).Scan(&adminAccountUUID, &adminUsername); err == nil {
		if _, err := db.Exec(`DELETE FROM users WHERE role = 'ADMIN'`); err != nil {
			return types.SemesterSummary{}, err
		}
		if _, err := db.Exec(`INSERT INTO users (account_uuid, username, password_hash, real_name, student_number, role, is_active, must_change_password) VALUES (?, ?, '', '系统管理员', '', 'ADMIN', 1, 0)`, adminAccountUUID, adminUsername); err != nil {
			return types.SemesterSummary{}, err
		}
	}
	_ = db.Close()
	filename := id + ".db"
	target := filepath.Join(s.cfg.SemesterDatabaseDir, filename)
	if err := os.Rename(tempPath, target); err != nil {
		return types.SemesterSummary{}, err
	}
	if _, err := s.control.Exec(`INSERT INTO semesters (id, name, database_filename, first_monday, archived, draft, active) VALUES (?, ?, ?, ?, 1, 0, 0)`, id, name, filename, firstMonday); err != nil {
		_ = os.Remove(target)
		return types.SemesterSummary{}, err
	}
	return types.SemesterSummary{ID: id, Name: name, Database: target, FirstMonday: firstMonday, Archived: true}, nil
}

func safeSemesterFilename(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, value)
	if value == "" {
		return "semester"
	}
	return value
}
