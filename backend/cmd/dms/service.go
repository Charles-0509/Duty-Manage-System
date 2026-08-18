package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// serviceController delegates production service management to systemd.
type serviceController struct {
	app      *appContext
	unitName string
}

func newServiceController(app *appContext) *serviceController {
	return &serviceController{
		app:      app,
		unitName: "dms.service",
	}
}

func (s *serviceController) systemdAvailable() bool {
	if runtime.GOOS != "linux" || !commandExists("systemctl") {
		return false
	}
	cmd := exec.Command("systemctl", "list-unit-files", s.unitName, "--no-legend")
	return cmd.Run() == nil
}

func (s *serviceController) describe() string {
	if s.systemdAvailable() {
		return s.unitName
	}
	return "systemd 未安装"
}

func (s *serviceController) systemctl(args ...string) error {
	full := append([]string{"systemctl"}, args...)
	if os.Geteuid() != 0 && commandExists("sudo") {
		full = append([]string{"sudo"}, full...)
	}
	return runCommand(full[0], full[1:]...)
}

func (s *serviceController) active() bool {
	return s.systemdAvailable() && exec.Command("systemctl", "is-active", "--quiet", s.unitName).Run() == nil
}

func (s *serviceController) start() error {
	if !s.systemdAvailable() {
		return fmt.Errorf("未安装 %s systemd unit", s.unitName)
	}
	if err := s.systemctl("start", s.unitName); err != nil {
		return err
	}
	return s.waitHealthy(20 * time.Second)
}

func (s *serviceController) stop() error {
	if !s.systemdAvailable() {
		return fmt.Errorf("未安装 %s systemd unit", s.unitName)
	}
	return s.systemctl("stop", s.unitName)
}

func (s *serviceController) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.app.healthCheck() == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("服务在 %s 内未通过健康检查，请查看日志（dms logs）", timeout)
}

func (a *appContext) healthCheck() error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/health", a.appPort()))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("健康检查返回 %d", response.StatusCode)
	}
	return nil
}

func runStart(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("start 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	svc := newServiceController(app)
	if err := svc.start(); err != nil {
		return err
	}
	fmt.Println("启动完成")
	return nil
}

func runStop(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("stop 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	return newServiceController(app).stop()
}

func runRestart(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("restart 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	svc := newServiceController(app)
	if svc.active() {
		if err := svc.stop(); err != nil {
			return err
		}
	}
	if err := svc.start(); err != nil {
		return err
	}
	fmt.Println("重启完成")
	return nil
}

func runStatus(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("status 不接受参数")
	}
	app, err := newAppContext()
	if err != nil {
		return err
	}
	svc := newServiceController(app)

	fmt.Printf("安装目录: %s\n", app.root)
	fmt.Printf("运行模式: %s\n", svc.describe())
	fmt.Printf("服务状态: ")
	if svc.active() {
		fmt.Println("运行中")
	} else {
		fmt.Println("未运行")
	}

	if healthErr := app.healthCheck(); healthErr == nil {
		fmt.Printf("健康检查: 正常 (http://127.0.0.1:%s/health)\n", app.appPort())
	} else {
		fmt.Printf("健康检查: 不可达 (%v)\n", healthErr)
	}

	if head, err := gitOutput(app.root, "log", "-1", "--format=%h %cd", "--date=format:%Y-%m-%d %H:%M"); err == nil {
		fmt.Printf("代码版本: %s\n", strings.TrimSpace(head))
	}

	if controlPath, err := app.resolvePath(app.envValue("CONTROL_DATABASE_PATH", "../data/control.db")); err == nil {
		if info, statErr := os.Stat(controlPath); statErr == nil {
			fmt.Printf("控制数据库: %s (%s)\n", controlPath, formatBytes(info.Size()))
		} else {
			fmt.Printf("控制数据库: 未找到 %s\n", controlPath)
		}
	}
	if semesterDir, err := app.resolvePath(app.envValue("SEMESTER_DATABASE_DIR", "../data/semesters")); err == nil {
		databases, _ := listSemesterDatabases(semesterDir)
		fmt.Printf("学期数据库: %s (%d 个)\n", semesterDir, len(databases))
	}
	return nil
}

func runLogs(args []string) error {
	lines := "100"
	follow := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-n":
			index++
			if index >= len(args) {
				return fmt.Errorf("-n 需要行数参数")
			}
			lines = args[index]
		case "-f":
			follow = true
		default:
			return fmt.Errorf("logs 不接受参数 %q", arg)
		}
	}

	app, err := newAppContext()
	if err != nil {
		return err
	}
	svc := newServiceController(app)
	if !svc.systemdAvailable() {
		return fmt.Errorf("未安装 %s systemd unit", svc.unitName)
	}
	command := []string{"journalctl", "-u", svc.unitName, "-n", lines, "--no-pager"}
	if follow {
		command = append(command, "-f")
	}
	return runCommand(command[0], command[1:]...)
}
