package agent

import (
	"encoding/json" // JSON 序列化与反序列化，用于持久化存储
	"fmt"           // 字符串格式化，用于生成 Dump 输出
	"os"            // 文件读写操作
	"path/filepath" // 路径处理，用于创建父目录
	"sync"          // 读写锁，保证并发安全
)

// Memory 是一个极简的键值对持久化存储，数据保存在 JSON 文件中。
// Agent 启动时自动从文件加载，写入时自动同步到磁盘。
type Memory struct {
	mu   sync.RWMutex      // 读写锁：保证多 goroutine 并发读写的安全性
	data map[string]string // 内存中的键值对数据
	path string            // 持久化文件路径；为空时仅保存在内存中，不落盘
}

// NewMemory 创建一个记忆实例。path 为空时为纯内存模式，不持久化。
func NewMemory(path string) *Memory {
	m := &Memory{
		data: make(map[string]string), // 初始化空 map
		path: path,                    // 记录持久化路径
	}
	if path != "" {
		m.load() // 若指定了路径，启动时尝试从文件加载已有数据
	}
	return m
}

// Save 存储一个键值对，并立即持久化到磁盘。
func (m *Memory) Save(key, value string) error {
	m.mu.Lock()         // 写锁：独占写入，防止并发冲突
	defer m.mu.Unlock() // 函数退出时释放
	m.data[key] = value // 更新内存中的值
	return m.persist()  // 同步写入磁盘文件
}

// Load 按 key 读取记忆值，返回值和是否存在的布尔标志。
func (m *Memory) Load(key string) (string, bool) {
	m.mu.RLock()         // 读锁：允许并发读取
	defer m.mu.RUnlock() // 函数退出时释放读锁
	v, ok := m.data[key] // 查找 key 对应的值
	return v, ok
}

// Delete 删除指定 key 的记忆，并立即持久化。
func (m *Memory) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key) // 从 map 中删除 key
	return m.persist()  // 同步更新磁盘文件
}

// Keys 返回当前所有记忆的 key 列表（顺序不保证）。
func (m *Memory) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data)) // 预分配容量
	for k := range m.data {
		keys = append(keys, k) // 遍历收集所有 key
	}
	return keys
}

// Dump 将所有记忆格式化为多行文本，供注入 system prompt 使用。
// 格式为："- key: value\n"
func (m *Memory) Dump() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.data) == 0 {
		return "" // 无记忆时返回空字符串，避免注入无意义的 <memory> 块
	}
	result := ""
	for k, v := range m.data {
		result += fmt.Sprintf("- %s: %s\n", k, v) // 每条记忆占一行
	}
	return result
}

// load 从磁盘文件加载记忆数据（内部方法，仅在初始化时调用）。
func (m *Memory) load() {
	data, err := os.ReadFile(m.path) // 读取 JSON 文件
	if err != nil {
		return // 文件不存在或读取失败时静默忽略（首次启动时正常）
	}
	_ = json.Unmarshal(data, &m.data) // 将 JSON 反序列化到 map；忽略错误（文件损坏时重新开始）
}

// persist 将当前内存数据序列化为 JSON 并写入磁盘（内部方法）。
func (m *Memory) persist() error {
	if m.path == "" {
		return nil // 纯内存模式，不需要写磁盘
	}
	dir := filepath.Dir(m.path)                    // 提取父目录路径
	if err := os.MkdirAll(dir, 0755); err != nil { // 确保父目录存在（自动创建）
		return err
	}
	data, err := json.MarshalIndent(m.data, "", "  ") // 格式化为易读的 JSON
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0600) // 写入文件，权限 0600（仅文件所有者可读写）
}
