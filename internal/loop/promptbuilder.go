package loop

import "strings"

// buildAgentPrompt 构造 coder/tester/fixer/reviewer 等非 orchestrator Agent 使用的系统指令。
//
// 其职责是将通用约束（读取 docs、更新进度、只完成一个任务等）集中在一处，
// 以便未来演进时可以在不修改 loop 控制流的前提下统一调整 Prompt 规范。
func buildAgentPrompt(orchestratorArgs string) string {
	base := "Read .harnesscode/claude-progress.txt, .harnesscode/feature_list.json, and docs under .harnesscode/docs/ if present. In .harnesscode/docs/, treat tech_spec.md as the technical plan and feature breakdown, architecture.md as the project/module architecture, and conventions.md as the code style, testing, and error-handling rules. Then follow your system instructions, complete ONE task (code change, test update, or state update), append a brief progress note, and exit cleanly."
	if strings.TrimSpace(orchestratorArgs) == "" {
		return base
	}
	return base + " Orchestrator instruction: " + orchestratorArgs
}

// buildOrchestratorPrompt 构造 orchestrator 专用的系统指令，
// 强调从技术文档与 feature_list.json 读取状态并给出下一步调度决策。
func buildOrchestratorPrompt() string {
	return "Read .harnesscode/claude-progress.txt, .harnesscode/feature_list.json, and docs under .harnesscode/docs/ if present. For planning, prioritize tech_spec.md (technical plan and phases) and architecture.md (project/module architecture); consult conventions.md as needed. Then follow your system instructions, decide next agent and optional args, output exactly one line in format '\n--- ORCHESTRATOR NEXT: [AGENT] [args] ---\n', and exit."
}
