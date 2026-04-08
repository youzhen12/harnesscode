package backend

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Backend describes an AI execution backend (opencode, claude, etc.).
//
// 目前先只支持通过外部 CLI 调用，后续可以在此接口下挂接 HTTP API 实现。
type Backend interface {
	// Name returns backend identifier, e.g. "opencode" or "claude".
	Name() string

	// CommandPath returns the executable path.
	CommandPath() (string, error)

	// BuildRunCmd builds the full command for running a specific agent.
	BuildRunCmd(agent string, prompt string, model string) ([]string, error)

	// IsInstalled reports whether the backend CLI seems to be available.
	IsInstalled() bool
}

// -----------------------------------------------------------------------------
// OpenCode backend
// -----------------------------------------------------------------------------

type openCodeBackend struct{}

func (o *openCodeBackend) Name() string { return "opencode" }

func (o *openCodeBackend) CommandPath() (string, error) {
	if p := os.Getenv("OPENCODE_PATH"); p != "" {
		return p, nil
	}

	if p, err := exec.LookPath("opencode"); err == nil {
		return p, nil
	}

	home, _ := os.UserHomeDir()
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(home, "AppData", "Roaming", "npm", "opencode.cmd"),
			filepath.Join(home, "AppData", "Local", "npm-global", "opencode.cmd"),
		}
	} else {
		candidates = []string{
			filepath.Join(home, ".npm-global", "bin", "opencode"),
			"/usr/local/bin/opencode",
			"/opt/homebrew/bin/opencode",
			filepath.Join(home, ".local", "bin", "opencode"),
		}
	}

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "opencode", errors.New("opencode executable not found; using bare name")
}

func (o *openCodeBackend) BuildRunCmd(agent string, prompt string, model string) ([]string, error) {
	path, _ := o.CommandPath()
	cmd := []string{path, "run", "--agent", "harnesscode-" + agent}
	if model != "" {
		cmd = append(cmd, "--model", model)
	}
	cmd = append(cmd, prompt)
	return cmd, nil
}

func (o *openCodeBackend) IsInstalled() bool {
	if p := os.Getenv("OPENCODE_PATH"); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
	}
	if _, err := exec.LookPath("opencode"); err == nil {
		return true
	}
	path, err := o.CommandPath()
	if err != nil {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// -----------------------------------------------------------------------------
// Claude backend
// -----------------------------------------------------------------------------

type claudeBackend struct{}

func (c *claudeBackend) Name() string { return "claude" }

func (c *claudeBackend) CommandPath() (string, error) {
	if p := os.Getenv("CLAUDE_PATH"); p != "" {
		return p, nil
	}

	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}

	home, _ := os.UserHomeDir()
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(home, "AppData", "Local", "Programs", "claude-code", "claude.exe"),
			filepath.Join(home, "AppData", "Roaming", "npm", "claude.cmd"),
		}
	} else if runtime.GOOS == "darwin" {
		candidates = []string{
			"/usr/local/bin/claude",
			"/opt/homebrew/bin/claude",
			filepath.Join(home, ".local", "bin", "claude"),
		}
	} else {
		candidates = []string{
			"/usr/local/bin/claude",
			filepath.Join(home, ".local", "bin", "claude"),
		}
	}

	for _, cnd := range candidates {
		if fi, err := os.Stat(cnd); err == nil && !fi.IsDir() {
			return cnd, nil
		}
	}
	return "claude", errors.New("claude executable not found; using bare name")
}

func (c *claudeBackend) BuildRunCmd(agent string, prompt string, model string) ([]string, error) {
	path, _ := c.CommandPath()
	agentName := "harnesscode-" + agent
	cmd := []string{path, "--agent", agentName, "-p", prompt,
		"--dangerously-skip-permissions",
		"--permission-mode", "bypassPermissions",
		"--no-session-persistence",
		"--verbose",
		"--output-format", "stream-json",
	}
	if model != "" {
		cmd = append(cmd, "--model", model)
	}
	return cmd, nil
}

func (c *claudeBackend) IsInstalled() bool {
	if p := os.Getenv("CLAUDE_PATH"); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
	}
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}
	path, err := c.CommandPath()
	if err != nil {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// -----------------------------------------------------------------------------
// Factory
// -----------------------------------------------------------------------------

// DetectBackend 尝试检测可用后端，优先从环境变量 HARNESSCODE_BACKEND。
// 如果检测不到，则默认 opencode。
func DetectBackend() Backend {
	name := os.Getenv("HARNESSCODE_BACKEND")
	switch name {
	case "claude":
		return &claudeBackend{}
	default:
		// 如果用户声明 claude 但未安装，将在运行时暴露错误。
		return &openCodeBackend{}
	}
}

// GetBackend 根据名称获取后端实例；空字符串表示自动检测。
func GetBackend(name string) Backend {
	if name == "" {
		return DetectBackend()
	}
	switch name {
	case "opencode":
		return &openCodeBackend{}
	case "claude":
		return &claudeBackend{}
	default:
		return &openCodeBackend{}
	}
}
