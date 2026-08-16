package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// serviceController manages the DMS service. When a systemd unit exists the
// CLI delegates to systemctl; otherwise it manages a directly-spawned server
// process tracked by a pid file.
type serviceController struct {
	app         *appContext
	unitName    string
	forceDirect bool
}

func newServiceController(app *appContext) *serviceController {
	return &serviceController{
		app:      app,
		unitName: app.envValue("UPDATE_SERVICE_NAME", "dms.service"),
	}
}

func (s *serviceController) systemdAvailable() bool {
	if s.forceDirect || runtime.GOOS != "linux" || !commandExists("systemctl") {
		return false
	}
	cmd := exec.Command("systemctl", "list-unit-files", s.unitName, "--no-legend")
	return cmd.Run() == nil
}

func (s *serviceController) exists() bool {
	return s.systemdAvailable()
}

func (s *serviceController) describe() string {
	if s.systemdAvailable() {
		return s.unitName
	}
	return "直接进程模式"
}

func (s *serviceController) systemctl(args ...string) error {
	full := append([]string{"systemctl"}, args...)
	if os.Geteuid() != 0 && commandExists("sudo") {
		full = append([]string{"sudo"}, full...)
	}
	return runCommand(full[0], full[1:]...)
}

func (s *serviceController) active() bool {
	if s.systemdAvailable() {
		cmd := exec.Command("systemctl", "is-active", "--quiet", s.unitName)
		return cmd.Run() == nil
	}
	pid, err := s.readPid()
	return err == nil && processAlive(pid)
}

func (s *serviceController) start() error {
	if s.systemdAvailable() {
		return s.systemctl("start", s.unitName)
	}
	return s.startDirect()
}

func (s *serviceController) stop() error {
	if s.systemdAvailable() {
		return s.systemctl("stop", s.unitName)
	}
	return s.stopDirect()
}

func (s *serviceController) pidFile() string {
	return filepath.Join(s.app.root, ".dms.pid")
}

func (s *serviceController) logFile() string {
	return filepath.Join(s.app.root, "dms.log")
}

func (s *serviceController) readPid() (int, error) {
	content, err := os.ReadFile(s.pidFile())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(content)))
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		// os.Process.Signal 不能用于 Windows 探活（仅支持 Kill），改用 tasklist。
		out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), strconv.Itoa(pid))
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (s *serviceController) startDirect() error {
	if pid, err := s.readPid(); err == nil && processAlive(pid) {
		return fmt.Errorf("服务已在运行（PID %d），如需重启请先执行 dms stop", pid)
	}
	binary := s.app.serverBinaryPath()
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		return fmt.Errorf("未找到服务二进制 %s，请先执行 dms build", binary)
	}

	logFile, err := os.OpenFile(s.logFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer logFile.Close()

	env := os.Environ()
	// The server also reads backend/.env itself; forward the effective values
	// so OS-level overrides (used by tests and ops) reach the process.
	forward := []string{"APP_PORT", "CONTROL_DATABASE_PATH", "SEMESTER_DATABASE_DIR", "DATABASE_PATH", "PRIVATE_MEMBERS_PATH", "JWT_SECRET", "DEFAULT_ADMIN_PASSWORD", "FIRST_MONDAY", "WORK_STUDY_TEMPLATE_DIR", "ACCESS_TOKEN_TTL", "GIN_MODE"}
	for _, key := range forward {
		if value := strings.TrimSpace(s.app.env[key]); value != "" {
			env = append(env, key+"="+value)
		}
	}
	if runtime.GOOS != "windows" {
		env = append(env, "GIN_MODE=release")
	}

	cmd := exec.Command(binary)
	cmd.Dir = s.app.backendDir
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动服务进程失败: %w", err)
	}

	if err := os.WriteFile(s.pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return fmt.Errorf("写入 pid 文件失败: %w", err)
	}

	fmt.Printf("服务已启动（PID %d），日志: %s\n", cmd.Process.Pid, s.logFile())
	return s.waitHealthy(20 * time.Second)
}

func (s *serviceController) stopDirect() error {
	pid, err := s.readPid()
	if err != nil {
		os.Remove(s.pidFile())
		return fmt.Errorf("没有运行中的服务记录（%s 不存在）", s.pidFile())
	}
	if !processAlive(pid) {
		os.Remove(s.pidFile())
		return fmt.Errorf("记录的进程 %d 已不在运行", pid)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Windows cannot kill a process tree via Signal; use taskkill.
		if err := runCommand("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)); err != nil {
			return err
		}
	} else {
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return err
		}
		deadline := time.Now().Add(10 * time.Second)
		for processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(300 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = proc.Signal(syscall.SIGKILL)
		}
	}
	os.Remove(s.pidFile())
	fmt.Printf("服务进程 %d 已停止\n", pid)
	return nil
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
	if svc.systemdAvailable() {
		_ = svc.waitHealthy(20 * time.Second)
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
	if svc.systemdAvailable() {
		_ = svc.waitHealthy(20 * time.Second)
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
		count := countFiles(semesterDir, ".db")
		fmt.Printf("学期数据库: %s (%d 个)\n", semesterDir, count)
	}
	return nil
}

func countFiles(dir, extension string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), extension) {
			count++
		}
	}
	return count
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
	if svc.systemdAvailable() {
		args := []string{"journalctl", "-u", svc.unitName, "-n", lines, "--no-pager"}
		if follow {
			args = append(args, "-f")
		}
		return runCommand(args[0], args[1:]...)
	}

	logPath := svc.logFile()
	if _, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("日志文件不存在: %s（服务可能从未以直接进程模式启动）", logPath)
	}
	if follow {
		if runtime.GOOS == "windows" {
			fmt.Println("提示: Windows 下暂不支持 -f 跟踪，仅输出最近日志")
		} else {
			return runCommand("tail", "-n", lines, "-f", logPath)
		}
	}
	return tailFile(logPath, lines)
}

func tailFile(path string, lineCount string) error {
	count, err := strconv.Atoi(lineCount)
	if err != nil || count <= 0 {
		count = 100
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}
