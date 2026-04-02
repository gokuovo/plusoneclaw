// Package agent 包含 Agent 的所有核心组件：上下文、记忆、控制、解析器和主循环。
package agent

import (
	"plusoneclaw/llm" // 引用消息类型
)

// Context 管理对话上下文，包括 system prompt 和滚动的对话历史。
type Context struct {
	systemPrompt string        // 系统提示词，作为对话的第一条消息（不参与历史裁剪）
	messages     []llm.Message // 对话历史消息列表（不含 system prompt）
	maxMessages  int           // 历史消息最大保留条数；0 表示不限制
}

// NewContext 创建一个新的对话上下文。
func NewContext(systemPrompt string, maxMessages int) *Context {
	return &Context{
		systemPrompt: systemPrompt, // 设置初始 system prompt
		maxMessages:  maxMessages,  // 设置历史消息上限
	}
}

// SystemPrompt 返回当前的 system prompt 内容。
func (c *Context) SystemPrompt() string {
	return c.systemPrompt // 直接返回字段值
}

// SetSystemPrompt 更新 system prompt（例如注入 memory 内容时调用）。
func (c *Context) SetSystemPrompt(prompt string) {
	c.systemPrompt = prompt // 覆盖原有 system prompt
}

// Append 向对话历史末尾追加一条消息，并在超限时自动裁剪旧消息。
func (c *Context) Append(msg llm.Message) {
	c.messages = append(c.messages, msg) // 追加到历史列表
	c.trim()                             // 检查并裁剪超出上限的旧消息
}

// Messages 返回完整的消息列表，system prompt 作为第一条消息拼入。
// 该列表直接传给 LLM.Chat()。
func (c *Context) Messages() []llm.Message {
	msgs := make([]llm.Message, 0, len(c.messages)+1) // 预分配容量（历史数量 + 1 条 system）
	if c.systemPrompt != "" {
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: c.systemPrompt}) // 将 system prompt 置于首位
	}
	msgs = append(msgs, c.messages...) // 追加完整对话历史
	return msgs
}

// History 返回对话历史的浅拷贝（不含 system prompt），用于读取而不修改原始数据。
func (c *Context) History() []llm.Message {
	cp := make([]llm.Message, len(c.messages)) // 创建等长切片
	copy(cp, c.messages)                       // 复制元素，避免外部修改影响内部状态
	return cp
}

// Clear 清空对话历史，保留 system prompt 不变。
func (c *Context) Clear() {
	c.messages = c.messages[:0] // 重置切片长度为 0，但保留底层数组以复用内存
}

// trim 在消息数量超过 maxMessages 时，丢弃最早的消息，只保留最新的 maxMessages 条。
func (c *Context) trim() {
	if c.maxMessages <= 0 || len(c.messages) <= c.maxMessages {
		return // 未设置上限，或尚未超限，直接返回
	}
	excess := len(c.messages) - c.maxMessages // 计算需要丢弃的消息数量
	c.messages = c.messages[excess:]          // 截取尾部最新的 maxMessages 条消息
}
