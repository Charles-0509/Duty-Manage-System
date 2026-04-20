package config

import "os"

type AppConfig struct {
	Port          string
	DatabasePath  string
	JWTSecret     string
	AdminPassword string
	FirstMonday   string
	SyncEnabled   bool
	SyncToken     string
}

type SeedUser struct {
	Username           string
	Password           string
	RealName           string
	Role               string
	MustChangePassword bool
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
		"manage_final_schedule",
		"view_workorders",
		"export_workorders",
	},
}

var UserNames = []string{
	"叶梓枫", "熊昊臻", "江芊桦", "张新宇", "吴一帆", "唐育豪", "严慧仪", "薛浩然", "吴昶予", "李霈霖", "汤煜", "纪锐津", "黄广涛", "徐梓玮", "黄源兴", "张泽华", "万腾远", "郑雅淳", "于渼琦", "张馨怡", "刘思洁", "吴嘉伟", "邓智豪", "辜锡伟", "许德佳", "钟宇", "邓志峰", "罗梓基", "林淼", "黄佳炫",
}

var realNameOrderIndex = func() map[string]int {
	index := make(map[string]int, len(UserNames))
	for i, name := range UserNames {
		index[name] = i
	}
	return index
}()

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

var NameToPinyin = map[string]string{
	"叶梓枫": "yezifeng",
	"熊昊臻": "xionghaozhen",
	"江芊桦": "jiangqianhua",
	"张新宇": "zhangxinyu",
	"吴一帆": "wuyifan",
	"唐育豪": "tangyuhao",
	"许德佳": "xudejia",
	"郑雅淳": "zhengyachun",
	"于渼琦": "yumeiqi",
	"张馨怡": "zhangxinyi",
	"刘思洁": "liusijie",
	"吴嘉伟": "wujiawei",
	"邓智豪": "dengzhihao",
	"辜锡伟": "guxiwei",
	"钟宇":  "zhongyu",
	"邓志峰": "dengzhifeng",
	"罗梓基": "luoziji",
	"林淼":  "linmiao",
	"黄佳炫": "huangjiaxuan",
	"杨锐坤": "yangruikun",
	"纪锐津": "jiruijin",
	"黄广涛": "huangguangtao",
	"徐梓玮": "xuziwei",
	"黄源兴": "huangyuanxing",
	"张泽华": "zhangzehua",
	"万腾远": "wantengyuan",
	"严慧仪": "yanhuiyi",
	"薛浩然": "xuehaoran",
	"吴昶予": "wuchangyu",
	"李霈霖": "lipeilin",
	"汤煜":  "tangyu",
}

func Load() AppConfig {
	return AppConfig{
		Port:          getEnv("APP_PORT", "8080"),
		DatabasePath:  getEnv("DATABASE_PATH", "./data/personnel.db"),
		JWTSecret:     getEnv("JWT_SECRET", "please-change-me"),
		AdminPassword: getEnv("DEFAULT_ADMIN_PASSWORD", "admin"),
		FirstMonday:   getEnv("FIRST_MONDAY", "20260302"),
		SyncEnabled:   getEnvBool("SYNC_ENABLED", false),
		SyncToken:     getEnv("SYNC_TOKEN", ""),
	}
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
		"manage_final_schedule",
		"view_workorders",
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

	for _, realName := range UserNames {
		users = append(users, SeedUser{
			Username:           NameToPinyin[realName],
			Password:           NameToPinyin[realName],
			RealName:           realName,
			Role:               "USER",
			MustChangePassword: true,
		})
	}

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
