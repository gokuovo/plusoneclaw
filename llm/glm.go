// glm.go 实现与智谱 GLM API（/chat/completions）的通信。
// GLM 采用 OpenAI 兼容协议，错误码为 string 类型（非数字）。
package llm

import (
	"bytes"         // 构建 HTTP 请求体
	"context"       // 请求超时与取消控制
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io"            // 读取 HTTP 响应体
	"net/http"      // HTTP 客户端
)

// GLMConfig 保存智谱 GLM 客户端的配置项。
type GLMConfig struct {
	APIKey  string // 智谱 API Key
	BaseURL string // API 端点基础地址，默认 "https://open.bigmodel.cn/api/paas/v4"
	Model   string // 模型名称，如 "glm-4-flash"
}

// glmClient 是 LLM 接口的智谱 GLM 实现。
type glmClient struct {
	cfg    GLMConfig    // 客户端配置
	client *http.Client // HTTP 客户端实例
}

// NewGLM 根据配置创建一个智谱 GLM LLM 客户端。
func NewGLM(cfg GLMConfig) LLM {
	if cfg.BaseURL == "" { // 未指定时使用官方默认地址
		cfg.BaseURL = "https://open.bigmodel.cn/api/paas/v4"
	}
	return &glmClient{cfg: cfg, client: &http.Client{}} // 创建并返回客户端实例
}

// --- GLM 请求/响应结构 ---

// glmMessage 是 GLM 请求中的消息，Content 为 interface{} 以支持多模态。
type glmMessage struct {
	Role       Role        `json:"role"`                   // 消息角色
	Content    interface{} `json:"content"`                // 消息内容
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`   // 工具调用列表
	ToolCallID string      `json:"tool_call_id,omitempty"` // 工具结果关联 ID
}

// glmRequest 是发送给 GLM /chat/completions 端点的请求体。
type glmRequest struct {
	Model    string       `json:"model"`           // 模型名称
	Messages []glmMessage `json:"messages"`        // 对话消息列表
	Tools    []Tool       `json:"tools,omitempty"` // 可用工具列表
}

// glmResponse 是 GLM /chat/completions 端点返回的响应体。
type glmResponse struct {
	Choices []struct { // 候选回复列表
		Message struct {
			Content   string     `json:"content"`              // 文本回复
			ToolCalls []ToolCall `json:"tool_calls,omitempty"` // 工具调用请求
		} `json:"message"`
	} `json:"choices"`
	Error *struct { // API 错误信息（成功时为 nil）
		Message string `json:"message"` // 错误描述
		Code    string `json:"code"`    // 错误码（GLM 特有：字符串类型）
	} `json:"error,omitempty"`
}

// Chat 实现 LLM 接口，向智谱 GLM API 发送聊天请求。
func (c *glmClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	msgs := make([]glmMessage, 0, len(messages)) // 预分配容量
	for _, msg := range messages {               // 遍历每条消息
		gm := glmMessage{
			Role:       msg.Role,       // 复制角色
			ToolCalls:  msg.ToolCalls,  // 复制工具调用
			ToolCallID: msg.ToolCallID, // 复制工具结果 ID
		}
		if len(msg.Parts) > 0 { // 多模态消息
			gm.Content = buildOpenAIContentField(msg.GetParts()) // 转换为 OpenAI 兼容格式
		} else { // 纯文本消息
			gm.Content = msg.Content
		}
		msgs = append(msgs, gm) // 追加到列表
	}

	reqBody := glmRequest{ // 构建请求体
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

	var glmResp glmResponse // 解析响应 JSON
	if err := json.Unmarshal(body, &glmResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if glmResp.Error != nil { // 检查 API 错误，包含 GLM 特有的字符串错误码
		return nil, fmt.Errorf("api error: [%s] %s", glmResp.Error.Code, glmResp.Error.Message)
	}
	if len(glmResp.Choices) == 0 { // 检查是否有回复
		return nil, fmt.Errorf("no choices in response")
	}

	choice := glmResp.Choices[0].Message // 取第一个候选回复
	return &Response{
		Content:   choice.Content,   // 文本回复
		ToolCalls: choice.ToolCalls, // 工具调用请求
	}, nil
}
