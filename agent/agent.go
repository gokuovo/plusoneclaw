package agent

import (
	"context"       // 用于向 LLM 传递带超时/取消的 context
	"encoding/json" // 注册内置 memory tool 时构造 JSON Schema
	"fmt"           // 错误格式化与工具结果拼接
	"log/slog"      // 结构化日志
	"strings"       // 字符串操作

	"plusoneclaw/llm"  // LLM 接口和消息类型
	"plusoneclaw/tool" // 可执行工具系统（function calling）
)

// Agent 是框架的核心，它将 LLM、Tool、Skill、Context、Memory、Control 和 Parser 组合在一起，
// 通过 ReAct 循环（思考 → 行动 → 观察 → 重复）来完成目标。
//
// Tool 和 Skill 的区别：
//   - Tool（工具）：通过 LLM 的 function calling API 调用的可执行能力（如执行命令、读写文件）
//   - Skill（技能）：基于目录的指令集，通过 system prompt 注入，指导 Agent 完成特定领域任务
type Agent struct {
	llm              llm.LLM        // 语言模型客户端，负责与 API 通信
	tools            *tool.Registry // 可执行工具注册中心（function calling）
	skills           *SkillRegistry // 技能注册中心（prompt-based，遵循 OpenClaw Skills 规范）
	ctx              *Context       // 对话上下文，维护 system prompt 和历史消息
	memory           *Memory        // 持久化键值记忆，跨对话保留重要信息
	control          *Control       // 执行控制，限制迭代次数、超时和错误策略
	parser           *Parser        // 响应解析器，将 LLM 输出转为结构化 Action
	logger           *slog.Logger   // 结构化日志记录器
	baseSystemPrompt string         // 原始 system prompt（不含动态注入的 memory/skills），每次 Run 时作为基底重新组装
}

// Config 是创建 Agent 时的完整配置项。
type Config struct {
	LLM          llm.LLM        // 必填：LLM 客户端实例
	Tools        *tool.Registry // 可选：function calling 工具注册中心；nil 时自动创建空注册中心
	Skills       *SkillRegistry // 可选：技能注册中心；nil 时自动创建空注册中心
	SkillsDir    string         // 可选：技能发现目录路径；非空时自动扫描该目录下的 SKILL.md
	SystemPrompt string         // 可选：初始 system prompt，定义 Agent 的角色和行为准则
	MaxMessages  int            // 可选：对话历史最大保留条数；0 时默认使用 100
	MemoryPath   string         // 可选：持久化记忆文件路径；空字符串时使用纯内存模式
	Control      *Control       // 可选：执行控制配置；nil 时使用 DefaultControl()
	Logger       *slog.Logger   // 可选：自定义结构化日志记录器；nil 时使用 slog 默认实例
}

// New 根据配置创建并初始化一个 Agent 实例。
func New(cfg Config) *Agent {
	if cfg.Tools == nil {
		cfg.Tools = tool.NewRegistry() // 未提供时创建空注册中心
	}
	if cfg.Skills == nil {
		cfg.Skills = NewSkillRegistry() // 未提供时创建空注册中心
	}
	if cfg.Control == nil {
		cfg.Control = DefaultControl() // 未提供时使用默认控制配置
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default() // 未提供时使用 slog 全局默认实例
	}
	if cfg.MaxMessages == 0 {
		cfg.MaxMessages = 100 // 默认保留最近 100 条历史消息
	}

	// 自动发现技能目录
	if cfg.SkillsDir != "" {
		if err := cfg.Skills.Discover(cfg.SkillsDir); err != nil {
			cfg.Logger.Warn("skill discover error", "error", err)
		}
	}

	a := &Agent{
		llm:              cfg.LLM,
		tools:            cfg.Tools,
		skills:           cfg.Skills,
		ctx:              NewContext(cfg.SystemPrompt, cfg.MaxMessages), // 初始化对话上下文
		memory:           NewMemory(cfg.MemoryPath),                     // 初始化记忆（自动加载持久化数据）
		control:          cfg.Control,
		parser:           NewParser(), // 创建无状态解析器
		logger:           cfg.Logger,
		baseSystemPrompt: cfg.SystemPrompt, // 保存原始 system prompt，避免重复追加
	}

	a.registerMemoryTools() // 注册内置的 memory_save / memory_load 工具
	return a
}

// Run 是 Agent 的主入口：将用户输入加入上下文，然后执行 ReAct 循环直到得出最终答案。
// 每次循环：调用 LLM → 解析响应 → 执行工具或激活技能 → 将结果追回上下文 → 重复直到无动作。
func (a *Agent) Run(ctx context.Context, input string) (string, error) {
	loopCtx, cancel := a.control.WithContext(ctx) // 根据配置的 Timeout 包装 context
	defer cancel()                                // 确保超时 context 在 Run 退出后被释放

	a.rebuildSystemPrompt(input)                                  // 每次 Run 重建完整 system prompt（避免重复追加）
	a.ctx.Append(llm.Message{Role: llm.RoleUser, Content: input}) // 将用户输入加入对话历史

	var lastResponse string // 记录最近一次文本回答，超出迭代限制时返回此值

	for i := 0; ; i++ {
		if err := a.control.CheckIteration(i); err != nil { // 检查是否超过最大迭代次数
			a.logger.Warn("iteration limit reached", "error", err)
			if lastResponse != "" {
				return lastResponse, nil // 已有部分回答时优先返回，而非报错
			}
			return "", err
		}

		if a.control.BeforeStep != nil {
			a.control.BeforeStep(i) // 执行用户注册的步骤前钩子
		}

		tools := a.tools.Tools()                                  // 获取当前所有可用工具描述（function calling 格式）
		resp, err := a.llm.Chat(loopCtx, a.ctx.Messages(), tools) // 调用 LLM
		if err != nil {
			switch a.control.HandleError(err) { // 根据策略决定如何处理 LLM 错误
			case ErrorContinue, ErrorRetry:
				a.logger.Warn("llm error, continuing", "error", err) // 记录错误但继续循环
				continue
			default:
				return "", fmt.Errorf("llm error: %w", err) // ErrorAbort：终止并返回错误
			}
		}

		actions := a.parser.Parse(resp) // 将 LLM 响应解析为 Action 列表

		hasToolCall := false // 标记本轮是否有工具调用，决定是否继续循环
		for _, act := range actions {
			switch act.Type {
			case ActionToolCall:
				hasToolCall = true
				a.logger.Info("tool call", "tool", act.ToolName)

				// 将包含 tool_calls 的 assistant 消息追加到历史，这是 OpenAI 协议要求的
				a.ctx.Append(llm.Message{
					Role:             llm.RoleAssistant,
					Content:          resp.Content,          // 助手可能在 tool call 之前有文本内容
					ReasoningContent: resp.ReasoningContent, // Kimi thinking 推理内容，必须回传
					ToolCalls: []llm.ToolCall{{
						ID:   act.ToolID,
						Type: "function",
						Function: llm.ToolCallFunc{
							Name:      act.ToolName,
							Arguments: string(act.ToolArgs),
						},
					}},
				})

				result, execErr := a.tools.Execute(loopCtx, act.ToolName, act.ToolArgs) // 执行 Tool
				if execErr != nil {
					result = fmt.Sprintf("Error: %v", execErr) // 将错误信息作为工具结果返回给 LLM
					a.logger.Warn("tool execution error", "tool", act.ToolName, "error", execErr)
				}

				// 将工具执行结果以 tool 角色追加，LLM 下一轮将看到此结果
				a.ctx.Append(llm.Message{
					Role:       llm.RoleTool,
					Content:    result,
					ToolCallID: act.ToolID, // 必须与对应 tool call 的 ID 一致
				})

			case ActionAnswer:
				lastResponse = act.Content // 记录文本回答（可能在工具调用前出现）
			}
		}

		if a.control.AfterStep != nil {
			a.control.AfterStep(i, lastResponse) // 执行用户注册的步骤后钩子
		}

		if !hasToolCall {
			return lastResponse, nil // 本轮没有工具调用，说明 LLM 已给出最终答案
		}
		// 有工具调用时继续下一轮，让 LLM 处理工具执行结果
	}
}

// Chat 是单轮对话的简化接口：不启用工具调用，直接返回 LLM 的文本回复。
// 适用于不需要 ReAct 能力的简单问答场景。
func (a *Agent) Chat(ctx context.Context, input string) (string, error) {
	a.ctx.Append(llm.Message{Role: llm.RoleUser, Content: input}) // 加入用户消息
	resp, err := a.llm.Chat(ctx, a.ctx.Messages(), nil)           // 调用 LLM，不传 tools
	if err != nil {
		return "", err
	}
	a.ctx.Append(llm.Message{Role: llm.RoleAssistant, Content: resp.Content}) // 将回复加入历史
	return resp.Content, nil
}

func (a *Agent) Tools() *tool.Registry  { return a.tools }  // Tools 返回工具注册中心
func (a *Agent) Skills() *SkillRegistry { return a.skills } // Skills 返回技能注册中心
func (a *Agent) Memory() *Memory        { return a.memory } // Memory 返回记忆存储
func (a *Agent) Context() *Context      { return a.ctx }    // Context 返回对话上下文

// SetLLM 在运行时切换 LLM 客户端，用于 /model 命令热切换模型。
func (a *Agent) SetLLM(l llm.LLM) { a.llm = l }

// rebuildSystemPrompt 在每次 Run 开始时，以 baseSystemPrompt 为基底重新组装完整的 system prompt。
// 避免多次 Run() 调用时 memory 和 skills 块重复累积。
func (a *Agent) rebuildSystemPrompt(input string) {
	prompt := a.baseSystemPrompt // 从原始 system prompt 开始重建

	// --- 注入记忆 ---
	mem := a.memory.Dump() // 获取所有记忆的格式化文本
	if mem != "" {
		prompt += "\n\n<memory>\n" + mem + "</memory>" // 将记忆注入 system prompt 尾部
	}

	// --- 注入技能 ---
	if err := a.skills.Reload(); err != nil { // 重新扫描技能目录（自动拾取新增技能）
		a.logger.Warn("skill reload error", "error", err)
	}
	var sections []string // 收集所有技能注入块

	// 自动激活：匹配 triggers 的技能直接注入正文
	activated := a.skills.ActivatedSkillsPrompt(input)
	if activated != "" {
		sections = append(sections, activated) // 追加已激活技能的完整指令
		a.logger.Info("skills auto-activated")
	}

	// 可用技能列表（供 Agent 手动读取未自动激活的技能）
	available := a.skills.AvailableSkillsPrompt()
	if available != "" {
		sections = append(sections, available+"\n\n"+
			"When a user's task matches an available skill that was not auto-activated, "+
			"read the skill's full instructions from the <location> path using the read_file tool. "+
			"Skills listed in <activated_skills> are already loaded — do NOT read them again.")
	}

	if len(sections) > 0 {
		prompt += "\n\n" + strings.Join(sections, "\n\n") // 将技能块拼接到 prompt 尾部
	}

	a.ctx.SetSystemPrompt(prompt) // 原子替换 system prompt，不会累积
}

// registerMemoryTools 向工具注册中心注册两个内置记忆工具：
// memory_save（保存）和 memory_load（读取），供 LLM 通过 function calling 调用。
func (a *Agent) registerMemoryTools() {
	// memory_save：LLM 可调用此工具将重要信息存入持久化记忆
	a.tools.Register(&tool.FuncTool{
		ToolName: "memory_save",
		Desc:     "Save a key-value pair to persistent memory for later recall.",
		Params: json.RawMessage(`{
			"type": "object",
			"properties": {
				"key":   {"type": "string", "description": "The memory key"},
				"value": {"type": "string", "description": "The value to remember"}
			},
			"required": ["key", "value"]
		}`),
		Fn: func(ctx context.Context, params json.RawMessage) (string, error) {
			var p struct {
				Key   string `json:"key"`   // 记忆键名
				Value string `json:"value"` // 记忆值
			}
			if err := json.Unmarshal(params, &p); err != nil { // 解析 LLM 传入的参数
				return "", err
			}
			if err := a.memory.Save(p.Key, p.Value); err != nil { // 持久化到磁盘
				return "", err
			}
			return fmt.Sprintf("Saved: %s = %s", p.Key, p.Value), nil // 返回确认信息给 LLM
		},
	})

	// memory_load：LLM 可调用此工具从持久化记忆中读取之前存储的值
	a.tools.Register(&tool.FuncTool{
		ToolName: "memory_load",
		Desc:     "Load a value from persistent memory by key.",
		Params: json.RawMessage(`{
			"type": "object",
			"properties": {
				"key": {"type": "string", "description": "The memory key to retrieve"}
			},
			"required": ["key"]
		}`),
		Fn: func(ctx context.Context, params json.RawMessage) (string, error) {
			var p struct {
				Key string `json:"key"` // 要读取的记忆键名
			}
			if err := json.Unmarshal(params, &p); err != nil { // 解析参数
				return "", err
			}
			v, ok := a.memory.Load(p.Key) // 从记忆中查找
			if !ok {
				return fmt.Sprintf("No memory found for key: %s", p.Key), nil // 未找到时告知 LLM
			}
			return v, nil // 返回找到的值
		},
	})
}
