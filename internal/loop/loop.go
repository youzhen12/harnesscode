package loop

import (
	"fmt"
	"os"
	"strings"
	"time"

	"harnesscode-go/internal/backend"
	"harnesscode-go/internal/metrics"
	"harnesscode-go/internal/project"
	"harnesscode-go/internal/report"
	"harnesscode-go/internal/state"
)

const (
	idleTimeout             = 5 * time.Minute
	maxOrchestratorFailures = 3
	maxInitializerRetries   = 3
)

// Run 主开发循环的一个精简版本：
// 1. 读取项目配置与 backend
// 2. 周期性调用 orchestrator，解析下一步 agent
// 3. 调用对应 agent，并记录 metrics
//
// 与 Python 版相比：
// - 暂不实现复杂的 feature_list/missing_info/报告生成
// - 保留最核心的 orchestrator -> agent 调度路径
func Run() error {
	paths, err := project.DetectPaths("")
	if err != nil {
		return err
	}
	cfg, err := project.LoadConfig(paths)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID, _ = project.GetOrCreateProjectID(paths)
	}

	be := backend.GetBackend(cfg.Backend)
	if !be.IsInstalled() {
		return fmt.Errorf("backend %s not installed", be.Name())
	}

	store, err := metrics.NewStore(paths.Root, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("metrics store: %w", err)
	}

	fmt.Println("[hc-go] Starting loop")
	fmt.Println("  Backend:", be.Name())
	fmt.Println("  Project:", cfg.ProjectID)

	// 初始加载一次 feature_list 作为进度监控基线；如不存在则交由 ensureFeatureList 统一处理。
	runner := NewRunner(paths.Root, be)
	var lastFeatures *state.FeatureList
	fl, ferr := ensureFeatureList(paths, cfg, runner, store)
	if ferr != nil {
		fmt.Println("[hc-go] warning: ensureFeatureList failed:", ferr)
	}
	if fl != nil {
		lastFeatures = fl
		// 启动时发送一次当前进度。
		_ = notifyProgress(cfg.WebhookURL, cfg.ProjectID, nil, fl)
	}

	iteration := 1
	var orchFailures int
	for {
		fmt.Printf("\n===== Cycle %d =====\n", iteration)

		// 1. 调 orchestrator
		orchPrompt := buildOrchestratorPrompt()
		agent := "orchestrator"
		start := time.Now()
		output, err := runner.Run(agent, orchPrompt)
		dur := time.Since(start).Seconds()
		_ = store.RecordSession(agent, err == nil, dur)
		logAgentRun(iteration, agent, dur, err == nil, err)
		if err != nil {
			orchFailures++
			fmt.Printf("[hc-go] orchestrator error (consecutive=%d/%d): %v\n", orchFailures, maxOrchestratorFailures, err)
			if orchFailures >= maxOrchestratorFailures {
				return fmt.Errorf("orchestrator failed %d times in a row: %w", orchFailures, err)
			}
			backoff := time.Duration(orchFailures) * 5 * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			fmt.Printf("[hc-go] retrying orchestrator after %s...\n", backoff)
			time.Sleep(backoff)
			iteration++
			continue
		}
		orchFailures = 0

		nextAgent, nextArgs := parseDecision(output)

		// 每次 orchestrator 决策后，基于当前 feature_list 做一次硬性检查。
		var (
			stats       state.FeatureStats
			statsLoaded bool
		)
		if flNow, ferr := state.LoadFeatureList(paths.Root); ferr == nil && flNow != nil {
			statsLoaded = true
			stats = state.ComputeFeatureStats(flNow)
			fmt.Printf("[hc-go] features: total=%d, completed=%d, pending=%d\n", stats.Total, stats.Completed, stats.Pending)
			// 所有 feature 已完成，而 orchestrator 仍未给出 complete 时，强制结束并生成报告。
			if state.AllFeaturesCompleted(stats) && nextAgent != "complete" {
				fmt.Println("[hc-go] all features completed in feature_list.json; forcing completion")
				if path, rerr := report.GenerateDevReport(paths.Root, cfg.ProjectID, "final"); rerr != nil {
					fmt.Println("[hc-go] failed to generate final report:", rerr)
				} else {
					fmt.Println("[hc-go] final report:", path)
				}
				return nil
			}
		}

		// manual_features 模式下，禁止运行 initializer，强制 orchestrator 重新决策。
		if cfg.ManualFeatures && strings.ToLower(nextAgent) == "initializer" {
			fmt.Println("[hc-go] manual_features enabled; skipping initializer and asking orchestrator to reconsider")
			time.Sleep(5 * time.Second)
			iteration++
			continue
		}

		if nextAgent == "" {
			fmt.Println("[hc-go] no decision from orchestrator, stopping")
			return nil
		}
		if nextAgent == "complete" {
			fmt.Println("[hc-go] orchestrator signaled completion")
			// 生成最终报告（如果有 feature_list）。
			if _, err := state.LoadFeatureList(paths.Root); err == nil {
				_, _ = report.GenerateDevReport(paths.Root, cfg.ProjectID, "final")
			}
			return nil
		}
		// 如果 orchestrator 要求 coder，但当前 feature_list 中已经没有 pending，则跳过这一轮，让 orchestrator 重新决策。
		if nextAgent == "coder" && statsLoaded && state.AllFeaturesCompleted(stats) {
			fmt.Println("[hc-go] orchestrator requested coder but no pending features in feature_list.json; skipping coder and asking orchestrator to reconsider")
			time.Sleep(5 * time.Second)
			iteration++
			continue
		}

		fmt.Printf("[hc-go] next: %s %s\n", nextAgent, nextArgs)

		// 2. 运行下一 agent
		prompt := buildAgentPrompt(nextArgs)
		start = time.Now()
		_, err = runner.Run(nextAgent, prompt)
		dur = time.Since(start).Seconds()
		_ = store.RecordSession(nextAgent, err == nil, dur)
		logAgentRun(iteration, nextAgent, dur, err == nil, err)
		if err != nil {
			fmt.Printf("[hc-go] agent %s error: %v\n", nextAgent, err)
		}

		// 3. agent 运行后重载 feature_list，比较变更并发送进度通知。
		if fl, err := state.LoadFeatureList(paths.Root); err == nil {
			_ = notifyProgress(cfg.WebhookURL, cfg.ProjectID, lastFeatures, fl)
			lastFeatures = fl
			// 如果所有 feature 已完成，则直接结束循环并生成最终报告。
			stats := state.ComputeFeatureStats(fl)
			if state.AllFeaturesCompleted(stats) {
				fmt.Println("[hc-go] all features completed in feature_list.json; stopping loop")
				fmt.Printf("[hc-go] features: total=%d, completed=%d, pending=%d\n", stats.Total, stats.Completed, stats.Pending)
				if path, rerr := report.GenerateDevReport(paths.Root, cfg.ProjectID, "final"); rerr != nil {
					fmt.Println("[hc-go] failed to generate final report:", rerr)
				} else {
					fmt.Println("[hc-go] final report:", path)
				}
				return nil
			}
		}

		time.Sleep(5 * time.Second)
		iteration++
	}
}

// ensureFeatureList 负责在启动阶段统一处理 feature_list.json 的初始化逻辑：
// 1) 如果已存在则直接加载
// 2) 不存在且 manual_features=false 时，调用 initializer 做一次引导
// 3) 任何失败都不阻塞主循环，只打印 warning
func ensureFeatureList(paths *project.Paths, cfg *project.Config, runner *Runner, store *metrics.Store) (*state.FeatureList, error) {
	fl, err := state.LoadFeatureList(paths.Root)
	if err == nil {
		return fl, nil
	}
	if !os.IsNotExist(err) {
		fmt.Println("[hc-go] warning: failed to load feature_list.json:", err)
		return nil, err
	}

	if cfg.ManualFeatures {
		fmt.Println("[hc-go] manual_features enabled but no .harnesscode/feature_list.json found; please create it manually.")
		return nil, nil
	}

	initPrompt := "Scan PRD and tech spec documents under input/ and docs/, then build or update .harnesscode/feature_list.json, then exit cleanly."
	for attempt := 1; attempt <= maxInitializerRetries; attempt++ {
		fmt.Printf("[hc-go] no .harnesscode/feature_list.json found; running initializer to bootstrap features (attempt %d/%d)\n", attempt, maxInitializerRetries)
		start := time.Now()
		_, runErr := runner.Run("initializer", initPrompt)
		dur := time.Since(start).Seconds()
		_ = store.RecordSession("initializer", runErr == nil, dur)
		logAgentRun(0, "initializer", dur, runErr == nil, runErr)
		if runErr != nil {
			fmt.Printf("[hc-go] initializer error on attempt %d/%d: %v\n", attempt, maxInitializerRetries, runErr)
			if attempt == maxInitializerRetries {
				return nil, runErr
			}
			backoff := time.Duration(attempt) * 5 * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			fmt.Printf("[hc-go] retrying initializer after %s...\n", backoff)
			time.Sleep(backoff)
			continue
		}

		fl, err = state.LoadFeatureList(paths.Root)
		if err != nil {
			fmt.Println("[hc-go] warning: initializer completed but failed to load feature_list.json:", err)
			return nil, err
		}
		return fl, nil
	}

	// 理论上不会到达这里，但为确保编译通过保留兜底返回值。
	return nil, fmt.Errorf("initializer retries exceeded without creating feature_list.json")
}

// logAgentRun 统一输出每次 Agent 运行的关键信息，便于排查问题和观测性能。
// cycle 为当前循环编号；对于初始化阶段（如 ensureFeatureList 中的 initializer），可传入 0。
func logAgentRun(cycle int, agent string, durSeconds float64, success bool, err error) {
	status := "ok"
	if !success {
		status = "error"
	}
	if err != nil {
		fmt.Printf("[hc-go] cycle=%d agent=%s status=%s duration=%.1fs err=%v\n", cycle, agent, status, durSeconds, err)
		return
	}
	fmt.Printf("[hc-go] cycle=%d agent=%s status=%s duration=%.1fs\n", cycle, agent, status, durSeconds)
}

func parseDecision(output string) (agent, args string) {
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(strings.ToUpper(l), "--- ORCHESTRATOR NEXT:") {
			// 示例: --- ORCHESTRATOR NEXT: coder module 1 ---
			idx := strings.Index(l, ":")
			if idx < 0 {
				continue
			}
			body := strings.TrimSpace(l[idx+1:])
			body = strings.Trim(body, "-")
			fields := strings.Fields(body)
			if len(fields) == 0 {
				continue
			}
			agent = strings.ToLower(fields[0])
			if len(fields) > 1 {
				args = strings.Join(fields[1:], " ")
			}
			return
		}
	}
	return "", ""
}

// notifyProgress 比较前后两次 feature_list，发现状态变化时：
// 1) 在控制台打印简要变更
// 2) 如果配置了 webhook_url，通过 IM 发送进度摘要
func notifyProgress(webhookURL, projectID string, old, new *state.FeatureList) error {
	if new == nil || len(new.Features) == 0 {
		return nil
	}

	// 构建 old 的索引以便 diff。
	oldIdx := map[int]state.Feature{}
	if old != nil {
		for _, f := range old.Features {
			oldIdx[f.ID] = f
		}
	}

	var changes []string

	// 使用抽象的状态统计，避免在 loop 中重复计算总数和完成数。
	stats := state.ComputeFeatureStats(new)
	total := stats.Total
	completed := stats.Completed

	for _, f := range new.Features {
		if prev, ok := oldIdx[f.ID]; ok {
			if prev.Status != f.Status {
				label := "[UPDATE]"
				if f.Status == "completed" {
					label = "[PASS]"
				} else if f.Status == "pending" && prev.Status == "completed" {
					label = "[BACK]"
				}
				name := f.Name
				if name == "" {
					name = f.Description
				}
				if len(name) > 40 {
					name = name[:40]
				}
				changes = append(changes, fmt.Sprintf("%s %d: %s (%s -> %s)", label, f.ID, name, prev.Status, f.Status))
			}
		}
	}

	if len(changes) == 0 {
		return nil
	}

	progress := float64(0)
	if total > 0 {
		progress = float64(completed) / float64(total) * 100
	}

	fmt.Printf("[hc-go] Progress update: %d/%d completed (%.1f%%)\n", completed, total, progress)
	for _, line := range changes {
		fmt.Println("  ", line)
	}

	// 没有 webhook 时只打印本地日志。
	if webhookURL == "" {
		return nil
	}

	msg := fmt.Sprintf("[Progress] %s: %d/%d completed (%.1f%%)\n\nChanges:\n%s", projectID, completed, total, progress, strings.Join(changes, "\n"))
	return report.SendWebhook(webhookURL, msg)
}
