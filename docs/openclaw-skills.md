# OpenClaw Skills 规范与教程

> OpenClaw Skills 是 PlusOneClaw Agent 框架的技能系统规范，定义了如何编写、组织和激活基于目录的 Agent 技能。

## 目录

- [概念](#概念)
- [快速开始](#快速开始)
- [SKILL.md 规范](#skillmd-规范)
  - [Frontmatter 字段](#frontmatter-字段)
  - [正文（Body）](#正文body)
- [目录结构约定](#目录结构约定)
- [激活机制](#激活机制)
  - [自动激活（Trigger）](#自动激活trigger)
  - [手动激活（Read File）](#手动激活read-file)
- [System Prompt 注入格式](#system-prompt-注入格式)
- [完整示例](#完整示例)
- [最佳实践](#最佳实践)
- [与 agentskills.io 的区别](#与-agentskillsio-的区别)

---

## 概念

**Skill（技能）** 与 **Tool（工具）** 是 PlusOneClaw 中两种不同的能力扩展方式：

| | Tool（工具） | Skill（技能） |
|---|---|---|
| **机制** | LLM function calling API | System prompt 注入指令 |
| **定义方式** | Go 代码注册 `tool.FuncTool` | 目录 + `SKILL.md` 文件 |
| **用途** | 可执行原子操作（shell、读写文件） | 领域知识 / 流程指令（代码审查、测试编写） |
| **激活方式** | LLM 通过 `tool_calls` 调用 | triggers 自动激活 或 LLM 手动 `read_file` |

简而言之：**Tool 是 Agent 的手脚，Skill 是 Agent 的经验和知识。**

---

## 快速开始

### 1. 创建技能目录

```bash
mkdir -p skills/my-skill
```

### 2. 编写 SKILL.md

```markdown
---
name: my-skill
description: 简短描述这个技能做什么，以及何时应该使用它。
version: "1.0"
tags: [示例, 教程]
tools: [read_file, shell]
triggers: [关键词1, 关键词2]
---

# 技能标题

这里写技能的完整指令，告诉 Agent 如何一步步完成任务。

## Step 1: 理解任务
...

## Step 2: 执行操作
...
```

### 3. 启用技能发现

在 `main.go` 中配置 `SkillsDir`：

```go
a := agent.New(agent.Config{
    LLM:       client,
    Tools:     tools,
    SkillsDir: "skills",  // 指向技能根目录
    // ...
})
```

Agent 启动时会自动扫描 `skills/` 目录下所有包含 `SKILL.md` 的子目录。

---

## SKILL.md 规范

每个技能由一个目录定义，目录中**必须**包含 `SKILL.md` 文件。该文件由 YAML frontmatter 和 Markdown 正文两部分组成。

### Frontmatter 字段

Frontmatter 使用 `---` 包裹的 YAML 格式：

```yaml
---
name: code-review
description: >
  Reviews code for quality, style, and potential issues.
  Use when asked to review code, check for bugs, or improve code quality.
version: "1.0"
author: Your Name
tags: [code-quality, review, best-practices]
tools: [read_file, shell]
triggers: [review, code review, 代码审查, 代码检查]
---
```

| 字段 | 必填 | 类型 | 说明 |
|------|------|------|------|
| `name` | ✅ | string | 技能唯一标识名，**必须与目录名一致** |
| `description` | ✅ | string | 技能描述，Agent 据此判断何时激活。应包含"何时使用"的提示 |
| `version` | ❌ | string | 语义化版本号（如 `"1.0"`、`"2.1.3"`） |
| `author` | ❌ | string | 技能作者 |
| `tags` | ❌ | list | 分类标签，用于组织和检索 |
| `tools` | ❌ | list | 该技能依赖的工具列表，便于 Agent 了解所需能力 |
| `triggers` | ❌ | list | 自动激活触发词，用户输入包含这些词时自动注入技能正文 |

**关于 `triggers`：**

- 匹配规则：大小写不敏感的子字符串匹配
- 支持中英文混合触发词
- 用户输入只需包含**任意一个**触发词即可激活
- 不设置 triggers 时，技能仅支持手动激活

**关于 `description`：**

好的 description 应该同时说明：
1. 这个技能**做什么**
2. **何时**应该使用它

```yaml
# ✅ 好的 description
description: Reviews code for quality, style, and potential issues. Use when asked to review code, check for bugs, or improve code quality.

# ❌ 不好的 description
description: Code review skill.
```

### 正文（Body）

Frontmatter 之后的 Markdown 内容即为技能正文，是给 Agent 的完整工作指令。

正文应包含：
1. **任务分解**：将复杂任务拆为明确的步骤
2. **具体方法**：每步怎么做、用哪些工具
3. **输出格式**：期望的输出结构
4. **注意事项**：常见陷阱和约束

```markdown
# Code Review

When reviewing code, follow these steps systematically:

## Step 1: Understand the Context
- Read the file(s) to be reviewed using the `read_file` tool
- Identify the programming language and framework

## Step 2: Check for Issues
### Correctness
- Logic errors and off-by-one mistakes
- Null/nil pointer dereferences

### Security
- Input validation and sanitization
- SQL injection, XSS vulnerabilities

## Step 3: Provide Feedback
Structure your review as:
1. **Summary**: One-paragraph overview
2. **Issues**: List with severity (Critical / Warning / Info)
3. **Suggestions**: Actionable improvement recommendations
```

---

## 目录结构约定

```
skills/
├── code-review/                  # 技能目录名 = 技能 name
│   ├── SKILL.md                  # 必需：技能定义文件
│   ├── examples/                 # 可选：示例文件
│   │   ├── good-review.md        #   优秀审查报告示例
│   │   └── bad-patterns.md       #   常见反模式示例
│   ├── templates/                # 可选：输出模板
│   │   └── review-report.md      #   审查报告模板
│   ├── scripts/                  # 可选：可执行脚本
│   │   └── lint.sh               #   代码静态检查脚本
│   └── references/               # 可选：参考资料
│       └── style-guide.md        #   编码风格指南
├── test-helper/
│   ├── SKILL.md
│   └── templates/
│       └── test-template.go
└── data-analysis/
    ├── SKILL.md
    ├── scripts/
    │   └── analyze.py
    └── examples/
        └── sample-report.md
```

### 子目录说明

| 目录 | 用途 | 使用方式 |
|------|------|---------|
| `examples/` | Few-shot 示例，帮助 Agent 理解期望的输出格式 | 在 SKILL.md 正文中引用：`参见 examples/good-review.md` |
| `templates/` | 输出模板，Agent 可用于生成格式化输出 | Agent 通过 `read_file` 工具读取 |
| `scripts/` | 辅助脚本，Agent 可通过 `shell` 工具执行 | Agent 通过 `shell` 工具执行 |
| `references/` | 参考资料，为 Agent 提供背景知识 | Agent 通过 `read_file` 工具按需读取 |

Agent 可通过 `Skill.ReadFile(relPath)` 方法（或 `read_file` 工具配合技能目录路径）读取这些资源文件。

---

## 激活机制

OpenClaw Skills 支持两种激活模式，均在每次 `Agent.Run()` 调用时自动处理。

### 自动激活（Trigger）

当技能定义了 `triggers` 字段时，框架会在用户输入中检查这些关键词。匹配成功的技能，其**完整正文**会自动注入到 system prompt 的 `<activated_skills>` 块中。

**流程：**
```
用户输入 "帮我做一下代码审查"
    ↓
框架匹配到 triggers: ["代码审查"]
    ↓
自动加载 code-review 的 SKILL.md 正文
    ↓
注入 <activated_skills> 块到 system prompt
    ↓
Agent 直接按照技能指令执行，无需额外操作
```

**优点：** 零延迟激活，无需消耗一个 LLM 调用来读取技能文件

**注意事项：**
- triggers 应设置为具有区分度的关键词，避免误触发
- 自动激活的技能正文会占用 system prompt 上下文窗口
- 适合高频使用、正文较短（< 2000 token）的技能

### 手动激活（Read File）

未设置 triggers 或未被自动激活的技能，Agent 仍可通过 `<openclaw_skills>` 列表看到它们的摘要信息（name + description），并在判断任务匹配时，使用 `read_file` 工具读取 `<location>` 指向的 SKILL.md 文件获取完整指令。

**流程：**
```
Agent 在 system prompt 中看到可用技能列表
    ↓
Agent 判断当前任务匹配某个技能
    ↓
Agent 调用 read_file 读取技能的 SKILL.md
    ↓
Agent 按照技能指令执行
```

**优点：** 不占用常驻 system prompt 空间，适合低频使用或正文很长的技能

---

## System Prompt 注入格式

框架会自动将技能信息注入 Agent 的 system prompt，格式如下：

### 可用技能列表

所有已注册技能的摘要信息，始终注入：

```xml
<openclaw_skills>
<skill>
  <name>code-review</name>
  <description>Reviews code for quality, style, and potential issues.</description>
  <tags>code-quality, review, best-practices</tags>
  <tools>read_file, shell</tools>
  <location>/path/to/skills/code-review/SKILL.md</location>
</skill>
<skill>
  <name>test-helper</name>
  <description>Helps write unit tests for Go code.</description>
  <tags>testing, go, unit-test</tags>
  <tools>read_file, write_file, shell</tools>
  <location>/path/to/skills/test-helper/SKILL.md</location>
</skill>
</openclaw_skills>
```

### 自动激活技能正文

当 triggers 匹配时，额外注入：

```xml
<activated_skills>
<skill name="code-review">
# Code Review

When reviewing code, follow these steps systematically:
...（SKILL.md 的完整正文）
</skill>
</activated_skills>
```

---

## 完整示例

### 示例 1：代码审查技能

```
skills/code-review/
├── SKILL.md
└── templates/
    └── review-report.md
```

**SKILL.md：**

```markdown
---
name: code-review
description: Reviews code for quality, style, and potential issues. Use when asked to review code, check for bugs, or improve code quality.
version: "1.0"
tags: [code-quality, review, best-practices]
tools: [read_file, shell]
triggers: [review, code review, 代码审查, 代码检查]
---

# Code Review

When reviewing code, follow these steps systematically:

## Step 1: Understand the Context
- Read the file(s) to be reviewed using the `read_file` tool
- Identify the programming language and framework
- Understand the purpose of the code

## Step 2: Check for Issues

### Correctness
- Logic errors and off-by-one mistakes
- Null/nil pointer dereferences
- Unhandled error cases

### Security
- Input validation and sanitization
- SQL injection, XSS, or other injection vulnerabilities
- Hardcoded secrets or credentials

### Performance
- Unnecessary allocations or copies
- N+1 query problems
- Unbounded growth (memory leaks)

### Style & Maintainability
- Naming conventions (clear, consistent)
- Function length (prefer < 30 lines)
- Code duplication

## Step 3: Provide Feedback
Structure your review as:
1. **Summary**: One-paragraph overview of the code quality
2. **Issues**: List each issue with severity (Critical / Warning / Info)
3. **Suggestions**: Actionable improvement recommendations
4. **Positive**: Highlight what's done well

## Guidelines
- Be constructive, not dismissive
- Provide specific line references when possible
- Suggest concrete fixes, not just "this is wrong"
- Prioritize critical issues over style nitpicks
```

### 示例 2：测试编写技能（含 templates）

```
skills/test-helper/
├── SKILL.md
└── templates/
    └── table-test.go
```

**SKILL.md：**

```markdown
---
name: test-helper
description: Helps write unit tests and test cases for Go code. Use when asked to write tests, add test coverage, or explain how to test a function.
version: "1.0"
tags: [testing, go, unit-test]
tools: [read_file, write_file, shell]
triggers: [test, unit test, 测试, 单元测试, test coverage, 测试覆盖]
---

# Test Helper

当被要求编写测试代码时，遵循以下步骤：

## Step 1: 读取目标代码
使用 `read_file` 工具读取要测试的源文件，理解函数签名、边界条件和依赖关系。

## Step 2: 确定测试策略
- 纯函数：直接测试输入输出（table-driven tests）
- 依赖外部资源：使用接口 + `t.TempDir()`

## Step 3: 覆盖关键场景
| 场景 | 举例 |
|------|------|
| 正常路径 | 有效输入 → 预期输出 |
| 边界值 | 空字符串、nil、0 |
| 错误路径 | 非法输入 → 返回 error |

## Step 4: 生成测试文件
测试文件放在与源文件同一目录，命名为 `xxx_test.go`。
```

### 示例 3：仅手动激活的大型技能

```markdown
---
name: architecture-review
description: Performs comprehensive architecture review for Go projects. Use when asked to review system design, analyze dependencies, or suggest architectural improvements.
version: "1.0"
tags: [architecture, design, go]
tools: [read_file, shell]
# 注意：不设置 triggers，因为正文很长，不适合自动注入
---

# Architecture Review

（很长的架构审查指令...）
```

---

## 最佳实践

### 命名规范
- 目录名使用 **kebab-case**（如 `code-review`、`test-helper`）
- 目录名**必须**与 frontmatter 中的 `name` 一致
- 使用描述性名称，避免缩写

### Description 编写
- 同时描述**做什么**和**何时用**
- 保持在 1-2 句话以内（发现阶段加载到 system prompt，过长浪费 token）
- 使用英文编写（便于 LLM 理解）

### Triggers 设计
- 选择**高区分度**的关键词，避免常见词（如 "help"、"how"）
- 包含中英文双语触发词以支持多语言用户
- 数量控制在 3-8 个，过多可能导致误触发
- 如果技能正文超过 2000 token，考虑不设置 triggers（使用手动激活）

### 正文编写
- 使用**步骤化**结构（Step 1、Step 2...）
- 明确指出要使用哪些**工具**（`read_file`、`shell` 等）
- 提供**输出格式**规范
- 控制正文在 **5000 token 以内**（过长的正文会占用过多上下文窗口）

### 资源文件
- 将 few-shot 示例放在 `examples/` 目录
- 将可复用模板放在 `templates/` 目录
- 将辅助脚本放在 `scripts/` 目录
- 在正文中通过相对路径引用这些文件

---

## 与 agentskills.io 的区别

OpenClaw Skills 在 [agentskills.io](https://agentskills.io) 的基础上进行了扩展和改进：

| 特性 | agentskills.io | OpenClaw Skills |
|------|----------------|-----------------|
| Frontmatter 字段 | `name`、`description` | 新增 `version`、`author`、`tags`、`tools`、`triggers` |
| 激活方式 | 仅手动（Agent 用 read_file 读取） | 自动激活（triggers）+ 手动激活 双模式 |
| Prompt 格式 | `<available_skills>` XML 块 | `<openclaw_skills>` + `<activated_skills>` 双层结构 |
| 目录约定 | `scripts/`、`references/` | 新增 `examples/`、`templates/` 标准子目录 |
| 元数据丰富度 | 最小化 | 包含 tags（分类）、tools（依赖）、version（版本管理） |
| 热重载 | 支持 | 支持（`Reload()` 自动拾取新增技能） |

---

## API 参考

### Go 代码中使用技能

```go
// 创建技能注册中心
skills := skill.NewRegistry()

// 自动发现 skills/ 目录下的所有技能
skills.Discover("skills")

// 手动注册技能（无需磁盘文件）
skills.Register(&skill.Skill{
    Name:        "inline-skill",
    Description: "A programmatically registered skill.",
    Triggers:    []string{"inline"},
})

// 查询技能
s, ok := skills.Get("code-review")
if ok {
    body, _ := s.LoadBody()           // 加载正文
    content, _ := s.ReadFile("examples/good-review.md") // 读取资源文件
}

// 生成 system prompt 注入内容
available := skills.AvailableSkillsPrompt()     // 可用技能摘要列表
activated := skills.ActivatedSkillsPrompt(input) // 根据用户输入自动激活
matched := skills.MatchTriggers(input)            // 获取匹配的技能列表
```

### 在 Agent 配置中启用

```go
a := agent.New(agent.Config{
    LLM:       client,
    Tools:     tools,
    SkillsDir: "skills",  // 自动发现此目录下所有技能
    // ...
})
```

Agent 在每次 `Run()` 时会：
1. 调用 `Reload()` 重新扫描技能目录（拾取新增/修改的技能）
2. 通过 `ActivatedSkillsPrompt(input)` 自动激活匹配的技能
3. 通过 `AvailableSkillsPrompt()` 注入所有技能的摘要列表
