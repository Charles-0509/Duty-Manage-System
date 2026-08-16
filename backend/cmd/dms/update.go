package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const lastUpdateRefFile = ".dms-last-update"

type updateOptions struct {
	branch     string
	manageServ bool
	skipBuild  bool
}

func parseUpdateFlags(args []string, command string) (updateOptions, error) {
	options := updateOptions{manageServ: true}
	rest := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-branch":
			index++
			if index >= len(args) {
				return options, fmt.Errorf("-branch 需要一个分支名参数")
			}
			options.branch = args[index]
		case "-no-restart":
			options.manageServ = false
		case "-skip-build":
			options.skipBuild = true
		default:
			rest = append(rest, arg)
		}
	}
	if len(rest) > 1 {
		return options, fmt.Errorf("%s 不接受多个参数", command)
	}
	if len(rest) == 1 {
		if options.branch != "" {
			return options, fmt.Errorf("分支同时通过位置参数和 -branch 指定")
		}
		options.branch = rest[0]
	}
	return options, nil
}

func runUpdate(args []string) error {
	app, err := newAppContext()
	if err != nil {
		return err
	}
	options, err := parseUpdateFlags(args, "update")
	if err != nil {
		return err
	}
	if value, present := os.LookupEnv("UPDATE_MANAGE_SERVICE"); present && value == "0" {
		options.manageServ = false
	}
	if options.branch == "" {
		options.branch = os.Getenv("UPDATE_BRANCH")
	}
	if !commandExists("git") {
		return fmt.Errorf("缺少 git 命令，请先安装 git")
	}

	branch, err := resolveTargetBranch(app, options.branch)
	if err != nil {
		return err
	}

	fmt.Printf("更新目录: %s\n", app.root)
	fmt.Printf("目标分支: %s\n", branch)

	svc := newServiceController(app)
	wasActive := false
	if options.manageServ && svc.exists() && svc.active() {
		wasActive = true
		fmt.Printf("停止服务 %s\n", svc.describe())
		if err := svc.stop(); err != nil {
			return fmt.Errorf("停止服务失败: %w", err)
		}
	}

	restore := func(failure error) error {
		if !wasActive {
			return failure
		}
		fmt.Fprintf(os.Stderr, "更新失败，尝试恢复服务…\n")
		if err := svc.start(); err != nil {
			fmt.Fprintf(os.Stderr, "自动恢复服务失败，请手动检查: %v\n", err)
		}
		return failure
	}

	if err := gitRun(app.root, "fetch", "origin", "--prune"); err != nil {
		return restore(fmt.Errorf("git fetch 失败: %w", err))
	}
	if err := gitRun(app.root, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err != nil {
		return restore(fmt.Errorf("远端分支不存在: origin/%s", branch))
	}

	// 记录更新前的 HEAD 供 rollback 使用；在 git clean 之后落盘，避免被
	// 旧版本（.gitignore 尚未包含该文件）的 clean 误删。
	previousHead, err := gitOutput(app.root, "rev-parse", "HEAD")
	if err != nil {
		previousHead = ""
	}

	if err := gitRun(app.root, "reset", "--hard", "origin/"+branch); err != nil {
		return restore(fmt.Errorf("git reset 失败: %w", err))
	}
	if err := gitRun(app.root, "clean", "-fd"); err != nil {
		return restore(fmt.Errorf("git clean 失败: %w", err))
	}
	if head := strings.TrimSpace(previousHead); head != "" {
		if err := os.WriteFile(filepath.Join(app.root, lastUpdateRefFile), []byte(head+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "警告: 写入回退记录失败: %v\n", err)
		}
	}
	if runtime.GOOS != "windows" {
		for _, script := range []string{"build.sh", "run.sh", "clean.sh", "update.sh", "backup.sh"} {
			_ = os.Chmod(filepath.Join(app.root, script), 0o755)
		}
	}

	fmt.Printf("代码已更新到 origin/%s\n", branch)

	newHead, _ := gitOutput(app.root, "rev-parse", "--short", "HEAD")
	fmt.Printf("当前版本: %s\n", strings.TrimSpace(newHead))

	if options.skipBuild {
		fmt.Println("已按参数跳过构建")
	} else {
		fmt.Println("开始构建项目")
		if err := executeBuildScript(app); err != nil {
			return restore(fmt.Errorf("构建失败: %w", err))
		}
	}

	if wasActive {
		fmt.Printf("启动服务 %s\n", svc.describe())
		if err := svc.start(); err != nil {
			return fmt.Errorf("启动服务失败: %w", err)
		}
		fmt.Println("服务已重启")
	}

	fmt.Println("更新完成")
	return nil
}

func runBuild(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("build 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	return executeBuildScript(app)
}

func runRollback(args []string) error {
	skipBuild := false
	for _, arg := range args {
		if arg == "-skip-build" {
			skipBuild = true
			continue
		}
		return fmt.Errorf("rollback 不接受参数 %q", arg)
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(filepath.Join(app.root, lastUpdateRefFile))
	if err != nil {
		return fmt.Errorf("没有可回退的版本记录（%s 不存在），回退功能仅在执行过 dms update 后可用", lastUpdateRefFile)
	}
	commit := strings.TrimSpace(string(raw))
	if commit == "" {
		return fmt.Errorf("版本记录为空，无法回退")
	}
	if err := gitRun(app.root, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return fmt.Errorf("版本记录 %s 在本地仓库中不存在（可能已被清理）", commit)
	}

	short, _ := gitOutput(app.root, "rev-parse", "--short", commit)
	fmt.Printf("回退到上次更新前的版本: %s\n", strings.TrimSpace(short))

	svc := newServiceController(app)
	wasActive := svc.exists() && svc.active()
	if wasActive {
		fmt.Printf("停止服务 %s\n", svc.describe())
		if err := svc.stop(); err != nil {
			return fmt.Errorf("停止服务失败: %w", err)
		}
	}

	if err := gitRun(app.root, "reset", "--hard", commit); err != nil {
		if wasActive {
			_ = svc.start()
		}
		return fmt.Errorf("git reset 失败: %w", err)
	}
	if skipBuild {
		fmt.Println("已按参数跳过构建")
	} else if err := executeBuildScript(app); err != nil {
		if wasActive {
			_ = svc.start()
		}
		return fmt.Errorf("构建失败: %w", err)
	}
	if wasActive {
		if err := svc.start(); err != nil {
			return fmt.Errorf("启动服务失败: %w", err)
		}
	}
	fmt.Println("回退完成")
	return nil
}

func resolveTargetBranch(app *appContext, preferred string) (string, error) {
	if strings.TrimSpace(preferred) != "" {
		return strings.TrimSpace(preferred), nil
	}
	if current, err := gitOutput(app.root, "branch", "--show-current"); err == nil && strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current), nil
	}
	return "main", nil
}

func executeBuildScript(app *appContext) error {
	if runtime.GOOS == "windows" {
		if !commandExists("powershell") {
			return fmt.Errorf("缺少 powershell 命令")
		}
		return runCommand("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(app.root, "build.ps1"))
	}
	if !commandExists("bash") {
		return fmt.Errorf("缺少 bash 命令")
	}
	return runCommand("bash", filepath.Join(app.root, "build.sh"))
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
