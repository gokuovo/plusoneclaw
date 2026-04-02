![PlusOneClaw Logo](docs/plusoneclaw-logo.png)

# PlusOneClaw

> 一个极简的 Go 语言 **助手 Agent 模版**，搭建了 Agent 所需的最小可用架构。

## 介绍

PlusOneClaw 不追求功能大而全，只做一件事：**提供一套清晰、可直接二次开发的 Agent 骨架**。

整个框架由七个核心模块构成，每个模块职责单一、边界清晰：

- **LLM** — 对接多家模型 API，统一调用接口
- **Loop** — ReAct 循环驱动（think → act → observe → repeat）
- **Skill** — 基于目录的技能系统，按需注入 prompt 指令
- **Context** — 管理 system prompt、对话历史与滑动窗口裁剪
- **Memory** — KV 持久化，跨对话保留重要信息
- **Parser** — 解析 LLM 输出，优先 native tool call，fallback 文本解析
- **Control** — 控制迭代次数、超时、错误策略与生命周期钩子

代码量极少，没有复杂抽象，适合直接阅读和修改。

### 适合的场景

| 场景 | 说明 |
|------|------|
| **个人助手 / 本地 Bot** | 快速搭建一个有记忆、会用工具的私人助手 |
| **学习 Agent 架构** | 代码结构清晰，适合理解 ReAct 循环和 function calling 的底层实现 |
| **二次开发模版** | 直接 fork，在骨架上叠加自己的业务工具和技能，而不是从零开始 |
| **多模型对比测试** | 内置 6 个原生客户端，可随时切换模型测试效果差异 |
| **轻量生产部署** | 单二进制 + Web Server，无额外依赖，容器化或直接运行均可 |

不适合的场景：需要 RAG、复杂 multi-agent 编排、向量数据库、自动 plan 生成等重型 Agent 能力时，建议选择更完整的框架（如 LangChain、AutoGen）。

## 架构

```
┌──────────────────────────────────────────────────┐
│                      Agent                        │
│                                                    │
│   ┌──────────┐ ┌─────────┐ ┌──────────┐ ┌──────┐ │
│   │   LLM    │ │  Tool   │ │  Skill   │ │Memory│ │
│   │(MiniMax/ │ │Registry │ │ Registry │ │ (KV) │ │
│   │ Claude/  │ │(fn call)│ │ (prompt) │ │      │ │
│   │ Gemini/  │ └────┬────┘ └────┬─────┘ └──┬───┘ │
│   │ OpenAI/  │      │           │           │     │
│   │ Kimi/GLM)│      │           │           │     │
│   └────┬─────┘      │           │           │     │
│        │            │           │           │     │
│   ┌────┴────────────┴───────────┴───────────┴──┐  │
│   │               Loop (ReAct)                  │  │
│   │   think → act → observe → ...               │  │
│   └────┬────────────────────────────────────┬───┘  │
│        │                                    │      │
│   ┌────┴────┐                       ┌───────┴──┐   │
│   │ Parser  │                       │ Control  │   │
│   │(native +│                       │(iter/time│   │
│   │ text fb)│                       │ /error)  │   │
│   └─────────┘                       └──────────┘   │
│                                                    │
│   ┌────────────────────────────────────────────┐   │
│   │               Context                       │   │
│   │  system prompt + history + memory + skills  │   │
│   └────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
                          │
              ┌───────────┴───────────┐
              │       Web Server      │
              │  /api/chat   :8080    │
              │  /api/models          │
              └───────────────────────┘
```

## 核心模块

| 模块          | 职责                                                                                  |
| ----------- | ----------------------------------------------------------------------------------- |
| **LLM**     | 6 个原生模型客户端（MiniMax / Claude / Gemini / OpenAI / Kimi / GLM），各用原生 API，支持多模态          |
| **Tool**    | 可执行工具系统（function calling），内置 shell、read\_file、write\_file、memory\_save、memory\_load |
| **Skill**   | 基于目录的技能系统，遵循 OpenClaw Skills 规范，支持 trigger 自动激活和 read\_file 手动激活                    |
| **Loop**    | ReAct 循环：调用 LLM → 解析响应 → 执行 Tool → 回传结果 → 重复                                        |
| **Context** | 管理 system prompt + 对话历史 + 记忆 + 技能列表，自动裁剪滑动窗口                                        |
| **Memory**  | 极简 KV 持久化存储（JSON 文件），自动注入 system prompt                                             |
| **Control** | 迭代上限、超时、错误策略、BeforeStep/AfterStep 生命周期钩子                                            |
| **Parser**  | 解析 LLM 输出：优先 native tool calling，fallback 文本格式解析                                    |

## 快速开始

```bash
# 编译
go build -o plusoneclaw

# 守护模式：Web 服务器常驻 http://localhost:8080
./plusoneclaw

# REPL 模式：连接到已运行的服务器的交互式命令行
./plusoneclaw repl
```

## 配置

### config.yaml

```yaml
server:
  addr: ":8080"

log:
  level: debug      # debug | info | warn | error
  format: text      # json（生产）| text（本地开发）

agent:
  system_prompt_file: "config/system-prompt.txt"  # 从文件加载 system prompt
  skills_dir: "skills"                             # 技能自动发现目录
  max_messages: 50                                 # 对话历史最大保留条数
  memory_path: ".plusoneclaw/memory.json"          # 持久化记忆文件路径
  max_iterations: 15                               # Agent Loop 最大迭代次数
```

所有字段均支持环境变量覆盖：`SERVER_ADDR`、`LOG_LEVEL`、`LOG_FORMAT`。

### models.json

通过 `models.json` 管理所有模型，每个模型使用自己的原生 API 客户端：

```json
{
  "models": [
    {"name": "minimax", "type": "minimax",   "api_key": "sk-...",                "base_url": "https://api.minimaxi.com/v1",               "model": "MiniMax-M2.7"},
    {"name": "claude",  "type": "anthropic", "api_key_env": "ANTHROPIC_API_KEY", "base_url": "https://api.anthropic.com",                 "model": "claude-sonnet-4-20250514"},
    {"name": "gemini",  "type": "gemini",    "api_key_env": "GEMINI_API_KEY",    "base_url": "https://generativelanguage.googleapis.com", "model": "gemini-2.5-flash"},
    {"name": "openai",  "type": "openai",    "api_key_env": "OPENAI_API_KEY",    "base_url": "https://api.openai.com/v1",                 "model": "gpt-4o-mini"},
    {"name": "kimi",    "type": "kimi",      "api_key_env": "MOONSHOT_API_KEY",  "base_url": "https://api.moonshot.cn/v1",                "model": "kimi-latest"},
    {"name": "glm",     "type": "glm",       "api_key_env": "ZHIPU_API_KEY",     "base_url": "https://open.bigmodel.cn/api/paas/v4",      "model": "glm-4-flash"}
  ],
  "default": "minimax"
}
```

API Key 支持两种方式（二选一）：

- `api_key`：直接写死 key（本地调试用）
- `api_key_env`：填环境变量名，程序启动时读取（推荐，避免泄露）

REPL 模型切换命令：

```
> /model              # 列出所有可用模型（* 标记当前）
> /model claude       # 切换到 Claude
> /model kimi         # 切换到 Kimi
```

### System Prompt

System prompt 独立存放于 `config/system-prompt.txt`，支持任意长度，直接编辑即可，无需修改代码。

## Tool 与 Skill 的区别

| <br />   | Tool（工具）                 | Skill（技能）                                    |
| -------- | ------------------------ | -------------------------------------------- |
| **机制**   | LLM function calling API | System prompt 注入指令                           |
| **定义**   | Go 代码注册（`tool.FuncTool`） | 目录 + `SKILL.md`（YAML frontmatter + Markdown） |
| **用途**   | 可执行原子操作（shell、读写文件）      | 领域知识/流程指令（代码审查、测试编写）                         |
| **激活**   | LLM 通过 `tool_calls` 调用   | triggers 自动激活 或 LLM 用 `read_file` 手动激活       |
| **数量建议** | 单 Agent 不超过 20 个         | 不限，按需激活                                      |

## 扩展 Tool

在 `tool/builtin.go` 中添加新工具，或在 `main.go` 中直接注册：

```go
tools.Register(&tool.FuncTool{
    ToolName: "search_web",
    Desc:     "Search the web for information.",
    Params: json.RawMessage(`{
        "type": "object",
        "properties": {
            "query": {"type": "string", "description": "Search query"}
        },
        "required": ["query"]
    }`),
    Fn: func(ctx context.Context, params json.RawMessage) (string, error) {
        var p struct{ Query string `json:"query"` }
        json.Unmarshal(params, &p)
        return "search results", nil
    },
})
```

## 创建 Skill

遵循 OpenClaw Skills 规范（详见 `docs/openclaw-skills.md`），在 `skills/` 目录下创建子目录：

```
skills/
└── my-skill/
    └── SKILL.md
```

`SKILL.md` 格式：

```markdown
---
name: code-review
description: Reviews code for quality, style, and potential issues.
version: "1.0"
tags: [code-quality, review]
tools: [read_file, shell]
triggers: [review, code review, 代码审查]
---

# Code Review

When reviewing code, follow these steps:
1. Read the file(s) using the read_file tool
2. Check for correctness, security, performance issues
3. Provide structured feedback with severity levels
```

## 多模态消息

`Message` 支持 `Parts` 字段携带图片，各客户端自动转换为原生格式：

```go
msg := llm.Message{
    Role: llm.RoleUser,
    Parts: []llm.ContentPart{
        {Type: "text", Text: "这张图里有什么？"},
        {Type: "image", MediaType: "image/png", Data: base64EncodedPNG},
    },
}
```

| 客户端                           | 图片格式                                                                   |
| ----------------------------- | ---------------------------------------------------------------------- |
| OpenAI / MiniMax / Kimi / GLM | `content: [{type:"image_url", image_url:{url:"data:...;base64,..."}}]` |
| Anthropic                     | `content: [{type:"image", source:{type:"base64", ...}}]`               |
| Gemini                        | `parts: [{inlineData: {mimeType:"...", data:"..."}}]`                  |

## 自定义 Control

```go
agent.New(agent.Config{
    Control: &agent.Control{
        MaxIterations: 10,
        Timeout:       2 * time.Minute,
        StopCheck: func(resp string) bool {
            return strings.Contains(resp, "DONE")
        },
        OnError: func(err error) agent.ErrorAction {
            return agent.ErrorRetry // 或 ErrorContinue / ErrorAbort
        },
        BeforeStep: func(i int) { log.Printf("Step %d", i) },
    },
})
```

## 项目结构

```
plusoneclaw/
├── main.go                    # 入口：子命令路由 + Tool 注册 + Web/REPL 启动
├── models.json                # 模型配置（6 个原生客户端，运行时热切换）
├── config.yaml                # 服务器、日志、Agent 行为配置
│
├── agent/                     # Agent 核心（所有 Agent 运行时逻辑）
│   ├── agent.go               #   核心编排器 + ReAct Loop
│   ├── context.go             #   对话上下文管理（滑动窗口历史）
│   ├── control.go             #   执行控制（迭代/超时/错误策略/钩子）
│   ├── memory.go              #   极简 KV 持久化记忆（JSON 文件）
│   ├── parser.go              #   LLM 输出解析（native + 文本 fallback）
│   └── skill.go               #   技能系统（发现/注册/触发/注入 prompt）
│
├── config/                    # 配置与初始化
│   ├── config.go              #   YAML 加载 + 环境变量覆盖
│   ├── logger.go              #   全局 slog 初始化（JSON/text 格式）
│   └── system-prompt.txt      #   默认 system prompt（可任意编辑）
│
├── llm/                       # LLM 客户端适配层
│   ├── message.go             #   统一接口 + 消息类型（含多模态 ContentPart）
│   ├── openai.go              #   OpenAI 原生客户端
│   ├── anthropic.go           #   Anthropic Claude 原生客户端
│   ├── gemini.go              #   Google Gemini 原生客户端
│   ├── minimax.go             #   MiniMax 原生客户端
│   ├── kimi.go                #   Moonshot Kimi 原生客户端
│   └── glm.go                 #   智谱 GLM 原生客户端
│
├── tool/                      # 工具系统
│   ├── tool.go                #   Tool 接口 + 线程安全注册中心
│   └── builtin.go             #   内置工具（shell / read_file / write_file）
│
├── web/                       # Web 服务器
│   ├── web.go                 #   REST API（/api/chat、/api/models）
│   └── static/
│       └── index.html         #   暗色主题聊天 UI
│
├── skills/                    # 技能内容目录（运行时自动扫描）
│   ├── code-review/
│   │   └── SKILL.md
│   └── test-helper/
│       └── SKILL.md
│
└── docs/
    └── openclaw-skills.md     # OpenClaw Skills 规范文档
```

## License

This project is licensed under the MIT License.
