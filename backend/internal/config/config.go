package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	UsernameMaxBytes  = 64
	PasswordMinRunes  = 12
	PasswordMaxBytes  = 72
	JWTSecretMinBytes = 32
)

var ErrWeakPassword = errors.New("密码不符合安全要求")

type AppConfig struct {
	Port                 string
	ControlDatabasePath  string
	SemesterDatabaseDir  string
	JWTSecret            string
	AdminPassword        string
	FirstMonday          string
	EnvFilePath          string
	WorkStudyTemplateDir string
	WorkStudyContent     string
	// AccessTokenTTLSeconds controls the lifetime of JWT access tokens.
	// Refresh tokens always live for 7 days regardless of this value.
	AccessTokenTTLSeconds int
}

var WeekdaysCode = []string{"Mon", "Tue", "Wed", "Thu", "Fri"}
var WeekdaysDisplay = []string{"周一", "周二", "周三", "周四", "周五"}
var TimeSlots = []string{
	"8:00-10:00",
	"10:00-12:00",
	"13:30-15:30",
	"15:30-17:30",
	"18:10-20:10",
	"20:10-22:10",
}

var UserRoles = map[string]string{
	"ADMIN":   "管理员",
	"OWNER":   "负责人",
	"LEADER":  "组长",
	"HR":      "人事专员",
	"FINANCE": "财务",
	"USER":    "值班人员",
}

var RolePermissions = map[string][]string{
	"ADMIN": {
		"view_schedule", "manage_schedule", "manage_final_schedule", "view_workorders", "manage_workorders",
		"manage_users", "export_schedule", "export_workorders", "view_finance",
	},
	"OWNER": {
		"view_schedule", "manage_schedule", "manage_final_schedule", "view_workorders", "manage_workorders",
		"export_schedule", "export_workorders", "view_finance",
	},
	"LEADER":  {"view_schedule", "submit_availability", "view_workorders", "manage_workorders", "view_finance"},
	"HR":      {"view_schedule", "manage_schedule", "manage_final_schedule", "view_workorders", "export_schedule", "export_workorders", "view_finance"},
	"FINANCE": {"view_workorders", "manage_workorders", "export_workorders", "view_finance"},
	"USER":    {"view_schedule", "submit_availability", "view_finance"},
}

var memberDirectoryMu sync.RWMutex
var memberNames = []string{}
var realNameOrderIndex = map[string]int{}

func Load() (AppConfig, error) {
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}
	_, backendDir := resolveProjectPaths(workDir)
	envPath := filepath.Join(backendDir, ".env")
	envValues := map[string]string{}
	if _, statErr := os.Stat(envPath); statErr == nil {
		values, readErr := readEnvFile(envPath)
		if readErr != nil {
			return AppConfig{}, readErr
		}
		envValues = values
	} else if !os.IsNotExist(statErr) {
		return AppConfig{}, statErr
	}

	cfg := AppConfig{
		Port:                 getEnvValue(envValues, "APP_PORT", "3000"),
		ControlDatabasePath:  getEnvValue(envValues, "CONTROL_DATABASE_PATH", "../data/control.db"),
		SemesterDatabaseDir:  getEnvValue(envValues, "SEMESTER_DATABASE_DIR", "../data/semesters"),
		JWTSecret:            getEnvValue(envValues, "JWT_SECRET", ""),
		AdminPassword:        getEnvValue(envValues, "DEFAULT_ADMIN_PASSWORD", ""),
		FirstMonday:          "20260302",
		EnvFilePath:          envPath,
		WorkStudyTemplateDir: getEnvValue(envValues, "WORK_STUDY_TEMPLATE_DIR", "../data/work-study/templates"),
		WorkStudyContent:     "机房运维C5-569",
	}
	cfg.AccessTokenTTLSeconds = parsePositiveInt(getEnvValue(envValues, "ACCESS_TOKEN_TTL", "7200"), 7200)
	if len(cfg.JWTSecret) < JWTSecretMinBytes || cfg.JWTSecret == "please-change-me" {
		return AppConfig{}, fmt.Errorf("JWT_SECRET 必须显式设置为至少 %d 字节的随机值", JWTSecretMinBytes)
	}

	return cfg, nil
}

func ValidatePassword(username, password string) error {
	if utf8.RuneCountInString(password) < PasswordMinRunes {
		return fmt.Errorf("%w：至少需要 %d 个字符", ErrWeakPassword, PasswordMinRunes)
	}
	if len(password) > PasswordMaxBytes {
		return fmt.Errorf("%w：UTF-8 编码后不能超过 %d 字节", ErrWeakPassword, PasswordMaxBytes)
	}
	normalized := strings.ToLower(strings.TrimSpace(password))
	if normalized == strings.ToLower(strings.TrimSpace(username)) || normalized == "please-change-me" {
		return fmt.Errorf("%w：不能与用户名或项目默认值相同", ErrWeakPassword)
	}
	return nil
}

// ApplyMemberDirectory refreshes the active semester's display order without
// changing authentication data, which is stored in the global control database.
func ApplyMemberDirectory(names []string) {
	userNames := make([]string, 0, len(names))
	orderIndex := make(map[string]int, len(names))
	for _, name := range names {
		realName := strings.TrimSpace(name)
		if realName == "" {
			continue
		}
		orderIndex[realName] = len(userNames)
		userNames = append(userNames, realName)
	}
	memberDirectoryMu.Lock()
	defer memberDirectoryMu.Unlock()
	memberNames = userNames
	realNameOrderIndex = orderIndex
}

func MemberNames() []string {
	memberDirectoryMu.RLock()
	defer memberDirectoryMu.RUnlock()
	return append([]string(nil), memberNames...)
}

func realNameOrder(realName string) int {
	if index, ok := realNameOrderIndex[realName]; ok {
		return index
	}
	return len(memberNames) + 1000
}

func LessRealName(a, b string) bool {
	memberDirectoryMu.RLock()
	defer memberDirectoryMu.RUnlock()
	aIndex := realNameOrder(a)
	bIndex := realNameOrder(b)
	if aIndex != bIndex {
		return aIndex < bIndex
	}
	return a < b
}

func PermissionsFor(role string) []string {
	return append([]string(nil), RolePermissions[role]...)
}

func readEnvFile(envPath string) (map[string]string, error) {
	content, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("read env file %s: %w", envPath, err)
	}

	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values, nil
}

func getEnvValue(fileValues map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if value := strings.TrimSpace(fileValues[key]); value != "" {
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			return value[1 : len(value)-1]
		}
		return value
	}
	return fallback
}

func parsePositiveInt(text string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func resolveProjectPaths(workDir string) (string, string) {
	cleaned := filepath.Clean(workDir)

	if fileExists(filepath.Join(cleaned, "backend", ".env.example")) {
		return cleaned, filepath.Join(cleaned, "backend")
	}

	if fileExists(filepath.Join(cleaned, ".env.example")) &&
		dirExists(filepath.Join(cleaned, "cmd")) &&
		dirExists(filepath.Join(cleaned, "internal")) {
		return filepath.Dir(cleaned), cleaned
	}

	return cleaned, filepath.Join(cleaned, "backend")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
