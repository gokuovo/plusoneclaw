package agent

import (
	"context" // 用于包装带超时的 context
	"fmt"     // 错误格式化
	"time"    // 超时时长类型
)

// ErrorAction 枚举 Agent Loop 遇到错误时的处理策略。
type ErrorAction int

const (
	ErrorContinue ErrorAction = iota // 记录错误后继续下一次循环
	ErrorRetry                       // 重试当前步骤（LLM 调用）
	ErrorAbort                       // 立即终止 Agent Loop 并返回错误
)

// Control 定义 Agent Loop 的执行控制参数和生命周期钩子。
type Control struct {
	MaxIterations int                                  // Loop 最大迭代次数；0 表示不限制（谨慎使用）
	Timeout       time.Duration                        // 整个 Run() 调用的总超时；0 表示不超时
	StopCheck     func(response string) bool           // 自定义停止条件：返回 true 时提前结束 Loop
	OnError       func(err error) ErrorAction          // 错误处理策略回调；nil 时默认 Abort
	MaxRetries    int                                  // ErrorRetry 策略下单步最大重试次数（当前由调用方控制）
	BeforeStep    func(iteration int)                  // 每次 LLM 调用前的钩子，iteration 为当前迭代序号（从 0 开始）
	AfterStep     func(iteration int, response string) // 每次 LLM 调用后的钩子，response 为本轮文本输出
}

// DefaultControl 返回一组合理的默认控制配置：最多 20 轮，5 分钟超时，错误时继续。
func DefaultControl() *Control {
	return &Control{
		MaxIterations: 20,              // 防止无限循环
		Timeout:       5 * time.Minute, // 保守的总超时时间
		MaxRetries:    2,               // 默认支持 2 次重试
		OnError: func(err error) ErrorAction {
			return ErrorContinue // 默认遇到错误时记录并继续，不中断对话
		},
	}
}

// WithContext 根据配置的 Timeout 包装 context；无超时时返回可取消的 context。
func (c *Control) WithContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.Timeout > 0 {
		return context.WithTimeout(ctx, c.Timeout) // 带超时：到期后自动取消所有下游 HTTP 请求
	}
	return context.WithCancel(ctx) // 无超时：仍返回可取消的 context，便于外部主动中断
}

// ShouldStop 判断是否满足自定义的提前停止条件。
func (c *Control) ShouldStop(response string) bool {
	if c.StopCheck != nil {
		return c.StopCheck(response) // 调用用户定义的停止检查函数
	}
	return false // 未设置则永不提前停止
}

// CheckIteration 检查是否达到最大迭代次数，超限时返回错误。
func (c *Control) CheckIteration(i int) error {
	if c.MaxIterations > 0 && i >= c.MaxIterations { // MaxIterations=0 时跳过检查
		return fmt.Errorf("max iterations (%d) exceeded", c.MaxIterations)
	}
	return nil
}

// HandleError 调用用户配置的错误处理函数，返回对应的错误策略。
func (c *Control) HandleError(err error) ErrorAction {
	if c.OnError != nil {
		return c.OnError(err) // 使用用户自定义的错误策略
	}
	return ErrorAbort // 未配置时默认中止，避免静默失败
}
