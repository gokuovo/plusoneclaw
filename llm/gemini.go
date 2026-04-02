// gemini.go 实现与 Google Gemini API（generateContent）的通信。
// Gemini 使用独立的 parts-based 协议，工具调用通过 functionCall/functionResponse 实现。
package llm

import (
	"bytes"         // 构建 HTTP 请求体
	"context"       // 请求超时与取消控制
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io"            // 读取 HTTP 响应体
	"net/http"      // HTTP 客户端
)

// GeminiConfig 保存 Google Gemini 客户端的配置项。
type GeminiConfig struct {
	APIKey  string // Google AI API Key
	BaseURL string // API 端点基础地址，默认 "https://generativelanguage.googleapis.com"
	Model   string // 模型名称，如 "gemini-2.0-flash"
}

// geminiClient 是 LLM 接口的 Google Gemini 实现。
type geminiClient struct {
	cfg    GeminiConfig
	client *http.Client
}

// NewGemini 根据配置创建一个 Google Gemini LLM 客户端。
func NewGemini(cfg GeminiConfig) LLM {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com"
	}
	return &geminiClient{cfg: cfg, client: &http.Client{}}
}

// --- Gemini API 请求/响应结构 ---

// geminiContent 是 Gemini 格式的消息内容。
type geminiContent struct {
	Role  string       `json:"role,omitempty"` // 角色："user" 或 "model"（系统消息通过 systemInstruction 传递）
	Parts []geminiPart `json:"parts"`          // 内容片段列表
}

// geminiPart 表示 Gemini 消息中的一个内容片段。
type geminiPart struct {
	Text             string              `json:"text,omitempty"`             // 文本内容
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`     // 函数调用请求（LLM 生成）
	FunctionResponse *geminiToolResponse `json:"functionResponse,omitempty"` // 函数调用结果（工具执行后回传）
	InlineData       *geminiInlineData   `json:"inlineData,omitempty"`       // 多模态内联数据（图片等）
}

// geminiInlineData 表示 Gemini 的内联二进制数据（图片等）。
type geminiInlineData struct {
	MimeType string `json:"mimeType"` // MIME 类型，如 "image/png"
	Data     string `json:"data"`     // base64 编码数据
}

// geminiFunctionCall 表示 Gemini 的函数调用请求。
type geminiFunctionCall struct {
	Name string                 `json:"name"`           // 函数名称
	Args map[string]interface{} `json:"args,omitempty"` // 参数 map（Gemini 用 map 而非 JSON 字符串）
}

// geminiToolResponse 表示 Gemini 的函数调用结果。
type geminiToolResponse struct {
	Name     string                 `json:"name"`     // 关联的函数名
	Response map[string]interface{} `json:"response"` // 结果数据
}

// geminiFunctionDeclaration 是 Gemini 的函数声明格式。
type geminiFunctionDeclaration struct {
	Name        string      `json:"name"`                 // 工具名称
	Description string      `json:"description"`          // 工具描述
	Parameters  interface{} `json:"parameters,omitempty"` // 参数 JSON Schema
}

// geminiToolDeclaration 是 Gemini 的工具声明格式（包装函数声明列表）。
type geminiToolDeclaration struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"` // 函数声明列表
}

// geminiSystemInstruction 是 Gemini 的 system prompt 格式。
type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"` // system prompt 的内容片段
}

// geminiRequest 是发送给 Gemini generateContent 端点的请求体。
type geminiRequest struct {
	Contents          []geminiContent          `json:"contents"`                    // 对话消息列表
	Tools             []geminiToolDeclaration  `json:"tools,omitempty"`             // 可用工具列表
	SystemInstruction *geminiSystemInstruction `json:"systemInstruction,omitempty"` // system prompt
}

// geminiResponse 是 Gemini generateContent 端点返回的响应体。
type geminiResponse struct {
	Candidates []struct { // 候选回复列表
		Content geminiContent `json:"content"` // 回复内容
	} `json:"candidates"`
	Error *struct { // API 错误信息（成功时为 nil）
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// Chat 实现 LLM 接口，将通用消息格式转换为 Gemini 格式并调用 API。
func (c *geminiClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	// 转换消息并提取 system prompt
	systemPrompt, gemContents := convertToGeminiContents(messages)

	// 转换工具格式：OpenAI function 样式 → Gemini functionDeclarations 样式
	var gemTools []geminiToolDeclaration
	if len(tools) > 0 { // 有工具时转换
		var funcs []geminiFunctionDeclaration
		for _, t := range tools { // 遍历每个工具
			funcs = append(funcs, geminiFunctionDeclaration{
				Name:        t.Function.Name,        // 工具名
				Description: t.Function.Description, // 工具描述
				Parameters:  t.Function.Parameters,  // 参数 Schema
			})
		}
		gemTools = append(gemTools, geminiToolDeclaration{FunctionDeclarations: funcs}) // 包装为工具声明
	}

	reqBody := geminiRequest{ // 构建请求体
		Contents: gemContents,
	}
	if systemPrompt != "" { // 有 system prompt 时添加
		reqBody.SystemInstruction = &geminiSystemInstruction{
			Parts: []geminiPart{{Text: systemPrompt}}, // system prompt 作为文本片段
		}
	}
	if len(gemTools) > 0 { // 有工具时添加
		reqBody.Tools = gemTools
	}

	data, err := json.Marshal(reqBody) // 序列化为 JSON
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Gemini API URL 格式：/v1beta/models/{model}:generateContent?key={apiKey}
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		c.cfg.BaseURL, c.cfg.Model, c.cfg.APIKey) // 拼接完整 URL

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data)) // 创建 HTTP POST 请求
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json") // 设置 JSON 内容类型

	resp, err := c.client.Do(req) // 发送 HTTP 请求
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() // 确保响应体被关闭

	body, err := io.ReadAll(resp.Body) // 读取完整响应体
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var gemResp geminiResponse // 解析响应 JSON
	if err := json.Unmarshal(body, &gemResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if gemResp.Error != nil { // 检查 API 错误
		return nil, fmt.Errorf("api error: %s", gemResp.Error.Message)
	}

	if len(gemResp.Candidates) == 0 { // 检查是否有回复
		return nil, fmt.Errorf("no candidates in response")
	}

	// 将 Gemini 响应转换为通用格式
	return convertFromGeminiResponse(&gemResp), nil
}

// convertToGeminiContents 将通用消息列表转换为 Gemini 格式。
// system 消息被提取出来作为 systemInstruction 传递。
func convertToGeminiContents(messages []Message) (string, []geminiContent) {
	var systemPrompt string    // 提取的 system prompt
	var result []geminiContent // 转换后的消息列表

	for _, msg := range messages { // 遍历每条消息
		switch msg.Role {
		case RoleSystem: // system 消息提取为单独参数
			systemPrompt = msg.Content

		case RoleUser: // 用户消息，支持多模态
			var parts []geminiPart
			for _, p := range msg.GetParts() { // 遍历内容片段
				switch p.Type {
				case "image": // 图片转换为 Gemini inlineData 格式
					parts = append(parts, geminiPart{
						InlineData: &geminiInlineData{
							MimeType: p.MediaType, // MIME 类型
							Data:     p.Data,      // base64 数据
						},
					})
				default: // 文本内容
					parts = append(parts, geminiPart{Text: p.Text})
				}
			}
			result = append(result, geminiContent{Role: "user", Parts: parts})

		case RoleAssistant: // 助手消息（Gemini 中角色为 "model"）
			var parts []geminiPart
			if msg.Content != "" { // 文本内容
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			// 将 tool_calls 转换为 Gemini 的 functionCall parts
			for _, tc := range msg.ToolCalls {
				var args map[string]interface{}                          // 参数 map
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) // 将 JSON 字符串解析为 map
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: tc.Function.Name, // 函数名
						Args: args,             // 参数 map
					},
				})
			}
			if len(parts) > 0 { // 避免追加空消息
				result = append(result, geminiContent{Role: "model", Parts: parts})
			}

		case RoleTool: // 工具结果消息
			// Gemini 用 functionResponse part 表示工具结果，role 为 "user"
			result = append(result, geminiContent{
				Role: "user", // Gemini 要求 functionResponse 在 user 角色下
				Parts: []geminiPart{{
					FunctionResponse: &geminiToolResponse{
						Name:     msg.ToolCallID,                                // 用 ToolCallID 作为关联
						Response: map[string]interface{}{"result": msg.Content}, // 结果包装为 map
					},
				}},
			})
		}
	}

	return systemPrompt, result // 返回提取的 system prompt 和转换后的消息
}

// convertFromGeminiResponse 将 Gemini 响应转换为通用 Response。
func convertFromGeminiResponse(resp *geminiResponse) *Response {
	var content string       // 文本回复内容
	var toolCalls []ToolCall // 工具调用列表

	if len(resp.Candidates) == 0 { // 无候选回复时返回空响应
		return &Response{}
	}

	candidate := resp.Candidates[0].Content // 取第一个候选回复
	for i, part := range candidate.Parts {  // 遍历回复中的每个片段
		if part.Text != "" { // 文本片段，拼接到回复内容
			content += part.Text
		}
		if part.FunctionCall != nil { // 函数调用片段
			argsJSON, _ := json.Marshal(part.FunctionCall.Args) // 将 args map 序列化为 JSON 字符串
			toolCalls = append(toolCalls, ToolCall{
				ID: fmt.Sprintf("gemini_%d", i), // Gemini 不提供 tool call ID，生成唯一标识
				Function: ToolCallFunc{
					Name:      part.FunctionCall.Name, // 工具名称
					Arguments: string(argsJSON),       // 参数 JSON 字符串
				},
			})
		}
	}

	return &Response{
		Content:   content,   // 文本回复
		ToolCalls: toolCalls, // 工具调用
	}
}
