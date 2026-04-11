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
)

// Runner 封装单次 Agent 调用的执行细节：
// - 基于 backend.Backend 构造命令
// - 负责子进程启动、输出流读取与 idle 超时控制
//
// 这样 loop.Run 只需关注“调度谁”和“如何记录结果”，
// 而无需关心具体的 CLI 交互与进程管理逻辑。
type Runner struct {
	projectRoot string
	backend     backend.Backend
}

// NewRunner 创建一个基于给定项目根目录和后端的 Runner。
func NewRunner(projectRoot string, be backend.Backend) *Runner {
	return &Runner{
		projectRoot: projectRoot,
		backend:     be,
	}
}

// Run 执行一次指定 Agent，并返回其完整输出。
//
// 行为等价于原来的 runAgentOnce：
// - 使用 backend.BuildRunCmd 构造命令
// - 实时打印标准输出
// - 在 idleTimeout 时间内无新输出则主动取消子进程
func (r *Runner) Run(agent, prompt string) (string, error) {
	cmdArgs, err := r.backend.BuildRunCmd(agent, prompt, "")
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = r.projectRoot
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
