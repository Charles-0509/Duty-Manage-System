package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AppConfig struct {
	Port               string
	DatabasePath       string
	JWTSecret          string
	AdminPassword      string
	FirstMonday        string
	SyncEnabled        bool
	SyncToken          string
	PrivateMembersPath string
	EnvFilePath        string
}

type SeedUser struct {
	Username           string
	Password           string
	RealName           string
	Role               string
	MustChangePassword bool
}

type PrivateMember struct {
	Username           string `json:"username"`
	RealName           string `json:"realName"`
	Role               string `json:"role,omitempty"`
	InitialPassword    string `json:"initialPassword,omitempty"`
	MustChangePassword *bool  `json:"mustChangePassword,omitempty"`
}

type privateMembersFile struct {
	Members []PrivateMember `json:"members"`
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
	"USER":  "值班人员",
	"ADMIN": "管理员",
	"HR":    "人事专员",
}

var RolePermissions = map[string][]string{
	"USER": {
		"view_schedule",
		"submit_availability",
		"view_workorders",
	},
	"ADMIN": {
		"view_schedule",
		"manage_schedule",
		"manage_final_schedule",
		"view_workorders",
		"manage_workorders",
		"manage_users",
		"export_schedule",
		"export_workorders",
	},
	"HR": {
		"view_schedule",
		"manage_schedule",
		"manage_final_schedule",
		"view_workorders",
		"export_schedule",
		"export_workorders",
	},
}

var UserNames = []string{}
var UsernameByRealName = map[string]string{}

var seedMembers = []SeedUser{}
var realNameOrderIndex = map[string]int{}

func Load() (AppConfig, error) {
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}
	_, backendDir := resolveProjectPaths(workDir)

	cfg := AppConfig{
		Port:               getEnv("APP_PORT", "3000"),
		DatabasePath:       getEnv("DATABASE_PATH", "./data/personnel.db"),
		JWTSecret:          getEnv("JWT_SECRET", "please-change-me"),
		AdminPassword:      getEnv("DEFAULT_ADMIN_PASSWORD", "admin"),
		FirstMonday:        getEnv("FIRST_MONDAY", "20260302"),
		SyncEnabled:        getEnvBool("SYNC_ENABLED", false),
		SyncToken:          getEnv("SYNC_TOKEN", ""),
		PrivateMembersPath: getEnv("PRIVATE_MEMBERS_PATH", "./data/member.json"),
		EnvFilePath:        filepath.Join(backendDir, ".env"),
	}

	if err := loadPrivateMembers(cfg.PrivateMembersPath); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func loadPrivateMembers(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("private members file not found: %s; copy backend/member.example.json to this path and keep the real file out of Git", path)
		}
		return fmt.Errorf("read private members file: %w", err)
	}

	var payload privateMembersFile
	if err := json.Unmarshal(content, &payload); err != nil {
		return fmt.Errorf("parse private members file %s: %w", path, err)
	}

	return applyPrivateMembers(payload.Members)
}

func applyPrivateMembers(members []PrivateMember) error {
	userNames := make([]string, 0, len(members))
	usernameByRealName := make(map[string]string, len(members))
	orderIndex := make(map[string]int, len(members))
	nextSeedMembers := make([]SeedUser, 0, len(members))
	seenUsernames := map[string]struct{}{}
	seenRealNames := map[string]struct{}{}

	for index, member := range members {
		username := strings.TrimSpace(member.Username)
		realName := strings.TrimSpace(member.RealName)
		role := strings.TrimSpace(member.Role)
		password := strings.TrimSpace(member.InitialPassword)

		if username == "" {
			return fmt.Errorf("private members[%d].username is required", index)
		}
		if realName == "" {
			return fmt.Errorf("private members[%d].realName is required", index)
		}
		if role == "" {
			role = "USER"
		}
		if role == "ADMIN" {
			return fmt.Errorf("private members[%d] cannot use ADMIN role; system admin is seeded separately", index)
		}
		if _, ok := AllUserRoles()[role]; !ok {
			return fmt.Errorf("private members[%d] uses unsupported role %q", index, role)
		}
		if password == "" {
			password = username
		}
		if _, exists := seenUsernames[username]; exists {
			return fmt.Errorf("duplicate username in private members file: %s", username)
		}
		if _, exists := seenRealNames[realName]; exists {
			return fmt.Errorf("duplicate realName in private members file: %s", realName)
		}

		mustChangePassword := true
		if member.MustChangePassword != nil {
			mustChangePassword = *member.MustChangePassword
		}

		seenUsernames[username] = struct{}{}
		seenRealNames[realName] = struct{}{}
		userNames = append(userNames, realName)
		usernameByRealName[realName] = username
		orderIndex[realName] = len(userNames) - 1
		nextSeedMembers = append(nextSeedMembers, SeedUser{
			Username:           username,
			Password:           password,
			RealName:           realName,
			Role:               role,
			MustChangePassword: mustChangePassword,
		})
	}

	UserNames = userNames
	UsernameByRealName = usernameByRealName
	realNameOrderIndex = orderIndex
	seedMembers = nextSeedMembers
	return nil
}

func RealNameOrder(realName string) int {
	if index, ok := realNameOrderIndex[realName]; ok {
		return index
	}
	return len(UserNames) + 1000
}

func LessRealName(a, b string) bool {
	aIndex := RealNameOrder(a)
	bIndex := RealNameOrder(b)
	if aIndex != bIndex {
		return aIndex < bIndex
	}
	return a < b
}

func PermissionsFor(role string) []string {
	permissions := AllRolePermissions()[role]
	result := make([]string, len(permissions))
	copy(result, permissions)
	return result
}

func AllUserRoles() map[string]string {
	result := map[string]string{}
	for role, label := range UserRoles {
		result[role] = label
	}

	result["LEADER"] = "组长"
	result["OWNER"] = "负责人"
	result["ADMIN"] = "管理员"
	result["USER"] = "值班人员"
	result["HR"] = "人事专员"
	return result
}

func AllRolePermissions() map[string][]string {
	result := map[string][]string{}
	for role, permissions := range RolePermissions {
		copied := make([]string, len(permissions))
		copy(copied, permissions)
		result[role] = copied
	}

	result["USER"] = []string{
		"view_schedule",
		"submit_availability",
		"view_finance",
	}
	result["LEADER"] = []string{
		"view_schedule",
		"submit_availability",
		"view_workorders",
		"manage_workorders",
		"view_finance",
	}
	result["OWNER"] = []string{
		"view_schedule",
		"manage_schedule",
		"manage_final_schedule",
		"view_workorders",
		"manage_workorders",
		"export_schedule",
		"export_workorders",
		"view_finance",
	}
	result["ADMIN"] = []string{
		"view_schedule",
		"manage_schedule",
		"manage_final_schedule",
		"view_workorders",
		"manage_workorders",
		"manage_users",
		"export_schedule",
		"export_workorders",
		"view_finance",
	}
	result["HR"] = []string{
		"view_schedule",
		"manage_schedule",
		"manage_final_schedule",
		"view_workorders",
		"export_schedule",
		"export_workorders",
		"view_finance",
	}

	return result
}

func DefaultUsers(adminPassword string) []SeedUser {
	users := []SeedUser{
		{
			Username:           "admin",
			Password:           adminPassword,
			RealName:           "系统管理员",
			Role:               "ADMIN",
			MustChangePassword: false,
		},
	}

	users = append(users, seedMembers...)
	return users
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
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
