package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// Build metadata, injected via -ldflags at build time when available.
var (
	buildCommit    = "unknown"
	buildDate      = "unknown"
	buildGoVersion = "unknown"
)

func runVersion(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("version 不接受参数")
	}
	fmt.Printf("dms CLI: %s (%s, go %s)\n", buildCommit, buildDate, buildGoVersion)

	app, err := newAppContext()
	if err != nil {
		fmt.Println("代码版本: 未知（未找到安装目录）")
		return nil
	}
	if branch, err := gitOutput(app.root, "branch", "--show-current"); err == nil && strings.TrimSpace(branch) != "" {
		fmt.Printf("代码分支: %s\n", strings.TrimSpace(branch))
	}
	if head, err := gitOutput(app.root, "log", "-1", "--format=%h %cd", "--date=format:%Y-%m-%d %H:%M"); err == nil {
		fmt.Printf("代码版本: %s\n", strings.TrimSpace(head))
	} else {
		fmt.Println("代码版本: 非 git 仓库或缺少 git")
	}
	if describe, err := gitOutput(app.root, "describe", "--tags", "--always", "--dirty"); err == nil {
		fmt.Printf("版本描述: %s\n", strings.TrimSpace(describe))
	}
	if info, err := os.Stat(app.serverBinaryPath()); err == nil {
		fmt.Printf("服务二进制: %s (构建于 %s)\n", app.serverBinaryPath(), info.ModTime().Format("2006-01-02 15:04"))
	} else {
		fmt.Println("服务二进制: 未构建（执行 dms build）")
	}
	return nil
}

func runEnv(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("env 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}

	controlPath, _ := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db"))
	semesterDir, _ := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters"))
	templateDir, _ := app.resolvePath(app.envValue("WORK_STUDY_TEMPLATE_DIR", "../data/work-study/templates"))
	backupDir := expandHome(app.envValue("BACKUP_DIR", filepath.Join(homeDir(), "DMS-backup")))

	secretState := "未设置"
	if value := app.envValue("JWT_SECRET", ""); value != "" {
		if value == "please-change-me" {
			secretState = "仍是默认值（危险！）"
		} else {
			secretState = fmt.Sprintf("已设置（%d 字符）", len(value))
		}
	}

	lines := []struct{ label, value string }{
		{"安装目录", app.root},
		{"服务端口", app.appPort()},
		{"访问令牌时效", app.envValue("ACCESS_TOKEN_TTL", "7200") + " 秒"},
		{"控制数据库", controlPath},
		{"学期数据库目录", semesterDir},
		{"全局模板目录", templateDir},
		{"备份目录", backupDir},
		{"备份 git 仓库", app.envValue("BACKUP_GIT_REPO", defaultBackupGitRepo)},
		{"JWT_SECRET", secretState},
	}
	for _, line := range lines {
		fmt.Printf("%-16s %s\n", line.label+":", line.value)
	}
	fmt.Println("\n（如需修改，请编辑 backend/.env；系统环境变量优先级更高）")
	return nil
}

type checkResult struct {
	label   string
	status  int // 0 ok, 1 warn, 2 fail
	message string
}

func runDoctor(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("doctor 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	var results []checkResult
	add := func(status int, label, message string) {
		results = append(results, checkResult{label, status, message})
	}

	// 环境文件
	if info, err := os.Stat(filepath.Join(app.backendDir, ".env")); err == nil && !info.IsDir() {
		add(0, "环境配置", "backend/.env 存在")
	} else {
		add(2, "环境配置", "backend/.env 不存在")
	}
	if secret := app.envValue("JWT_SECRET", "please-change-me"); secret == "please-change-me" {
		add(2, "JWT 密钥", "仍是默认值 please-change-me，生产环境必须修改")
	} else {
		add(0, "JWT 密钥", "已自定义")
	}

	// 依赖工具
	for _, tool := range []string{"git", "node", "npm", "go"} {
		if commandExists(tool) {
			if version, err := exec.Command(tool, "--version").Output(); err == nil {
				add(0, "依赖 "+tool, strings.TrimSpace(string(version)))
			} else {
				add(0, "依赖 "+tool, "可用")
			}
		} else {
			status := 2
			message := "缺少（更新和构建需要）"
			if tool == "go" {
				status = 1
				message = "缺少（仅当需要现场构建时才需要）"
			}
			add(status, "依赖 "+tool, message)
		}
	}

	// 服务与健康
	svc := newServiceController(app)
	if svc.active() {
		if err := app.healthCheck(); err == nil {
			add(0, "服务健康", fmt.Sprintf("运行中，/health 正常（端口 %s，%s）", app.appPort(), svc.describe()))
		} else {
			add(1, "服务健康", "服务进程在运行但健康检查失败，请查看日志")
		}
	} else {
		add(1, "服务健康", "服务未运行（dms start 可启动）")
	}

	// 数据库
	if controlPath, err := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db")); err == nil {
		if _, statErr := os.Stat(controlPath); statErr != nil {
			add(2, "控制数据库", "不存在: "+controlPath)
		} else if ok, message := sqliteQuickCheck(controlPath); ok {
			add(0, "控制数据库", message)
		} else {
			add(2, "控制数据库", message)
		}
	}
	if semesterDir, err := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters")); err == nil {
		databases, listErr := listSemesterDatabases(semesterDir)
		if listErr != nil {
			add(2, "学期数据库", "目录不可读: "+semesterDir)
		} else if len(databases) == 0 {
			add(1, "学期数据库", "目录为空（首次部署正常）")
		} else {
			broken := 0
			for _, database := range databases {
				if ok, _ := sqliteQuickCheck(database); !ok {
					broken++
				}
			}
			if broken == 0 {
				add(0, "学期数据库", fmt.Sprintf("%d 个，完整性全部通过", len(databases)))
			} else {
				add(2, "学期数据库", fmt.Sprintf("%d 个中 %d 个完整性检查失败", len(databases), broken))
			}
		}
	}

	// 备份目录与磁盘
	backupDir := expandHome(app.envValue("BACKUP_DIR", filepath.Join(homeDir(), "DMS-backup")))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		add(2, "备份目录", "无法创建或写入: "+backupDir)
	} else {
		snapshots, _ := listBackupSnapshots(backupDir)
		lastNote := "尚无快照"
		if len(snapshots) > 0 {
			lastNote = fmt.Sprintf("共 %d 个快照，最近: %s", len(snapshots), snapshots[len(snapshots)-1])
		}
		add(0, "备份目录", backupDir+"（"+lastNote+"）")
	}

	// 工作区状态
	if status, err := gitOutput(app.root, "status", "--porcelain"); err == nil {
		if strings.TrimSpace(status) == "" {
			add(0, "代码工作区", "干净")
		} else {
			lines := strings.Split(strings.TrimSpace(status), "\n")
			add(1, "代码工作区", fmt.Sprintf("有 %d 处本地改动（更新前会被覆盖）", len(lines)))
		}
	}

	failed := 0
	for _, result := range results {
		icon := "✅"
		switch result.status {
		case 1:
			icon = "⚠️ "
		case 2:
			icon = "❌"
			failed++
		}
		fmt.Printf("%s %-10s %s\n", icon, result.label, result.message)
	}
	fmt.Println()
	if failed > 0 {
		fmt.Printf("体检结果: %d 项需要立即处理\n", failed)
		return fmt.Errorf("存在 %d 个严重问题", failed)
	}
	fmt.Println("体检结果: 全部正常")
	return nil
}

func sqliteQuickCheck(path string) (bool, string) {
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(path, "?", "%3F")+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return false, "打开失败: " + path
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return false, "检查失败: " + err.Error()
	}
	if !strings.EqualFold(result, "ok") {
		return false, "完整性异常: " + result
	}
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&count)
	return true, fmt.Sprintf("%s（完整性 ok，账户 %d 个）", path, count)
}

func listBackupSnapshots(backupDir string) ([]string, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "20") && !strings.HasPrefix(entry.Name(), "latest") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
