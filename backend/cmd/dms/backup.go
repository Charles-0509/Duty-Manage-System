package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultBackupGitRepo   = "git@github.com:Charles-0509/DMS-backup.git"
	defaultBackupGitBranch = "main"
)

type backupOptions struct {
	outDir string
	noGit  bool
}

func runBackup(args []string) error {
	options := backupOptions{}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-out":
			index++
			if index >= len(args) {
				return fmt.Errorf("-out 需要目录参数")
			}
			options.outDir = args[index]
		case "-no-git":
			options.noGit = true
		default:
			return fmt.Errorf("backup 不接受参数 %q", args[index])
		}
	}

	app, err := newAppContext()
	if err != nil {
		return err
	}
	return performBackup(app, options)
}

func performBackup(app *appContext, options backupOptions) error {
	controlPath, err := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db"))
	if err != nil {
		return err
	}
	semesterDir, err := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters"))
	if err != nil {
		return err
	}
	templateDir, err := app.resolvePath(app.envValue("WORK_STUDY_TEMPLATE_DIR", "../data/work-study/templates"))
	if err != nil {
		return err
	}
	backupDir := expandHome(app.envValue("BACKUP_DIR", filepath.Join(homeDir(), "DMS-backup")))
	if options.outDir != "" {
		backupDir = expandHome(options.outDir)
	}

	if _, err := os.Stat(controlPath); err != nil {
		return fmt.Errorf("控制数据库不存在: %s", controlPath)
	}
	if info, err := os.Stat(semesterDir); err != nil || !info.IsDir() {
		return fmt.Errorf("学期数据库目录不存在: %s", semesterDir)
	}
	if info, err := os.Stat(templateDir); err != nil || !info.IsDir() {
		return fmt.Errorf("全局模板目录不存在: %s", templateDir)
	}

	semesterDBs, err := listSemesterDatabases(semesterDir)
	if err != nil {
		return err
	}
	if len(semesterDBs) == 0 {
		return fmt.Errorf("学期数据库目录 %s 中没有任何 .db 文件", semesterDir)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	snapshotDir := filepath.Join(backupDir, timestamp)
	latestDir := filepath.Join(backupDir, "latest")

	for _, dir := range []string{snapshotDir, filepath.Join(snapshotDir, "semesters"), filepath.Join(snapshotDir, "work-study", "templates"),
		filepath.Join(latestDir, "semesters"), filepath.Join(latestDir, "work-study", "templates")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// latest 反映最近一次快照，先清掉旧内容
	os.RemoveAll(filepath.Join(latestDir, "semesters"))
	os.RemoveAll(filepath.Join(latestDir, "work-study"))
	os.Remove(filepath.Join(latestDir, "control.db"))
	if err := os.MkdirAll(filepath.Join(latestDir, "semesters"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(latestDir, "work-study", "templates"), 0o755); err != nil {
		return err
	}

	if err := backupSQLite(controlPath, filepath.Join(snapshotDir, "control.db")); err != nil {
		return err
	}
	if err := backupSQLite(controlPath, filepath.Join(latestDir, "control.db")); err != nil {
		return err
	}
	for _, semesterDB := range semesterDBs {
		name := filepath.Base(semesterDB)
		if err := backupSQLite(semesterDB, filepath.Join(snapshotDir, "semesters", name)); err != nil {
			return err
		}
		if err := backupSQLite(semesterDB, filepath.Join(latestDir, "semesters", name)); err != nil {
			return err
		}
	}
	if err := copyDir(templateDir, filepath.Join(snapshotDir, "work-study", "templates")); err != nil {
		return fmt.Errorf("复制模板失败: %w", err)
	}
	if err := copyDir(templateDir, filepath.Join(latestDir, "work-study", "templates")); err != nil {
		return fmt.Errorf("复制模板失败: %w", err)
	}

	fmt.Println("备份完成:")
	fmt.Printf("  快照目录: %s\n", snapshotDir)
	fmt.Printf("  最新备份: %s\n", latestDir)
	fmt.Printf("  学期库数: %d\n", len(semesterDBs))

	if options.noGit {
		fmt.Println("已按参数跳过 git 推送")
		return nil
	}
	return syncBackupToGit(app, backupDir, timestamp)
}

// backupSQLite creates a consistent copy of a live SQLite database using
// VACUUM INTO, which takes a read transaction and produces a compact image
// (equivalent to the Python sqlite3 backup API used by the old script).
func backupSQLite(source, target string) error {
	if _, err := os.Stat(target); err == nil {
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("清理目标 %s 失败: %w", target, err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(source, "?", "%3F")+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	escaped := strings.ReplaceAll(target, "'", "''")
	if _, err := db.Exec("VACUUM INTO '" + escaped + "'"); err != nil {
		return fmt.Errorf("备份 %s 失败: %w", source, err)
	}
	return nil
}

func listSemesterDatabases(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".db") && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(names)
	return names, nil
}

func syncBackupToGit(app *appContext, backupDir, timestamp string) error {
	if value, present := app.env["BACKUP_GIT_ENABLED"]; present && !truthy(value) {
		fmt.Println("备份 git 推送已禁用（BACKUP_GIT_ENABLED）")
		return nil
	}
	if !commandExists("git") {
		return fmt.Errorf("缺少 git 命令，无法推送备份")
	}

	repo := app.envValue("BACKUP_GIT_REPO", defaultBackupGitRepo)
	branch := app.envValue("BACKUP_GIT_BRANCH", defaultBackupGitBranch)
	sshKey := expandHome(app.envValue("BACKUP_SSH_KEY", filepath.Join(homeDir(), ".ssh", "id_ed25519")))
	authorName := app.envValue("BACKUP_GIT_AUTHOR_NAME", "DMS Backup")
	authorEmail := app.envValue("BACKUP_GIT_AUTHOR_EMAIL", "dms-backup@localhost")

	if repo == "" || branch == "" {
		return fmt.Errorf("BACKUP_GIT_REPO / BACKUP_GIT_BRANCH 配置为空")
	}

	gitEnv := append(os.Environ(),
		"GIT_SSH_COMMAND=ssh -i "+sshKey+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new",
	)

	git := func(args ...string) error {
		cmd := gitCommand(backupDir, args...)
		cmd.Env = gitEnv
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	silentGit := func(args ...string) bool {
		cmd := gitCommand(backupDir, args...)
		cmd.Env = gitEnv
		return cmd.Run() == nil
	}

	if info, err := os.Stat(filepath.Join(backupDir, ".git")); err != nil || !info.IsDir() {
		if err := git("init", "-b", branch); err != nil {
			return fmt.Errorf("git init 失败: %w", err)
		}
	}
	_ = git("remote", "set-url", "origin", repo)
	if !silentGit("remote", "get-url", "origin") {
		if err := git("remote", "add", "origin", repo); err != nil {
			return err
		}
	}
	_ = git("config", "user.name", authorName)
	_ = git("config", "user.email", authorEmail)

	if err := git("add", "."); err != nil {
		return err
	}
	if silentGit("diff", "--cached", "--quiet") {
		fmt.Println("备份内容无变化，跳过提交")
		return nil
	}
	if err := git("commit", "-m", "DMS backup "+timestamp); err != nil {
		return err
	}
	if silentGit("ls-remote", "--exit-code", "--heads", "origin", branch) {
		if err := git("pull", "--rebase", "origin", branch); err != nil {
			return err
		}
	}
	if err := git("push", "-u", "origin", branch); err != nil {
		return err
	}
	fmt.Println("备份已推送到远端仓库:")
	fmt.Printf("  %s (%s)\n", repo, branch)
	return nil
}

func runRestore(args []string) error {
	snapshot := ""
	assumeYes := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-y":
			assumeYes = true
		default:
			if snapshot != "" {
				return fmt.Errorf("restore 只接受一个快照目录参数")
			}
			snapshot = args[index]
		}
	}
	if snapshot == "" {
		return fmt.Errorf("用法: dms restore <快照目录> [-y]")
	}
	snapshot = expandHome(snapshot)
	if info, err := os.Stat(snapshot); err != nil || !info.IsDir() {
		return fmt.Errorf("快照目录不存在: %s", snapshot)
	}
	controlBackup := filepath.Join(snapshot, "control.db")
	if _, err := os.Stat(controlBackup); err != nil {
		return fmt.Errorf("快照缺少 control.db: %s", controlBackup)
	}

	app, err := newAppContext()
	if err != nil {
		return err
	}

	fmt.Printf("将从快照恢复数据:\n  %s\n", snapshot)
	fmt.Println("恢复会覆盖当前控制库、全部学期库和全局模板目录。")
	if !assumeYes {
		fmt.Print("确认继续？(yes/no): ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(answer), "yes") && !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("已取消恢复")
			return nil
		}
	}

	// 恢复前先把当前数据再备份一份，防止误恢复
	safetyDir := filepath.Join(filepath.Dir(snapshot), "pre-restore-"+time.Now().Format("2006-01-02_15-04-05"))
	fmt.Printf("恢复前自动备份当前数据到: %s\n", safetyDir)
	currentControl, err := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db"))
	if err != nil {
		return err
	}
	currentSemesterDir, err := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters"))
	if err != nil {
		return err
	}
	currentTemplateDir, err := app.resolvePath(app.envValue("WORK_STUDY_TEMPLATE_DIR", "../data/work-study/templates"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(safetyDir, "semesters"), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(currentControl); err == nil {
		if err := backupSQLite(currentControl, filepath.Join(safetyDir, "control.db")); err != nil {
			return err
		}
	}
	if databases, err := listSemesterDatabases(currentSemesterDir); err == nil {
		for _, database := range databases {
			if err := backupSQLite(database, filepath.Join(safetyDir, "semesters", filepath.Base(database))); err != nil {
				return err
			}
		}
	}
	if info, err := os.Stat(currentTemplateDir); err == nil && info.IsDir() {
		if err := copyDir(currentTemplateDir, filepath.Join(safetyDir, "work-study", "templates")); err != nil {
			return err
		}
	}

	svc := newServiceController(app)
	wasActive := svc.active()
	if wasActive {
		fmt.Println("停止服务…")
		if err := svc.stop(); err != nil {
			return fmt.Errorf("停止服务失败: %w", err)
		}
	}

	restoreErr := func() error {
		if err := copyFile(controlBackup, currentControl); err != nil {
			return fmt.Errorf("恢复控制库失败: %w", err)
		}
		// 清空现有学期库后从快照复制
		if err := os.RemoveAll(currentSemesterDir); err != nil {
			return err
		}
		snapshotSemesters := filepath.Join(snapshot, "semesters")
		if info, err := os.Stat(snapshotSemesters); err == nil && info.IsDir() {
			if err := copyDir(snapshotSemesters, currentSemesterDir); err != nil {
				return fmt.Errorf("恢复学期库失败: %w", err)
			}
		}
		snapshotTemplates := filepath.Join(snapshot, "work-study", "templates")
		if info, err := os.Stat(snapshotTemplates); err == nil && info.IsDir() {
			if err := os.RemoveAll(currentTemplateDir); err != nil {
				return err
			}
			if err := copyDir(snapshotTemplates, currentTemplateDir); err != nil {
				return fmt.Errorf("恢复模板失败: %w", err)
			}
		}
		return nil
	}()

	if restoreErr != nil {
		fmt.Fprintf(os.Stderr, "恢复中断: %v\n", restoreErr)
	}
	if wasActive {
		fmt.Println("重新启动服务…")
		if err := svc.start(); err != nil {
			fmt.Fprintf(os.Stderr, "启动服务失败: %v\n", err)
		}
	}
	if restoreErr != nil {
		return restoreErr
	}
	fmt.Println("恢复完成。当前数据已保留在:", safetyDir)
	return nil
}

func gitCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}
