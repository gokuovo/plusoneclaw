// kimi.go 实现与 Moonshot Kimi API（/chat/completions）的通信。
// Kimi 采用 OpenAI 兼容协议，错误格式略有不同（额外的 type 字段）。
package llm

import (
	"bytes"         // 构建 HTTP 请求体
	"context"       // 请求超时与取消控制
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io"            // 读取 HTTP 响应体
	"net/http"      // HTTP 客户端
)

// KimiConfig 保存 Moonshot Kimi 客户端的配置项。
type KimiConfig struct {
	APIKey  string // Moonshot API Key
	BaseURL string // API 端点基础地址，默认 "https://api.moonshot.cn/v1"
	Model   string // 模型名称，如 "kimi-latest"
}

// kimiClient 是 LLM 接口的 Moonshot Kimi 实现。
type kimiClient struct {
	cfg    KimiConfig   // 客户端配置
	client *http.Client // HTTP 客户端实例
}

// NewKimi 根据配置创建一个 Moonshot Kimi LLM 客户端。
func NewKimi(cfg KimiConfig) LLM {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.moonshot.cn/v1"
	}
	return &kimiClient{cfg: cfg, client: &http.Client{}}
}

// --- Kimi 请求/响应结构 ---

// kimiMessage 是 Kimi 请求中的消息，Content 为 interface{} 以支持多模态。
type kimiMessage struct {
	Role             Role        `json:"role"`                        // 消息角色
	Content          interface{} `json:"content"`                     // 消息内容
	ReasoningContent string      `json:"reasoning_content,omitempty"` // thinking 推理内容，回传时需携带
	ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`        // 工具调用列表
	ToolCallID       string      `json:"tool_call_id,omitempty"`      // 工具结果关联 ID
}

// kimiRequest 是发送给 Kimi /chat/completions 端点的请求体。
type kimiRequest struct {
	Model    string        `json:"model"`           // 模型名称
	Messages []kimiMessage `json:"messages"`        // 对话消息列表
	Tools    []Tool        `json:"tools,omitempty"` // 可用工具列表
}

// kimiResponse 是 Kimi /chat/completions 端点返回的响应体。
type kimiResponse struct {
	Choices []struct { // 候选回复列表
		Message struct {
			Content          string     `json:"content"`                     // 文本回复
			ReasoningContent string     `json:"reasoning_content,omitempty"` // thinking 推理内容
			ToolCalls        []ToolCall `json:"tool_calls,omitempty"`        // 工具调用请求
		} `json:"message"`
	} `json:"choices"`
	Error *struct { // API 错误信息（成功时为 nil）
		Message string `json:"message"` // 错误描述
		Type    string `json:"type"`    // 错误类型（Kimi 特有）
	} `json:"error,omitempty"`
}

// Chat 实现 LLM 接口，向 Kimi API 发送聊天请求。
func (c *kimiClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	msgs := make([]kimiMessage, 0, len(messages)) // 预分配容量
	for _, msg := range messages {                // 遍历每条消息
		km := kimiMessage{
			Role:             msg.Role,             // 复制角色
			ReasoningContent: msg.ReasoningContent, // 回传 thinking 推理内容（Kimi 要求）
			ToolCalls:        msg.ToolCalls,        // 复制工具调用
			ToolCallID:       msg.ToolCallID,       // 复制工具结果 ID
		}
		if len(msg.Parts) > 0 { // 多模态消息
			km.Content = buildOpenAIContentField(msg.GetParts()) // 转换为 OpenAI 兼容格式
		} else { // 纯文本消息
			km.Content = msg.Content
		}
		msgs = append(msgs, km) // 追加到列表
	}

	reqBody := kimiRequest{ // 构建请求体
		Model:    c.cfg.Model,
		Messages: msgs,
	}
	if len(tools) > 0 { // 有工具时加入
		reqBody.Tools = tools
	}

	data, err := json.Marshal(reqBody) // 序列化为 JSON
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(data)) // 创建 HTTP POST 请求
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json") // JSON 内容类型
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey) // Bearer Token 认证
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

	var kimiResp kimiResponse // 解析响应 JSON
	if err := json.Unmarshal(body, &kimiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if kimiResp.Error != nil { // 检查 API 错误，包含 Kimi 特有的错误类型
		return nil, fmt.Errorf("api error: [%s] %s", kimiResp.Error.Type, kimiResp.Error.Message)
	}
	if len(kimiResp.Choices) == 0 { // 检查是否有回复
		return nil, fmt.Errorf("no choices in response")
	}

	choice := kimiResp.Choices[0].Message // 取第一个候选回复
	return &Response{
		Content:          choice.Content,          // 文本回复
		ToolCalls:        choice.ToolCalls,        // 工具调用请求
		ReasoningContent: choice.ReasoningContent, // thinking 推理内容
	}, nil
}
