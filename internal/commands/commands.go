package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"harnesscode-go/internal/backend"
	"harnesscode-go/internal/installer"
	"harnesscode-go/internal/loop"
	"harnesscode-go/internal/metrics"
	"harnesscode-go/internal/project"
)

// Init initializes project configuration.
func Init() error {
	paths, err := project.DetectPaths("")
	if err != nil {
		return err
	}

	if err := project.EnsureHarnessDir(paths); err != nil {
		return err
	}

	projectID, err := project.GetOrCreateProjectID(paths)
	if err != nil {
		return err
	}

	// 简化版：优先读取已有配置，否则根据环境变量/默认值生成。
	cfg, err := project.LoadConfig(paths)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg == nil {
		cfg = &project.Config{}
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID = projectID
	}
	if cfg.Backend == "" {
		// env 优先。
		if b := strings.ToLower(os.Getenv("HARNESSCODE_BACKEND")); b == "claude" || b == "opencode" {
			cfg.Backend = b
		} else {
			cfg.Backend = backend.DetectBackend().Name()
		}
	}
	if cfg.AutoCommit == 0 {
		cfg.AutoCommit = 1
	}

	if err := project.SaveConfig(paths, cfg); err != nil {
		return err
	}

	// Install or update AI backend agents (best-effort).
	if err := installer.EnsureAgentsInstalled(cfg.Backend); err != nil {
		fmt.Println("[hc-go] Warning: failed to install agents:", err)
	}

	// 创建 input 目录结构（prd / techspec）。
	inputPrd := filepath.Join(paths.Root, "input", "prd")
	inputTech := filepath.Join(paths.Root, "input", "techspec")
	_ = os.MkdirAll(inputPrd, 0o755)
	_ = os.MkdirAll(inputTech, 0o755)

	// 简单更新 .gitignore，避免重复添加。
	if err := ensureGitignore(paths.Root); err != nil {
		return err
	}

	fmt.Println("[hc-go] Project initialized")
	fmt.Println("  Project ID:", cfg.ProjectID)
	fmt.Println("  Backend:", cfg.Backend)
	fmt.Println("  Auto-commit:", cfg.AutoCommit)
	fmt.Println("  Config:", paths.ConfigPath)
	return nil
}

// Start starts the main development loop.
func Start() error {
	return loop.Run()
}

// Status prints project status and metrics.
func Status() error {
	paths, err := project.DetectPaths("")
	if err != nil {
		return err
	}

	cfg, err := project.LoadConfig(paths)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("project not initialized; run 'hc init' first")
		}
		return err
	}

	if cfg.ProjectID == "" {
		cfg.ProjectID, _ = project.GetOrCreateProjectID(paths)
	}

	fmt.Println("Project ID:", cfg.ProjectID)
	fmt.Println("Backend:", cfg.Backend)

	store, err := metrics.NewStore(paths.Root, cfg.ProjectID)
	if err != nil {
		// metrics 不可用不应阻断基本 status。
		fmt.Println("(metrics unavailable:", err, ")")
		return nil
	}

	agents := []string{"orchestrator", "coder", "tester", "fixer", "reviewer"}
	fmt.Println()
	fmt.Println("Agent success rates:")
	for _, a := range agents {
		rate, err := store.SuccessRate(a, 10)
		if err != nil {
			fmt.Printf("  %s: n/a (%v)\n", a, err)
			continue
		}
		fmt.Printf("  %s: %.1f%%\n", a, rate*100)
	}
	return nil
}

// Restore restores configuration files from backup.
func Restore() error {
	paths, err := project.DetectPaths("")
	if err != nil {
		return err
	}
	backupDir := filepath.Join(paths.HarnessDir, "backup")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		fmt.Println("[hc-go] no backup found")
		return nil
	}
	var restored int
	err = filepath.Walk(backupDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(backupDir, p)
		if err != nil {
			return err
		}
		target := filepath.Join(paths.Root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		fmt.Println("[hc-go] Restored:", rel)
		restored++
		return nil
	})
	if err != nil {
		return err
	}
	if restored == 0 {
		fmt.Println("[hc-go] No config files to restore")
	} else {
		fmt.Printf("[hc-go] Restored %d config file(s)\n", restored)
	}
	return nil
}

// Uninstall removes harnesscode data and agents.
func Uninstall() error {
	paths, err := project.DetectPaths("")
	if err != nil {
		return err
	}

	cfg, _ := project.LoadConfig(paths)
	backendName := ""
	if cfg != nil {
		backendName = cfg.Backend
	}
	be := backend.GetBackend(backendName)

	// 当前只做最小版本：删除 .harnesscode 目录和 dev-log.txt，不直接卸载 opencode/claude。
	if err := os.RemoveAll(paths.HarnessDir); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(paths.Root, "dev-log.txt"))

	fmt.Println("[hc-go] Local harnesscode data removed for backend", be.Name())
	return nil
}

// ensureGitignore 在项目根目录下追加基本 hc 相关忽略规则（若不存在）。
func ensureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	lines := []string{
		"# HarnessCode runtime data",
		".harnesscode/",
		"dev-log.txt",
		"cycle-log.txt",
	}

	var toAppend []string
	for _, l := range lines {
		if !strings.Contains(existing, l) {
			toAppend = append(toAppend, l)
		}
	}
	if len(toAppend) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f); err != nil {
		return err
	}
	for _, l := range toAppend {
		if _, err := fmt.Fprintln(f, l); err != nil {
			return err
		}
	}
	return nil
}
