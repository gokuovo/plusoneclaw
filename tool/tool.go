// Package tool 定义可被 LLM 通过 function calling 调用的可执行工具。
// 工具（Tool）与技能（Skill）不同：工具是通过 LLM API 的 function calling 机制调用的原子能力，
// 而技能是通过 system prompt 注入的指令集。
package tool

import (
	"context"       // 支持超时与取消的上下文
	"encoding/json" // JSON 序列化与反序列化
	"fmt"           // 错误格式化
	"sync"          // 读写锁，保证注册中心并发安全

	"plusoneclaw/llm" // 引用 llm 包的 Tool 类型，用于生成 LLM 工具描述
)

// Tool 是一个可被 LLM 通过 function calling 调用的可执行能力单元。
type Tool interface {
	Name() string                                                        // 工具唯一标识名，与 LLM 调用时的 function name 对应
	Description() string                                                 // 功能描述，LLM 据此决定何时调用该工具
	Parameters() json.RawMessage                                         // 参数 JSON Schema，描述参数名称、类型和是否必填
	Execute(ctx context.Context, params json.RawMessage) (string, error) // 执行工具，params 为 LLM 传入的 JSON 参数，返回结果字符串
}

// Registry 是线程安全的工具注册中心，管理所有可用工具。
type Registry struct {
	mu    sync.RWMutex    // 读写锁：并发读取时允许多个 goroutine，写入时独占
	tools map[string]Tool // 以工具名为 key 的工具映射表
}

// NewRegistry 创建一个空的工具注册中心。
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 向注册中心添加一个工具；若同名工具已存在则覆盖。
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get 按名称查询工具，返回工具实例和是否找到的布尔值。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Execute 按名称执行指定工具，params 为 LLM 生成的 JSON 参数。
func (r *Registry) Execute(ctx context.Context, name string, params json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return t.Execute(ctx, params)
}

// Tools 将注册中心中所有工具转换为 LLM 可识别的工具描述列表（OpenAI function calling 格式）。
func (r *Registry) Tools() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]llm.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		var params interface{}
		if raw := t.Parameters(); raw != nil {
			_ = json.Unmarshal(raw, &params)
		}
		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  params,
			},
		})
	}
	return tools
}

// FuncTool 是 Tool 接口的函数式封装，无需定义新类型即可快速注册一个工具。
type FuncTool struct {
	ToolName string                                                            // 工具名称
	Desc     string                                                            // 工具描述
	Params   json.RawMessage                                                   // 参数 JSON Schema
	Fn       func(ctx context.Context, params json.RawMessage) (string, error) // 实际执行逻辑
}

func (f *FuncTool) Name() string                { return f.ToolName }
func (f *FuncTool) Description() string         { return f.Desc }
func (f *FuncTool) Parameters() json.RawMessage { return f.Params }

// Execute 调用内部函数 Fn 执行工具逻辑。
func (f *FuncTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	return f.Fn(ctx, params)
}
