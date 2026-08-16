package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// appContext carries the resolved installation layout and the effective
// configuration for one CLI invocation.
type appContext struct {
	root       string
	backendDir string
	env        map[string]string
}

// findRoot locates the DMS installation directory: DMS_HOME wins, then the
// nearest ancestor of the working directory (and of the binary itself) that
// looks like the repository (backend/go.mod present).
func findRoot() (string, error) {
	if home := strings.TrimSpace(os.Getenv("DMS_HOME")); home != "" {
		if isRepoRoot(home) {
			return filepath.Abs(home)
		}
		return "", fmt.Errorf("DMS_HOME=%s 不是有效的 DMS 安装目录（缺少 backend/go.mod）", home)
	}

	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}

	for _, start := range candidates {
		dir := start
		for {
			if isRepoRoot(dir) {
				return filepath.Abs(dir)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("未找到 DMS 安装目录：请在项目目录内运行，或设置 DMS_HOME")
}

func isRepoRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "backend", "go.mod"))
	return err == nil && !info.IsDir()
}

func newAppContext() (*appContext, error) {
	root, err := findRoot()
	if err != nil {
		return nil, err
	}
	backendDir := filepath.Join(root, "backend")

	// OS environment takes precedence over backend/.env so operators can
	// override single values without editing the file.
	env := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			env[key] = value
		}
	}
	if fileValues, err := readEnvFile(filepath.Join(backendDir, ".env")); err == nil {
		for key, value := range fileValues {
			if _, overridden := os.LookupEnv(key); !overridden {
				env[key] = value
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 backend/.env 失败: %w", err)
	}

	return &appContext{root: root, backendDir: backendDir, env: env}, nil
}

// envValue returns the effective value for key, falling back to the given
// default.
func (a *appContext) envValue(key, fallback string) string {
	if value := strings.TrimSpace(a.env[key]); value != "" {
		return value
	}
	return fallback
}

// resolvePath expands ~ and resolves repository-relative paths against the
// backend directory, matching how the server resolves backend/.env paths.
func (a *appContext) resolvePath(raw string) (string, error) {
	value := expandHome(strings.TrimSpace(raw))
	if value == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	return filepath.Abs(filepath.Join(a.backendDir, value))
}

func expandHome(value string) string {
	if value == "~" {
		return homeDir()
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		return filepath.Join(homeDir(), value[2:])
	}
	return value
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return os.Getenv("HOME")
}

func readEnvFile(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			values[key] = value
		}
	}
	return values, nil
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// runCommand executes a child process, streaming output, and returns an error
// containing the command line on failure.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// serverBinaryPath returns the built server binary path for this platform.
func (a *appContext) serverBinaryPath() string {
	name := "personnel-management"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(a.root, name)
}

func (a *appContext) dmsBinaryName() string {
	if runtime.GOOS == "windows" {
		return "dms.exe"
	}
	return "dms"
}

func (a *appContext) appPort() string {
	return a.envValue("APP_PORT", "3000")
}

func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyDir recursively copies src into dst (dst is created).
func copyDir(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(path, destination)
	})
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, label := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, label)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}
