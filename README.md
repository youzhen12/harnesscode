# HarnessCode
一个面向“AI 驱动开发循环”的命令行工具。

> 架构与运行原理详见：[ARCHITECTURE.md](./ARCHITECTURE.md)

核心目标：

- 提供一个简单的 `hc` CLI
- 自动安装 / 配置 OpenCode 或 Claude Code 的 agents
- 在项目目录内维护 `.harnesscode/` 状态目录
- 循环调用 orchestrator / coder / tester / fixer / reviewer 等 agent
- 记录指标、发送进度通知、生成开发报告

> 注意：当前版本偏向“核心功能 + 精简实现”，接口稳定性未完全锁定，适合个人或小团队试用 / 二次开发。

---

## 1. 环境准备

1. 安装 Go（推荐 1.22+）
2. 安装至少一个 AI CLI 后端：
   - OpenCode: `opencode`
   - 或 Claude Code: `claude`

两种方式都支持：

- 通过 PATH 查找（例如 `opencode`, `claude` 直接可用）
- 或设置环境变量：

```bash
export OPENCODE_PATH=/path/to/opencode
export CLAUDE_PATH=/path/to/claude
```

3. 克隆本仓库后，进入 Go 子目录：

```bash
cd harnesscode-go
go build ./cmd/hc
```

构建成功后，当前目录下会生成一个 `hc` 可执行文件。

---

## 2. 在项目中初始化 HarnessCode

假设你在某个业务项目根目录下，希望接入 HarnessCode：

1. 将 `hc` 拷贝或链接到 PATH（可选）：

```bash
cp /path/to/harnesscode-go/hc /usr/local/bin/hc
```

2. 在项目根目录执行初始化：

```bash
cd /your/project
hc init
```

`hc init` 会做：

- 创建 `.harnesscode/` 目录
- 生成 `project_id` 并写入 `.harnesscode/project_id`
- 生成 `.harnesscode/config.yaml`，包含：
  - `project_id`
  - `backend`: `opencode` 或 `claude`
  - `auto_commit`: 当前 Go 版暂未使用，默认 `1`
- 创建 `input/prd` 和 `input/techspec` 目录
- 在项目根创建 `docs/` 目录，并在 `.harnesscode/` 下创建 `.harnesscode/docs/` 目录，用于存放和消费技术方案文档
- 更新项目根的 `.gitignore`，忽略 `.harnesscode/`、`dev-log.txt` 等
- 根据配置的 backend 自动安装 agents：
  - OpenCode:
    - `~/.config/opencode/agents/harnesscode-*.md`
    - 合并到 `~/.config/opencode/opencode.json` 的 `agent` 配置
  - Claude:
    - `~/.claude/agents/harnesscode-*.md`（带 YAML frontmatter）

> backend 选择逻辑：
> - 优先使用环境变量 `HARNESSCODE_BACKEND`（`opencode` 或 `claude`）
> - 否则自动检测已安装的后端（当前 Go 版默认偏向 `opencode`）

---

## 3. 配置 Webhook（可选）

如果想在每次功能进度变化时，通过 IM / Webhook 接收通知：

1. 打开 `.harnesscode/config.yaml`
2. 添加或修改：

```yaml
webhook_url: "https://your-webhook-endpoint"
```

通知内容包含：

- 当前完成进度：`已完成 / 总数 (百分比)`
- 状态变更列表，例如：

```text
[PASS] 2: login flow (pending -> completed)
[UPDATE] 5: cache tuning (in-progress -> completed)
```

> 如果未配置 `webhook_url`，系统仍会在本地控制台打印进度变更，只是不发送 IM。

---

## 4. 准备 feature_list（执行文档）

HarnessCode 的调度和进度监控依赖 `.harnesscode/feature_list.json`。

当前 Go 版支持两种 JSON 结构：

1. 包装形式（推荐）：

```json
{
  "features": [
    { "id": 1, "name": "login", "status": "pending" },
    { "id": 2, "name": "list users", "status": "pending" }
  ]
}
```

2. 纯数组形式：

```json
[
  { "id": 1, "name": "login", "status": "pending" },
  { "id": 2, "name": "list users", "status": "pending" }
]
```

字段说明（部分）：

- `id`：整数 ID，尽量保持稳定
- `name` / `description`：功能名称或描述
- `module`：所属模块（可选）
- `status`：功能状态，常见：
  - `pending`
  - `completed`
  - 其它状态会原样保留
- `dependencies`：依赖的其它 feature id 列表（可选）

通常有两种用法：

- 手工编写：你在接入初期手工写好一份 feature_list，描述想让 AI 完成的功能清单；
- 文档驱动自动生成：你把技术方案写在 `.harnesscode/docs/*.md`、项目根 `docs/` 或 `input/` 下，由 `initializer` agent 在 `hc start` 的循环中解析这些文档、生成和更新 `feature_list.json`，使其与技术方案对齐，作为“执行文档”。

此外，你可以通过以下文件“定制规则”，让测试和审查阶段完全以技术方案和你的规则为准：

- `.harnesscode/test_rules.md`：自定义测试规则（例如必须覆盖的模块、边界条件、性能/安全约束）。`tester` agent 会在生成和运行测试时参考这些规则，并在 `test_report.json` 中标明违反哪条规则。
- `.harnesscode/review_rules.md`：自定义审查规则（例如编码规范、安全红线、架构约束）。`reviewer` agent 会在审查代码时按这些规则给出结论，并在 `review_report.json` 中标明每条问题对应的规则或设计约束。

后续由 orchestrator / initializer / coder / tester / fixer / reviewer 等 agent 基于该文件来驱动开发，loop 只负责读取和对比、决定是否结束循环。

---

## 5. 启动主开发循环

在项目根目录执行：

```bash
hc start
```

行为概要：

1. 读取 `.harnesscode/config.yaml` 与 `project_id`
2. 检查 backend CLI 是否可用（`opencode` 或 `claude`）
3. 创建或打开指标文件：
   - `$HOME/.harnesscode/projects/<project_id>/learning/metrics.json`
4. 尝试读取 `.harnesscode/feature_list.json`：
   - 如存在，则作为当前“执行文档”基线并发送一次进度通知；
   - 如不存在且 `manual_features` 未开启，则在循环内调起 `initializer`，基于 `.harnesscode/docs/` / `docs/` / `input/` 下的技术方案生成 `feature_list.json`；
5. 进入主循环，每一轮：
   - 调用 orchestrator agent，读取 `.harnesscode/docs/` + `.harnesscode/feature_list.json` + 各种报告（test/review）并输出：
     `--- ORCHESTRATOR NEXT: [AGENT] [args] ---`
   - 按 orchestrator 的决策依次调用：
     - `initializer`：从技术方案文档生成/更新 feature_list（执行文档）；
     - `coder`：根据 `.harnesscode/docs/` + feature_list 实现或优化代码；
     - `tester`：为相关 API/行为编写或补充测试用例，并运行测试，将结果写入 `.harnesscode/test_report.json`；
     - `fixer`：根据测试报告和审查报告修复问题，更新报告；
     - `reviewer`：审查最近改动，将问题写入 `.harnesscode/review_report.json`；
   - 每轮结束后重新加载 feature_list 并与上一轮 diff：
     - 在控制台打印进度变化；
     - 如果配置了 `webhook_url`，则通过 IM 发送进度摘要；
   - 当所有 feature 都标记为 `completed`，且 orchestrator 认为测试和审查通过时，输出 `complete`，生成最终报告并退出循环。

loop 有一个空闲超时：

- 如果某次调用在 `5 分钟` 内没有产生任何输出，将终止该次 agent 调用并报错（防止无限挂起）。

---

## 6. 查看项目状态和指标

随时可以在项目根目录执行：

```bash
hc status
```

输出包括：

- Project ID
- Backend
- 各 agent 最近 10 次的成功率，如：

```text
Project ID: project-1234abcd
Backend: opencode

Agent success rates:
  orchestrator: 80.0%
  coder:       60.0%
  tester:      50.0%
  fixer:       75.0%
  reviewer:    100.0%
```

指标存放在：

```text
$HOME/.harnesscode/projects/<project_id>/learning/metrics.json
```

---

## 7. 恢复配置与卸载

### 7.1 恢复配置

如果某些配置文件在开发过程中被 agent 改动，并在 `.harnesscode/backup/` 下有备份，可以通过：

```bash
hc restore
```

来恢复。该命令会遍历 `.harnesscode/backup/`，将所有文件复制回项目根对应位置，并打印恢复的文件列表。

### 7.2 卸载本地数据

在项目根目录执行：

```bash
hc uninstall
```

当前 Go 版的行为：

- 删除当前项目的 `.harnesscode/` 目录
- 删除当前项目根下的 `dev-log.txt`（如果存在）
- **不会** 删除全局 `$HOME/.harnesscode/`
- **不会** 卸载系统中的 `opencode` / `claude` CLI

这是一种“轻量清理”，方便你重置项目内的 HarnessCode 状态而不影响其它项目。

---

## 8. 典型使用流程小结

1. 在本仓库内构建 `hc`：

   ```bash
   cd harnesscode-go
   go build ./cmd/hc
   ```

2. 将 `hc` 放入 PATH 或项目根目录。

3. 在业务项目根目录：

   ```bash
   hc init
   # （可选）编辑 .harnesscode/config.yaml，配置 webhook_url
   # （可选）预先写好 .harnesscode/feature_list.json
   hc start
   ```

4. 开发过程中：

   - 用 `hc status` 查看项目状态 & agent 成功率
   - 通过 Webhook 接收进度更新

5. 提交代码前，如需恢复配置：`hc restore`

6. 不再需要 HarnessCode 时，可用 `hc uninstall` 清理当前项目内的数据。

---

如果你打算在此基础上扩展功能（例如更复杂的报告格式、定制 agent 套件、HTTP 模型后端等），可以从 `internal` 各个包入手进行二次开发。
