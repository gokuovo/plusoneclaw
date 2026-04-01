// Package llm 定义与大语言模型通信所需的全部数据类型和统一接口。
// 包含消息结构、工具调用格式，以及 6 种原生 LLM 客户端实现。
package llm

import "context" // 用于 LLM.Chat 方法的超时与取消控制

// LLM 是大语言模型客户端的统一接口，所有 LLM 实现（OpenAI、Claude、Gemini 等）都需满足此接口。
type LLM interface {
	// Chat 发送对话消息给 LLM，可选传入工具列表以启用 function calling。
	// 返回模型的回复（可能包含文本和/或工具调用请求）。
	Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error)
}

// Role 表示对话消息的发送者角色。
type Role string

const (
	RoleSystem    Role = "system"    // 系统提示词角色，设定 Agent 的行为准则
	RoleUser      Role = "user"      // 用户角色，代表人类输入
	RoleAssistant Role = "assistant" // 助手角色，代表 LLM 的回复
	RoleTool      Role = "tool"      // 工具角色，携带工具执行结果
)

// ContentPart 表示消息中的一个内容片段，支持多模态（文本、图片等）。
type ContentPart struct {
	Type      string `json:"type"`                 // "text", "image"
	Text      string `json:"text,omitempty"`       // type=text 时的文本
	MediaType string `json:"media_type,omitempty"` // type=image 时的 MIME 类型，如 "image/png"
	Data      string `json:"data,omitempty"`       // base64 编码的二进制数据
	URL       string `json:"url,omitempty"`        // 图片 URL（与 Data 二选一）
}

// Message 表示对话历史中的一条消息。
// 纯文本消息使用 Content 字段；多模态消息使用 Parts 字段。二者取其一。
type Message struct {
	Role             Role          `json:"role"`                        // 消息发送者角色
	Content          string        `json:"content,omitempty"`           // 纯文本内容（与 Parts 二选一）
	Parts            []ContentPart `json:"parts,omitempty"`             // 多模态内容片段列表（图片+文本）
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`        // 助手消息中的工具调用请求列表
	ToolCallID       string        `json:"tool_call_id,omitempty"`      // 工具结果消息关联的调用 ID
	ReasoningContent string        `json:"reasoning_content,omitempty"` // Kimi 等模型 thinking 产生的推理内容，回传时需保留
}

// GetParts 返回消息的内容片段列表。
// 如果 Parts 非空则直接返回；否则将 Content 包装为单个 text part。
func (m *Message) GetParts() []ContentPart {
	if len(m.Parts) > 0 { // 有显式多模态片段时直接返回
		return m.Parts
	}
	if m.Content != "" { // 纯文本时包装为单个 text part，统一下游处理逻辑
		return []ContentPart{{Type: "text", Text: m.Content}}
	}
	return nil // 空消息
}

// IsMultimodal 报告消息是否包含非文本内容（如图片）。
func (m *Message) IsMultimodal() bool {
	for _, p := range m.Parts { // 遍历所有内容片段
		if p.Type != "text" { // 发现任何非文本类型即为多模态
			return true
		}
	}
	return false // 所有片段均为文本
}

// TextContent 返回消息中所有文本内容的拼接结果（忽略图片等非文本部分）。
func (m *Message) TextContent() string {
	if m.Content != "" { // 纯文本消息直接返回
		return m.Content
	}
	var s string                // 拼接结果
	for _, p := range m.Parts { // 遍历多模态片段
		if p.Type == "text" { // 只提取文本类型
			s += p.Text
		}
	}
	return s
}

// ToolCall 表示 LLM 发起的一次工具调用请求。
type ToolCall struct {
	ID       string       `json:"id"`             // 调用唯一标识，用于将结果消息与请求配对
	Type     string       `json:"type,omitempty"` // 调用类型，OpenAI 兼容接口通常固定为 "function"
	Function ToolCallFunc `json:"function"`       // 被调用的函数信息
}

// ToolCallFunc 描述被调用函数的名称和参数。
type ToolCallFunc struct {
	Name      string `json:"name"`      // 函数名，对应注册的工具名
	Arguments string `json:"arguments"` // JSON 格式的参数字符串
}

// Tool 描述可供 LLM 调用的一个工具，遵循 OpenAI function calling 格式。
type Tool struct {
	Type     string       `json:"type"`     // 工具类型，始终为 "function"
	Function ToolFunction `json:"function"` // 函数元信息
}

// ToolFunction 描述工具函数的元信息（名称、描述、参数 Schema）。
type ToolFunction struct {
	Name        string      `json:"name"`        // 工具名称
	Description string      `json:"description"` // 功能描述，LLM 据此决定何时调用
	Parameters  interface{} `json:"parameters"`  // 参数 JSON Schema
}

// Response 表示 LLM 的一次完整回复。
type Response struct {
	Content          string     `json:"content"`                     // 文本回复内容
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`        // 工具调用请求列表（可能为空）
	ReasoningContent string     `json:"reasoning_content,omitempty"` // Kimi 等模型 thinking 产生的推理内容
}

// buildOpenAIContentField 将 ContentParts 转换为 OpenAI content 格式。
// 纯文本返回 string；多模态返回 []map（content_part 数组）。
// 适用于 OpenAI / MiniMax / Kimi / GLM 等兼容格式。
func buildOpenAIContentField(parts []ContentPart) interface{} {
	if len(parts) == 0 { // 无内容时返回空字符串
		return ""
	}
	if len(parts) == 1 && parts[0].Type == "text" { // 单纯文本直接返回 string，简化请求体
		return parts[0].Text
	}
	var result []map[string]interface{} // 多模态时构建 content_part 数组
	for _, p := range parts {           // 遍历每个内容片段
		switch p.Type {
		case "text": // 文本片段
			result = append(result, map[string]interface{}{
				"type": "text",
				"text": p.Text,
			})
		case "image": // 图片片段
			url := p.URL                   // 优先使用图片 URL
			if url == "" && p.Data != "" { // 无 URL 时用 base64 data URI
				url = "data:" + p.MediaType + ";base64," + p.Data
			}
			result = append(result, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]string{"url": url}, // OpenAI 图片格式
			})
		}
	}
	return result // 返回多模态数组
}
