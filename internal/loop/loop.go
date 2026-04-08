package loop

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"harnesscode-go/internal/backend"
	"harnesscode-go/internal/metrics"
	"harnesscode-go/internal/project"
	"harnesscode-go/internal/report"
	"harnesscode-go/internal/state"
)

const idleTimeout = 5 * time.Minute

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

	// 初始加载一次 feature_list 作为进度监控基线。
	var lastFeatures *state.FeatureList
	if fl, err := state.LoadFeatureList(paths.Root); err == nil {
		lastFeatures = fl
		// 启动时发送一次当前进度。
		_ = notifyProgress(cfg.WebhookURL, cfg.ProjectID, nil, fl)
	}

	iteration := 1
	for {
		fmt.Printf("\n===== Cycle %d =====\n", iteration)

		// 1. 调 orchestrator
		orchPrompt := "Follow your system instructions, decide next agent and optional args, then output in format '\\n--- ORCHESTRATOR NEXT: [AGENT] [args] ---\\n' and exit."
		agent := "orchestrator"
		start := time.Now()
		output, err := runAgentOnce(paths.Root, be, agent, orchPrompt)
		dur := time.Since(start).Seconds()
		_ = store.RecordSession(agent, err == nil, dur)
		if err != nil {
			fmt.Println("[hc-go] orchestrator error:", err)
			time.Sleep(5 * time.Second)
			iteration++
			continue
		}

		nextAgent, nextArgs := parseDecision(output)
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

		fmt.Printf("[hc-go] next: %s %s\n", nextAgent, nextArgs)

		// 2. 运行下一 agent
		prompt := buildAgentPrompt(nextArgs)
		start = time.Now()
		_, err = runAgentOnce(paths.Root, be, nextAgent, prompt)
		dur = time.Since(start).Seconds()
		_ = store.RecordSession(nextAgent, err == nil, dur)
		if err != nil {
			fmt.Printf("[hc-go] agent %s error: %v\n", nextAgent, err)
		}

		// 3. agent 运行后重载 feature_list，比较变更并发送进度通知。
		if fl, err := state.LoadFeatureList(paths.Root); err == nil {
			_ = notifyProgress(cfg.WebhookURL, cfg.ProjectID, lastFeatures, fl)
			lastFeatures = fl
		}

		time.Sleep(5 * time.Second)
		iteration++
	}
}

func runAgentOnce(projectRoot string, be backend.Backend, agent, prompt string) (string, error) {
	cmdArgs, err := be.BuildRunCmd(agent, prompt, "")
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = projectRoot
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return "", err
	}

	outBuf := &strings.Builder{}
	lastOutput := time.Now()

	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println(line)
			outBuf.WriteString(line)
			outBuf.WriteString("\n")
			lastOutput = time.Now()
		}
		done <- scanner.Err()
	}()

	for {
		select {
		case err := <-done:
			_ = cmd.Wait()
			if err != nil {
				return outBuf.String(), err
			}
			return outBuf.String(), nil
		case <-time.After(10 * time.Second):
			if time.Since(lastOutput) > idleTimeout {
				cancel()
				_ = cmd.Wait()
				return outBuf.String(), fmt.Errorf("idle timeout (%s)", idleTimeout)
			}
		}
	}
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

func buildAgentPrompt(orchestratorArgs string) string {
	base := "Read .harnesscode/claude-progress.txt and .harnesscode/feature_list.json if present, follow your system instructions, complete ONE task, update progress, then exit cleanly."
	if strings.TrimSpace(orchestratorArgs) == "" {
		return base
	}
	return base + " Orchestrator instruction: " + orchestratorArgs
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
	var total, completed, pending int
	for _, f := range new.Features {
		total++
		if f.Status == "completed" {
			completed++
		} else if f.Status == "pending" {
			pending++
		}
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
