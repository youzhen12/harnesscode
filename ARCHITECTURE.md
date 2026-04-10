## HarnessCode 架构与运行原理

本文档简要说明当前 Go 版 HarnessCode 的整体架构和运行思路，便于二次开发和排查问题。

---

## 1. 设计目标

- 提供一个很薄的 `hc` 命令行工具
- 把“复杂的智能决策”交给外部 AI Agent（OpenCode / Claude Code）
- 在项目目录内维护 `.harnesscode/` 状态，形成可观察、可恢复的开发循环
- 记录每个 Agent 的运行情况与成功率，最终生成简单的开发报告

Go 代码刻意保持精简，尽量只做“编排 + 状态管理”，而不把业务逻辑写死在本地。

---

## 2. 代码结构总览

主要目录和包：

- `cmd/hc`：命令行入口，仅负责解析子命令并调用 `internal/commands`
- `internal/commands`：实现 `hc init`、`hc start`、`hc status`、`hc restore`、`hc uninstall` 等命令
- `internal/project`：项目级配置和路径检测，负责 `.harnesscode/` 目录和 `config.yaml`
- `internal/backend`：AI 后端抽象与实现，支持 `opencode` 和 `claude`
- `internal/installer`：为选定后端安装 / 更新 HarnessCode 的各个 agent 配置
- `internal/agents`：Agent 的系统提示词（Markdown），通过 `go:embed` 嵌入
- `internal/loop`：主开发循环（orchestrator -> 各类 agent 调度）
- `internal/state`：运行时状态文件读写，例如 `feature_list.json`、`missing_info.json`
- `internal/metrics`：指标收集与持久化（每个 Agent 的运行次数、成功率等）
- `internal/report`：根据 `feature_list.json` 等生成最终开发报告和进度通知
- `internal/knowledge`：写入“缺陷模式学习文档”，目前是一个简单的 Manager

---

## 3. 命令层：cmd 与 commands

### 3.1 `hc init`

入口：`internal/commands/commands.go::Init`

主要步骤：

1. 使用 `project.DetectPaths` 推断当前项目根目录与 `.harnesscode/` 路径
2. 确保 `.harnesscode/` 目录存在
3. 生成或读取 `project_id`，写入 `.harnesscode/project_id`
4. 读取或生成 `.harnesscode/config.yaml`，字段包括：
   - `project_id`
   - `backend`：`opencode` 或 `claude`
   - `auto_commit`：当前 Go 版未使用，默认 `1`
   - `webhook_url`：可选，进度通知用
5. 根据 backend 调用 `installer.EnsureAgentsInstalled` 安装 / 更新 agent：
   - OpenCode：写入 `~/.config/opencode/agents/harnesscode-*.md` 并合入 `opencode.json`
   - Claude：写入 `~/.claude/agents/harnesscode-*.md`（带 YAML frontmatter）
6. 创建 `input/prd` 和 `input/techspec` 目录
7. 更新项目根 `.gitignore`，忽略 `.harnesscode/`、`dev-log.txt`、`cycle-log.txt`

### 3.2 `hc start`

入口：`internal/commands/commands.go::Start`

内部直接调用 `loop.Run()`，由主循环接管后续逻辑（详见第 5 节）。

### 3.3 `hc status`

入口：`commands.Status`

读取 `.harnesscode/config.yaml` 和项目 `project_id`，然后：

- 创建 / 打开 `metrics.Store`
- 输出最近 10 次中 orchestrator、coder、tester、fixer、reviewer 的成功率

### 3.4 `hc restore` 和 `hc uninstall`

- `restore`：从 `.harnesscode/backup/` 恢复配置文件到项目根
- `uninstall`：删除当前项目的 `.harnesscode/` 目录和 `dev-log.txt`，但不删除全局 `$HOME/.harnesscode` 和 AI CLI

---

## 4. 项目配置与路径：`internal/project`

`project.Paths` 封装项目路径：

- `Root`：项目根目录
- `HarnessDir`：`.harnesscode` 目录
- `ConfigPath`：`.harnesscode/config.yaml`
- `ProjectIDPath`：`.harnesscode/project_id`

`project.Config` 是序列化到 `config.yaml` 的结构：

- `ProjectID`：项目 ID
- `Backend`：`opencode` 或 `claude`
- `AutoCommit`：保留字段
- `WebhookURL`：进度通知地址
- `ManualFeatures`：是否启用“手动特性模式”（见第 6.3 节）

`DetectPaths` 基于当前工作目录推断项目根；`GetOrCreateProjectID` 会在没有 `project_id` 时生成一个稳定的 ID。

---

## 5. AI 后端与 Agent 安装

### 5.1 后端抽象：`internal/backend`

定义 `Backend` 接口：

- `Name()`：后端名称
- `CommandPath()`：可执行路径
- `BuildRunCmd(agent, prompt, model)`：构造运行指定 agent 的完整命令行
- `IsInstalled()`：检测 CLI 是否可用

当前实现：

- `openCodeBackend`：
  - 调用 `opencode run --agent harnesscode-<name> <prompt>`
  - 优先使用 `OPENCODE_PATH`，其次 PATH 和若干默认安装路径
- `claudeBackend`：
  - 调用 `claude --agent harnesscode-<name> -p <prompt> ...`，带权限绕过参数

`DetectBackend` / `GetBackend` 负责根据环境变量 `HARNESSCODE_BACKEND` 或默认情况选择后端。

### 5.2 Agent 安装：`internal/installer`

`EnsureAgentsInstalled(backendName)` 会：

- 对 `opencode`：
  - 写入 `~/.config/opencode/agents/harnesscode-*.md`
  - 合并配置到 `~/.config/opencode/opencode.json` 的 `agent` 字段

- 对 `claude`：
  - 写入 `~/.claude/agents/harnesscode-*.md`，带 YAML frontmatter，注册为 `harnesscode-<role>` agent

Markdown 内容来自 `internal/agents` 包，通过 `go:embed` 嵌入，包括：

- `orchestrator.md`
- `initializer.md`
- `coder.md`
- `tester.md`
- `fixer.md`
- `reviewer.md`

这些文件定义了各 Agent 的系统职责说明。

---

## 6. 运行时状态与数据文件

### 6.1 项目内 `.harnesscode/`

典型内容：

- `config.yaml`：项目配置
- `project_id`：项目 ID
- `feature_list.json`：功能列表
- `missing_info.json`：缺失信息 / 问题列表
- `claude-progress.txt`：Agent 的文字进度记录
- `reports/`：开发报告 Markdown 文件
- `backup/`：配置文件备份（供 `hc restore` 使用）

`internal/state` 负责 `feature_list.json` 和 `missing_info.json` 的读写和格式规范：

- 支持 `FeatureList` 包装形式和纯数组形式
- 将各种“完成”状态归一化为 `completed`
- 提供 `SaveFeatureList` / `SaveMissingInfo` 覆盖写入

### 6.2 全局 `$HOME/.harnesscode/`

用于跨项目的学习与统计：

- `projects/<project_id>/learning/metrics.json`：每个 Agent 的运行记录
- `projects/<project_id>/learning/docs/solutions/bugs/*.md`：缺陷模式文档，由 `knowledge.Manager` 写入

---

## 7. 主开发循环：`internal/loop`

入口：`loop.Run()`，由 `hc start` 调用。

### 7.1 启动阶段

1. 使用 `project.DetectPaths` 和 `LoadConfig` 读取项目配置
2. 通过 `backend.GetBackend` 获取 AI 后端实现，并检查是否已安装
3. 创建 `metrics.Store` 用于记录各 Agent 的运行情况
4. 打印启动信息（Backend、Project ID）
5. 初次尝试加载 `.harnesscode/feature_list.json`：
   - 如加载成功：
     - 存为 `lastFeatures` 基线
     - 调用 `notifyProgress` 输出一次当前进度
   - 如文件不存在：
     - 若 `config.manual_features == true`：仅提示用户手动创建，不自动跑 initializer
     - 否则：调用 `initializer` agent 扫描文档生成 feature_list，然后再尝试加载

### 7.2 循环流程

主循环按“周期（Cycle）”工作：

1. 调用 orchestrator agent：
   - 构造提示词，告诉 orchestrator 按系统说明决策下一步要跑的 agent
   - 运行 AI CLI，捕获输出日志和结果
   - 用 `parseDecision` 从输出中解析：
     - `--- ORCHESTRATOR NEXT: [AGENT] [args] ---`

2. 基于 `feature_list` 的硬性检查：
   - 重新加载 `feature_list.json`，统计：
     - `totalFeatures`
     - `completedFeatures`
     - `pendingFeatures`
   - 如 `totalFeatures > 0` 且 `pendingFeatures == 0`：
     - 若 orchestrator 未返回 `complete`，则强制结束循环并生成最终报告
   - 若 `config.manual_features == true` 且 orchestrator 指定 `initializer`：
     - 跳过本轮，提示“manual_features 模式下不运行 initializer”，进入下一轮 orchestrator 决策

3. 处理 orchestrator 的决策：
   - 若 `nextAgent == ""`：无决策，打印日志并结束
   - 若 `nextAgent == "complete"`：
     - 尝试生成最终报告（读取 feature_list 统计）
     - 结束循环
   - 若 `nextAgent == "coder"` 且当前无 pending feature：
     - 跳过 coder，要求 orchestrator 重新决策

4. 调用指定的下一 Agent：
   - 使用 `buildAgentPrompt` 构造 Agent 提示词：
     - 指示读取 `.harnesscode/claude-progress.txt` 和 `.harnesscode/feature_list.json`（如存在）
     - 要求只完成一个任务并更新进度后退出
     - 附带 orchestrator 的额外参数（如模块 / feature ID 说明）
   - 调用 `runAgentOnce` 运行 AI CLI，实时打印输出
   - 记录运行结果和耗时到 metrics

5. Agent 执行后的进度与终止判断：
   - 再次加载 `feature_list.json`：
     - 调用 `notifyProgress` 与上一轮 `lastFeatures` 做 diff，打印 / 发送进度变化
     - 更新 `lastFeatures`
   - 统计当前 `total` / `pending`：
     - 如 `total > 0` 且 `pending == 0`：
       - 打印“所有特性已完成”，生成最终报告并结束循环

6. 在两轮之间 sleep 一小段时间（当前为 5 秒），避免过快轮询。

### 7.3 运行一个 Agent：`runAgentOnce`

`runAgentOnce` 封装了对外部 AI CLI 的一次调用：

- 使用 `backend.BuildRunCmd` 生成命令行参数
- 创建子进程并读取 stdout（stderr 合并到 stdout）
- 持续打印子进程输出，并缓存在内存中
- 使用一个 `idleTimeout`（默认 5 分钟）监控无输出情况：
  - 若一段时间内没有新输出，取消子进程并返回超时错误

---

## 8. Agent 角色与交互关系

当前内置的 Agent 角色有：

- `orchestrator`
- `initializer`
- `coder`
- `tester`
- `fixer`
- `reviewer`

它们的“硬接口”非常简单：

1. 每个 Agent 在本地 AI CLI 中注册为 `harnesscode-<name>`（例如 `harnesscode-orchestrator`）
2. Go 侧只关心两个维度：
   - 调哪个 Agent（字符串名称）
   - 给它一个 prompt（字符串）
3. Agent 通过读写项目内的 `.harnesscode/*` 文件和项目代码，与其他 Agent 间接通信

### 8.1 各 Agent 职责

职责定义主要由 `internal/agents/*.md` 中的系统提示词决定（这里是概要）：

- **orchestrator**（协调者）
  - 输入：
    - `.harnesscode/config.yaml`
    - `.harnesscode/feature_list.json`
    - `.harnesscode/claude-progress.txt`
    - 其它 `.harnesscode/*` 状态文件（如 `test_report.json`、`review_report.json` 等，未来可扩展）
  - 任务：
    - 读取当前项目状态
    - 决定下一步应该运行哪个 Agent，以及可选参数（模块名、feature id 等）
    - 按约定格式输出：`--- ORCHESTRATOR NEXT: [AGENT] [args] ---`
  - 输出：
    - 不直接改代码，只通过“下一 Agent 决策”影响后续流程

- **initializer**（特性初始化/同步）
  - 输入：
    - `.harnesscode/feature_list.json`（如存在）
    - `.harnesscode/claude-progress.txt`
    - PRD / 技术文档：`input/prd/`、`input/techspec/`、`docs/` 等
  - 任务：
    - 从文档中识别出功能点
    - 建立或更新 `.harnesscode/feature_list.json`，保证 ID 相对稳定
    - 简要记录本次同步的内容到 `claude-progress.txt`
  - 输出：
    - 更新后的 `feature_list.json`
    - 更新后的 `claude-progress.txt`
  - 说明：
    - 当 `config.manual_features == true` 时，主循环会禁止 orchestrator 调用 initializer，以避免自动“重建” feature 列表。

- **coder**（实现者）
  - 输入：
    - `.harnesscode/feature_list.json`（从中选择一个 pending feature）
    - `.harnesscode/claude-progress.txt`
    - 项目代码（例如 `internal/**`、`pkg/**`、`cmd/**`）
  - 任务：
    - 选取一个待完成的 feature
    - 在代码和测试中实现该特性或补齐缺口
    - 只在非常明确为“文档类” feature 时，才允许仅修改 docs/openspec 等
    - 若判断 feature 已实现，需要说明检查过哪些具体文件 / 函数，再更新其状态
  - 输出：
    - 代码变更（源代码和/或测试文件）
    - 将对应 feature 的 `status` 更新为 `completed`
    - 在 `claude-progress.txt` 中追加一条本轮工作摘要

- **tester**（测试执行者）
  - 输入：
    - 项目代码
    - 可能使用 `.harnesscode/feature_list.json` / 配置等，决定测试范围
  - 任务：
    - 运行项目测试（单元测试、集成测试、静态分析等）
    - 收集失败信息与状态
    - 将结果写入 `.harnesscode/test_report.json`
  - 输出：
    - `test_report.json`，包含整体 pass/fail 状态和失败详情
  - 约束：
    - 不直接修改源代码（修复由 fixer 完成）

- **fixer**（修复者）
  - 输入：
    - `.harnesscode/test_report.json`
    - `.harnesscode/review_report.json`
    - 项目代码
  - 任务：
    - 根据测试和代码审查报告，修复失败测试或存在问题的代码
    - 一次专注小范围修复，避免大范围 refactor
    - 更新对应报告中已修复项的状态
  - 输出：
    - 源代码修复
    - 更新后的 `test_report.json` 与 `review_report.json`

- **reviewer**（审查者）
  - 输入：
    - 最近代码改动（通常通过 git diff 或文件系统变化推断）
    - 项目代码
  - 任务：
    - 对近期变更进行风格、安全性、可维护性审查
    - 识别潜在问题，写入 `.harnesscode/review_report.json`
    - 输出可以被 fixer 使用的具体修复建议
  - 输出：
    - `review_report.json`，记录发现的问题和建议修复方式

### 8.2 Agent 间的交互与数据流

Agent 并不直接互相调用，而是通过以下机制间接交互：

1. orchestrator 决定“下一步跑哪个 Agent + 传什么参数”
2. 各 Agent 通过读写 `.harnesscode/*` 状态文件和项目代码，给后续 Agent 留下“上下文”和“任务清单”

一个典型的交互循环可能是：

1. **initializer**：从 PRD/docs 中构建 `feature_list.json`
2. **orchestrator**：看到有 pending feature，决策 `coder`（可能带上模块 / feature id）
3. **coder**：
   - 读取 `feature_list.json` 与对应代码
   - 实现某一 feature
   - 更新该 feature 的 `status` 为 `completed`
   - 在 `claude-progress.txt` 中记录说明
4. **orchestrator**：
   - 看到 feature 已完成，但可能有新的风险
   - 决策 `tester` 运行测试
5. **tester**：
   - 运行测试，将结果写入 `test_report.json`
6. **orchestrator**：
   - 根据 `test_report.json` 是否有失败，决定调用 `fixer` 或继续下一个 feature
7. **fixer**（如果有失败）：
   - 读取 `test_report.json`，修复具体问题
   - 更新报告状态
8. **reviewer**（可选）：
   - 对最近改动做审查，将问题写入 `review_report.json`
9. **fixer**（再次可选）：
   - 根据 `review_report.json` 修复风格/安全问题
10. 若所有 feature 均为 `completed`，且测试/审查通过：
    - orchestrator 输出 `complete`
    - 或主循环基于 `feature_list.json` 自动检测到无 pending feature，生成最终报告并结束

从数据流角度看：

- `feature_list.json`：串联 initializer、coder、orchestrator、loop 终止条件
- `claude-progress.txt`：记账和“人类可读”的过程日志，便于后续理解和调试
- `test_report.json` / `review_report.json`：连接 tester/reviewer 与 fixer

Go 端只负责：

- 串起 orchestrator 与各 Agent 的调用顺序
- 在关键点重载 `feature_list.json` 等状态文件，用于进度判断和退出条件
- 将 Agent 的 stderr/stdout 打印到本地，便于观察行为

---

## 9. 进度通知与报告

### 8.1 进度通知：`notifyProgress`

`notifyProgress(webhookURL, projectID, old, new)` 会：

1. 构建旧 feature 状态的索引
2. 遍历新 `FeatureList`，统计：
   - 总特性数
   - 完成数（`completed`）
   - 待完成数（`pending`）
3. 找出状态发生变化的特性，并为完成、回退等情况设置不同标签
4. 在控制台打印进度摘要 + 状态变更列表
5. 如配置了 `webhook_url`，构造一条文本消息并通过 HTTP POST 发送

### 8.2 开发报告：`internal/report`

`GenerateDevReport(projectRoot, projectID, reportType)` 会：

- 读取 `feature_list.json` 统计 `total` / `completed` / `pending`
- 在 `.harnesscode/reports/` 下生成一份 Markdown 报告：
  - 包含时间戳、项目 ID
  - 包含简单的 Summary 表格

主循环在以下场景自动生成报告：

- orchestrator 返回 `complete`
- 严格模式检测到“所有 feature 已完成”时强制结束循环

---

## 10. Metrics 与知识学习

### 9.1 Metrics：`internal/metrics`

`metrics.Store` 按 Agent 记录：

- 调用次数
- 成功 / 失败次数
- 最近 N 次（当前默认 10）的成功率

`hc status` 命令基于这些数据输出每个 Agent 的成功率，帮助观察 orchestrator 与各 Agent 的稳定性。

### 9.2 知识学习：`internal/knowledge`

`knowledge.Manager` 提供一个很简单的接口：

- `SaveBugPattern(summary, location, action)`：写入一篇“缺陷模式”文档到：
  - `$HOME/.harnesscode/projects/<project_id>/learning/docs/solutions/bugs/`

文档目前主要包含：摘要、位置、解决方案和一些简单的标签，后续可以扩展为更完整的知识库。

---

## 11. 扩展方向

如果你希望在现有架构上扩展功能，可以考虑：

1. 新增 Agent 类型：
   - 在 `internal/agents` 中添加新的 Markdown 文件
   - 在 `installer` 中注册新的 agent 名称
   - 在 orchestrator 提示词中加入新的决策分支

2. 接入新的 AI 后端：
   - 在 `internal/backend` 中实现新的 `Backend`
   - 在 `GetBackend` / `DetectBackend` 中增加分支

3. 丰富报告与进度展示：
   - 扩展 `internal/report`，生成更详细的开发报告
   - 在进度通知中附带更多上下文信息

4. 在主循环中增加更多“硬约束”：
   - 例如根据模块 / 依赖图控制 feature 顺序
   - 控制某些 Agent 的调用频率（如减少 initializer 频率）

整体原则：

- 尽量保持 Go 代码作为“编排层”，把决策与具体细节交给 AI Agent
- 所有 Agent 行为通过 Markdown Prompt 配置，便于快速迭代与调试
