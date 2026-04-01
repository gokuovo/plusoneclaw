package agent

import (
	"encoding/json" // 用于验证和包装 JSON 参数
	"fmt"           // 生成文本模式下的 tool call ID
	"regexp"        // 正则解析文本格式的工具调用
	"strings"       // 去除空白字符

	"plusoneclaw/llm" // 引用 Response 类型
)

// ActionType 枚举 LLM 响应解析后可能产生的动作类型。
type ActionType int

const (
	ActionAnswer   ActionType = iota // 最终文本回答，Loop 应在此后返回
	ActionToolCall                   // 工具调用请求（function calling），Loop 应执行对应 Tool 并将结果回传
)

// Action 表示从 LLM 响应中解析出的一个原子动作。
type Action struct {
	Type     ActionType      // 动作类型
	Content  string          // ActionAnswer 时的文本内容
	ToolName string          // ActionToolCall 时的工具名称
	ToolArgs json.RawMessage // ActionToolCall 时的 JSON 参数（原始字节，不做解析）
	ToolID   string          // 工具调用 ID，用于将 tool 结果消息与调用请求关联
}

// Parser 负责将 LLM 的原始响应转换为结构化的 Action 列表。
// 优先使用 native function calling；不支持时降级到文本格式解析。
type Parser struct{}

// NewParser 创建一个解析器实例（无状态，可复用）。
func NewParser() *Parser {
	return &Parser{}
}

// Parse 解析 LLM 响应，返回一个或多个 Action。
// 解析优先级：① native tool_calls → ② 文本 <tool>/<args> 格式 → ③ 纯文本回答。
func (p *Parser) Parse(resp *llm.Response) []Action {
	// ① 优先处理 native function calling 响应
	if len(resp.ToolCalls) > 0 {
		actions := make([]Action, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			actions = append(actions, Action{
				Type:     ActionToolCall,
				ToolName: tc.Function.Name,
				ToolArgs: json.RawMessage(tc.Function.Arguments),
				ToolID:   tc.ID,
			})
		}
		return actions
	}

	// ② 降级：尝试从文本中提取 <tool>/<args> 格式的工具调用
	if actions := p.parseText(resp.Content); len(actions) > 0 {
		return actions
	}

	// ③ 无工具调用，视为最终文本回答
	return []Action{{Type: ActionAnswer, Content: resp.Content}}
}

// parseText 尝试从文本内容中提取 <tool>name</tool><args>{...}</args> 格式的工具调用。
// 这是为不支持 native function calling 的模型（如部分 Ollama 模型）提供的降级方案。
func (p *Parser) parseText(content string) []Action {
	re := regexp.MustCompile(`<tool>(.*?)</tool>\s*<args>(.*?)</args>`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	actions := make([]Action, 0, len(matches))
	for i, m := range matches {
		if len(m) < 3 {
			continue
		}
		name := strings.TrimSpace(m[1])
		args := strings.TrimSpace(m[2])
		if !json.Valid([]byte(args)) {
			continue
		}
		actions = append(actions, Action{
			Type:     ActionToolCall,
			ToolName: name,
			ToolArgs: json.RawMessage(args),
			ToolID:   fmt.Sprintf("text_%d", i),
		})
	}
	return actions
}
