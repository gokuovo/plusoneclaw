// anthropic.go 实现与 Anthropic Claude API（/v1/messages）的通信。
// Claude 使用独立的协议格式，工具调用通过 tool_use/tool_result 内容块实现。
package llm

import (
	"bytes"         // 构建 HTTP 请求体
	"context"       // 请求超时与取消控制
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io"            // 读取 HTTP 响应体
	"net/http"      // HTTP 客户端
)

// AnthropicConfig 保存 Anthropic Claude 客户端的配置项。
type AnthropicConfig struct {
	APIKey  string // Anthropic API Key
	BaseURL string // API 端点基础地址，默认 "https://api.anthropic.com"
	Model   string // 模型名称，如 "claude-sonnet-4-20250514"
}

// anthropicClient 是 LLM 接口的 Anthropic Claude 实现。
type anthropicClient struct {
	cfg    AnthropicConfig
	client *http.Client
}

// NewAnthropic 根据配置创建一个 Anthropic Claude LLM 客户端。
func NewAnthropic(cfg AnthropicConfig) LLM {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	return &anthropicClient{cfg: cfg, client: &http.Client{}}
}

// --- Anthropic API 请求/响应结构 ---

// anthropicMessage 是 Anthropic 格式的消息。
type anthropicMessage struct {
	Role    string             `json:"role"`    // 角色："user" 或 "assistant"（Anthropic 不支持 system 角色消息）
	Content []anthropicContent `json:"content"` // 内容块列表（文本、图片、tool_use 等）
}

// anthropicContent 表示 Anthropic 消息中的一个内容块。
type anthropicContent struct {
	Type      string              `json:"type"`                  // 内容类型："text"、"image"、"tool_use"、"tool_result"
	Text      string              `json:"text,omitempty"`        // 文本内容
	ID        string              `json:"id,omitempty"`          // tool_use 的唯一 ID
	Name      string              `json:"name,omitempty"`        // tool_use 的工具名
	Input     json.RawMessage     `json:"input,omitempty"`       // tool_use 的输入参数（原始 JSON）
	ToolUseID string              `json:"tool_use_id,omitempty"` // tool_result 关联的 tool_use ID
	Content   string              `json:"content,omitempty"`     // tool_result 的执行结果文本
	Source    *anthropicImgSource `json:"source,omitempty"`      // type=image 时的图片来源
}

// anthropicImgSource 表示 Anthropic 图片数据来源（base64 编码）。
type anthropicImgSource struct {
	Type      string `json:"type"`       // 来源类型，始终为 "base64"
	MediaType string `json:"media_type"` // MIME 类型，如 "image/png"、"image/jpeg"
	Data      string `json:"data"`       // base64 编码数据
}

// anthropicTool 是 Anthropic 工具定义格式。
type anthropicTool struct {
	Name        string      `json:"name"`         // 工具名称
	Description string      `json:"description"`  // 工具描述
	InputSchema interface{} `json:"input_schema"` // 参数 JSON Schema（Anthropic 用 input_schema 而非 parameters）
}

// anthropicRequest 是发送给 Anthropic /v1/messages 端点的请求体。
type anthropicRequest struct {
	Model     string             `json:"model"`            // 模型名称
	MaxTokens int                `json:"max_tokens"`       // 最大生成 token 数（Anthropic 必填）
	System    string             `json:"system,omitempty"` // system prompt 单独传递（不放在 messages 中）
	Messages  []anthropicMessage `json:"messages"`         // 对话消息列表
	Tools     []anthropicTool    `json:"tools,omitempty"`  // 可用工具列表
}

// anthropicResponse 是 Anthropic /v1/messages 端点返回的响应体。
type anthropicResponse struct {
	Content []anthropicContent `json:"content"` // 回复内容块列表
	Error   *struct {          // API 错误信息（成功时为 nil）
		Type    string `json:"type"`    // 错误类型
		Message string `json:"message"` // 错误描述
	} `json:"error,omitempty"`
}

// Chat 实现 LLM 接口，将通用消息格式转换为 Anthropic 格式并调用 API。
func (c *anthropicClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	// 提取 system prompt 并转换消息格式
	systemPrompt, anthMsgs := convertToAnthropicMessages(messages)

	// 转换工具格式：OpenAI 样式 → Anthropic 样式
	var anthTools []anthropicTool
	for _, t := range tools { // 遍历每个工具
		anthTools = append(anthTools, anthropicTool{
			Name:        t.Function.Name,        // 工具名称
			Description: t.Function.Description, // 工具描述
			InputSchema: t.Function.Parameters,  // 参数 Schema（Anthropic 用 input_schema）
		})
	}

	reqBody := anthropicRequest{ // 构建请求体
		Model:     c.cfg.Model,
		MaxTokens: 4096,         // Anthropic 要求显式指定最大 token 数
		System:    systemPrompt, // system prompt 单独传递
		Messages:  anthMsgs,
	}
	if len(anthTools) > 0 { // 有工具时加入
		reqBody.Tools = anthTools
	}

	data, err := json.Marshal(reqBody) // 序列化为 JSON
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/messages", bytes.NewReader(data)) // 创建 HTTP POST 请求
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json") // JSON 内容类型
	req.Header.Set("x-api-key", c.cfg.APIKey)          // Anthropic 特有的 API Key 认证头
	req.Header.Set("anthropic-version", "2023-06-01")  // API 版本号（必填）

	resp, err := c.client.Do(req) // 发送 HTTP 请求
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() // 确保响应体被关闭

	body, err := io.ReadAll(resp.Body) // 读取完整响应体
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var anthResp anthropicResponse // 解析响应 JSON
	if err := json.Unmarshal(body, &anthResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if anthResp.Error != nil { // 检查 API 错误
		return nil, fmt.Errorf("api error: %s", anthResp.Error.Message)
	}

	// 将 Anthropic 响应转换为通用格式
	return convertFromAnthropicResponse(&anthResp), nil
}

// convertToAnthropicMessages 将通用消息列表转换为 Anthropic 格式。
// system 消息被提取出来单独传递，tool 消息被转换为 user 角色的 tool_result 内容块。
func convertToAnthropicMessages(messages []Message) (string, []anthropicMessage) {
	var systemPrompt string       // 提取的 system prompt
	var result []anthropicMessage // 转换后的消息列表

	for _, msg := range messages { // 遍历每条消息
		switch msg.Role {
		case RoleSystem: // system 消息提取为单独参数
			systemPrompt = msg.Content

		case RoleUser: // 用户消息，支持多模态
			var content []anthropicContent
			for _, p := range msg.GetParts() { // 遍历内容片段
				switch p.Type {
				case "image": // 图片转换为 Anthropic 原生图片格式
					content = append(content, anthropicContent{
						Type: "image",
						Source: &anthropicImgSource{
							Type:      "base64",    // Anthropic 要求 base64 格式
							MediaType: p.MediaType, // MIME 类型
							Data:      p.Data,      // base64 数据
						},
					})
				default: // 文本内容
					content = append(content, anthropicContent{Type: "text", Text: p.Text})
				}
			}
			result = append(result, anthropicMessage{Role: "user", Content: content})

		case RoleAssistant: // 助手消息，可能包含文本和 tool_use
			var content []anthropicContent
			if msg.Content != "" { // 有文本内容时添加文本块
				content = append(content, anthropicContent{Type: "text", Text: msg.Content})
			}
			// 将 tool_calls 转换为 Anthropic 的 tool_use 内容块
			for _, tc := range msg.ToolCalls {
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,                                  // 调用 ID
					Name:  tc.Function.Name,                       // 工具名
					Input: json.RawMessage(tc.Function.Arguments), // 参数 JSON
				})
			}
			if len(content) > 0 { // 避免追加空消息
				result = append(result, anthropicMessage{Role: "assistant", Content: content})
			}

		case RoleTool: // 工具结果消息
			// Anthropic 要求 tool_result 作为 user 角色的内容块
			result = append(result, anthropicMessage{
				Role: "user", // 必须是 user 角色
				Content: []anthropicContent{{
					Type:      "tool_result",  // 工具结果类型
					ToolUseID: msg.ToolCallID, // 关联对应的 tool_use ID
					Content:   msg.Content,    // 执行结果文本
				}},
			})
		}
	}

	return systemPrompt, result // 返回提取的 system prompt 和转换后的消息
}

// convertFromAnthropicResponse 将 Anthropic 响应转换为通用 Response。
func convertFromAnthropicResponse(resp *anthropicResponse) *Response {
	var content string       // 文本回复内容
	var toolCalls []ToolCall // 工具调用列表

	for _, block := range resp.Content { // 遍历响应中的每个内容块
		switch block.Type {
		case "text": // 文本块，拼接到回复内容
			content += block.Text
		case "tool_use": // 工具调用块，转换为通用 ToolCall 格式
			toolCalls = append(toolCalls, ToolCall{
				ID: block.ID, // Anthropic 提供的调用 ID
				Function: ToolCallFunc{
					Name:      block.Name,          // 工具名称
					Arguments: string(block.Input), // 参数 JSON 字符串
				},
			})
		}
	}

	return &Response{
		Content:   content,   // 文本回复
		ToolCalls: toolCalls, // 工具调用
	}
}
