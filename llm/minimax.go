// minimax.go 实现与 MiniMax API（/chat/completions）的通信。
// MiniMax 采用 OpenAI 兼容协议，但错误返回格式略有不同（额外的 base_resp 字段）。
package llm

import (
	"bytes"         // 构建 HTTP 请求体
	"context"       // 请求超时与取消控制
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io"            // 读取 HTTP 响应体
	"net/http"      // HTTP 客户端
	"regexp"        // 过滤 <think> 内容
)

// MiniMaxConfig 保存 MiniMax 客户端的配置项。
type MiniMaxConfig struct {
	APIKey  string // MiniMax API Key
	BaseURL string // API 端点基础地址，默认 "https://api.minimaxi.com/v1"
	Model   string // 模型名称，如 "MiniMax-M1"
}

// miniMaxClient 是 LLM 接口的 MiniMax 原生实现。
type miniMaxClient struct {
	cfg    MiniMaxConfig
	client *http.Client
}

// NewMiniMax 根据配置创建一个 MiniMax LLM 客户端。
func NewMiniMax(cfg MiniMaxConfig) LLM {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.minimaxi.com/v1"
	}
	return &miniMaxClient{cfg: cfg, client: &http.Client{}}
}

// --- MiniMax API 请求/响应结构 ---

// miniMaxMessage 是 MiniMax 请求中的消息，Content 为 interface{} 以支持多模态。
type miniMaxMessage struct {
	Role       Role        `json:"role"`                   // 消息角色
	Content    interface{} `json:"content"`                // 消息内容：string 或多模态数组
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`   // 工具调用列表
	ToolCallID string      `json:"tool_call_id,omitempty"` // 工具结果关联 ID
}

// miniMaxRequest 是发送给 MiniMax /chat/completions 端点的请求体。
type miniMaxRequest struct {
	Model    string           `json:"model"`           // 模型名称
	Messages []miniMaxMessage `json:"messages"`        // 对话消息列表
	Tools    []Tool           `json:"tools,omitempty"` // 可用工具列表
}

// miniMaxResponse 是 MiniMax /chat/completions 端点返回的响应体。
type miniMaxResponse struct {
	Choices []struct { // 候选回复列表
		Message struct {
			Content   string     `json:"content"`              // 文本回复
			ToolCalls []ToolCall `json:"tool_calls,omitempty"` // 工具调用请求
		} `json:"message"`
	} `json:"choices"`
	BaseResp *struct { // MiniMax 特有的业务错误码（非 HTTP 层面）
		StatusCode int    `json:"status_code"` // 业务状态码，0 表示成功
		StatusMsg  string `json:"status_msg"`  // 状态描述
	} `json:"base_resp,omitempty"`
	Error *struct { // 标准 API 错误信息
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// Chat 实现 LLM 接口，向 MiniMax API 发送聊天请求。
func (c *miniMaxClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	// 转换消息格式以支持多模态
	msgs := make([]miniMaxMessage, 0, len(messages)) // 预分配容量
	for _, msg := range messages {                   // 遍历每条消息
		mm := miniMaxMessage{
			Role:       msg.Role,       // 复制角色
			ToolCalls:  msg.ToolCalls,  // 复制工具调用
			ToolCallID: msg.ToolCallID, // 复制工具结果 ID
		}
		if len(msg.Parts) > 0 { // 多模态消息
			mm.Content = buildOpenAIContentField(msg.GetParts()) // 转换为 OpenAI 兼容格式
		} else { // 纯文本消息
			mm.Content = msg.Content
		}
		msgs = append(msgs, mm) // 追加到列表
	}

	reqBody := miniMaxRequest{ // 构建请求体
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

	var mmResp miniMaxResponse // 解析响应 JSON
	if err := json.Unmarshal(body, &mmResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	// 检查 MiniMax 特有的 base_resp 错误（业务层错误）
	if mmResp.BaseResp != nil && mmResp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("api error: %s (%d)", mmResp.BaseResp.StatusMsg, mmResp.BaseResp.StatusCode)
	}
	if mmResp.Error != nil { // 检查标准 API 错误
		return nil, fmt.Errorf("api error: %s", mmResp.Error.Message)
	}

	if len(mmResp.Choices) == 0 { // 检查是否有回复
		return nil, fmt.Errorf("no choices in response")
	}

	choice := mmResp.Choices[0].Message                                       // 取第一个候选回复
	content := regexp.MustCompile(`(?s)<think>.*?</think>`).ReplaceAllString( // 过滤 thinking 内容
		choice.Content, "")
	return &Response{
		Content:   content,          // 文本回复（已去除 <think> 块）
		ToolCalls: choice.ToolCalls, // 工具调用请求
	}, nil
}
