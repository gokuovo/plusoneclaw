// openai.go 实现与 OpenAI API（/v1/chat/completions）的通信。
// OpenAI 兼容协议也被 MiniMax、Kimi、GLM 等多个国产模型复用。
package llm

import (
	"bytes"         // 构建 HTTP 请求体
	"context"       // 请求超时与取消控制
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io"            // 读取 HTTP 响应体
	"net/http"      // HTTP 客户端
)

// OpenAIConfig 保存 OpenAI 客户端的配置项。
type OpenAIConfig struct {
	APIKey  string // API 密钥，用于认证
	BaseURL string // API 端点基础地址，默认 "https://api.openai.com/v1"
	Model   string // 模型名称，如 "gpt-4o-mini"
}

// openAIClient 是 LLM 接口的 OpenAI 实现。
type openAIClient struct {
	cfg    OpenAIConfig // 客户端配置
	client *http.Client // HTTP 客户端实例
}

// NewOpenAI 根据配置创建一个 OpenAI LLM 客户端。
func NewOpenAI(cfg OpenAIConfig) LLM {
	if cfg.BaseURL == "" { // 未指定时使用官方默认地址
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	return &openAIClient{cfg: cfg, client: &http.Client{}} // 创建并返回客户端实例
}

// openAIMessage 是 OpenAI 请求中的消息，Content 为 interface{} 以支持多模态。
type openAIMessage struct {
	Role       Role        `json:"role"`                   // 消息角色
	Content    interface{} `json:"content"`                // 消息内容：string 或多模态数组
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`   // 工具调用列表
	ToolCallID string      `json:"tool_call_id,omitempty"` // 工具结果关联 ID
}

// openAIChatRequest 是发送给 OpenAI /chat/completions 端点的请求体。
type openAIChatRequest struct {
	Model    string          `json:"model"`           // 模型名称
	Messages []openAIMessage `json:"messages"`        // 对话消息列表
	Tools    []Tool          `json:"tools,omitempty"` // 可用工具列表
}

// openAIChatResponse 是 OpenAI /chat/completions 端点返回的响应体。
type openAIChatResponse struct {
	Choices []struct { // 候选回复列表（通常只有一个）
		Message struct {
			Content   string     `json:"content"`              // 文本回复
			ToolCalls []ToolCall `json:"tool_calls,omitempty"` // 工具调用请求
		} `json:"message"`
	} `json:"choices"`
	Error *struct { // API 错误信息（成功时为 nil）
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat 实现 LLM 接口，向 OpenAI API 发送聊天请求。
func (c *openAIClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	msgs := convertToOpenAIMessages(messages) // 将通用消息转换为 OpenAI 格式

	reqBody := openAIChatRequest{ // 构建请求体
		Model:    c.cfg.Model,
		Messages: msgs,
	}
	if len(tools) > 0 { // 有工具时加入工具列表
		reqBody.Tools = tools
	}

	data, err := json.Marshal(reqBody) // 序列化为 JSON
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(data)) // 创建 HTTP POST 请求
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json") // 设置 JSON 内容类型
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey) // 设置 Bearer Token 认证
	}

	resp, err := c.client.Do(req) // 发送 HTTP 请求
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() // 确保响应体被关闭

	body, err := io.ReadAll(resp.Body) // 读取完整响应体
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var chatResp openAIChatResponse // 解析响应 JSON
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if chatResp.Error != nil { // 检查 API 返回的错误
		return nil, fmt.Errorf("api error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 { // 检查是否有回复
		return nil, fmt.Errorf("no choices in response")
	}

	choice := chatResp.Choices[0].Message // 取第一个候选回复
	return &Response{
		Content:   choice.Content,   // 文本回复
		ToolCalls: choice.ToolCalls, // 工具调用请求
	}, nil
}

// convertToOpenAIMessages 将通用 Message 列表转换为 OpenAI 请求格式，支持多模态。
func convertToOpenAIMessages(messages []Message) []openAIMessage {
	result := make([]openAIMessage, 0, len(messages)) // 预分配容量
	for _, msg := range messages {                    // 遍历每条消息
		om := openAIMessage{
			Role:       msg.Role,       // 复制角色
			ToolCalls:  msg.ToolCalls,  // 复制工具调用信息
			ToolCallID: msg.ToolCallID, // 复制工具结果关联 ID
		}
		// 有 Parts 时构建多模态 content；否则直接用 string
		if len(msg.Parts) > 0 {
			om.Content = buildOpenAIContentField(msg.GetParts()) // 转换为多模态格式
		} else {
			om.Content = msg.Content // 纯文本内容
		}
		result = append(result, om) // 追加到结果列表
	}
	return result
}
