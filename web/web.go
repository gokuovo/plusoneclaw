// Package web 提供基于 HTTP 的 Web 聊天界面，通过内嵌的静态页面与 Agent 交互。
// 支持 REST API 聊天、模型切换等功能。
package web

import (
	"context"       // 请求超时控制
	"embed"         // 嵌入静态文件（index.html）
	"encoding/json" // JSON 序列化/反序列化
	"fmt"           // 错误格式化
	"io/fs"         // 文件系统接口，用于提取嵌入文件的子目录
	"log/slog"      // 结构化日志
	"net/http"      // HTTP 服务器
	"sync"          // 互斥锁，保护并发访问
	"time"          // 超时配置

	"plusoneclaw/agent" // Agent 核心
	"plusoneclaw/llm"   // LLM 接口
)

//go:embed static
var staticFS embed.FS // 嵌入 static/ 目录下的所有文件（index.html 等）

// ModelEntry 镜像 main 包中的模型配置项，避免循环依赖。
type ModelEntry struct {
	Name    string `json:"name"`     // 模型显示名
	Type    string `json:"type"`     // 客户端类型
	BaseURL string `json:"base_url"` // API 基础地址
	Model   string `json:"model"`    // 模型名称
}

// CreateLLMFunc 是一个创建 LLM 客户端的回调函数类型。
type CreateLLMFunc func(name string) (llm.LLM, error)

// Server 是 Web 聊天服务器，封装 Agent 并提供 REST API。
type Server struct {
	agent        *agent.Agent  // Agent 实例，处理用户消息
	models       []ModelEntry  // 所有可用模型列表
	currentModel string        // 当前选中的模型名
	createLLM    CreateLLMFunc // LLM 工厂函数，根据名称创建客户端
	mu           sync.Mutex    // 保护 agent 和 currentModel 的并发访问
	logger       *slog.Logger  // 结构化日志记录器
}

// Config 是创建 Server 的配置。
type Config struct {
	Agent        *agent.Agent  // 必填：Agent 实例
	Models       []ModelEntry  // 可用模型列表
	CurrentModel string        // 初始默认模型
	CreateLLM    CreateLLMFunc // LLM 工厂函数
	Logger       *slog.Logger  // 可选：自定义日志记录器
}

// NewServer 创建一个 Web 聊天服务器。
func NewServer(cfg Config) *Server {
	if cfg.Logger == nil { // 未提供时使用默认日志
		cfg.Logger = slog.Default()
	}
	return &Server{
		agent:        cfg.Agent,        // 设置 Agent
		models:       cfg.Models,       // 设置模型列表
		currentModel: cfg.CurrentModel, // 设置当前模型
		createLLM:    cfg.CreateLLM,    // 设置 LLM 工厂
		logger:       cfg.Logger,       // 设置日志
	}
}

// chatRequest 是聊天 API 的请求体。
type chatRequest struct {
	Message string `json:"message"` // 用户输入的消息文本
}

// chatResponse 是聊天 API 的响应体。
type chatResponse struct {
	Response string `json:"response"`        // Agent 的回复文本
	Error    string `json:"error,omitempty"` // 错误信息（成功时为空）
}

// modelsResponse 是模型列表 API 的响应体。
type modelsResponse struct {
	Models  []ModelEntry `json:"models"`  // 所有可用模型
	Current string       `json:"current"` // 当前模型名
}

// switchRequest 是切换模型 API 的请求体。
type switchRequest struct {
	Name string `json:"name"` // 目标模型名称
}

// Handler 返回配置好路由的 HTTP handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()                            // 创建路由复用器
	staticSub, _ := fs.Sub(staticFS, "static")           // 提取嵌入文件的 static/ 子目录
	mux.Handle("/", http.FileServer(http.FS(staticSub))) // 根路径提供静态文件服务
	mux.HandleFunc("/api/chat", s.handleChat)            // 聊天 API 端点
	mux.HandleFunc("/api/models", s.handleModels)        // 模型管理 API 端点
	return mux                                           // 返回配置好的路由
}

// ListenAndServe 启动 HTTP 服务器并开始监听。
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,             // 监听地址
		Handler:      s.Handler(),      // 路由处理器
		ReadTimeout:  10 * time.Second, // 读取超时，防御慢速攻击
		WriteTimeout: 5 * time.Minute,  // 写入超时，多步工具调用可能耗时较长
	}
	s.logger.Info("Web server starting", "addr", "http://localhost"+addr) // 记录启动日志
	return srv.ListenAndServe()                                           // 开始监听
}

// handleChat 处理 POST /api/chat 请求，将用户消息传给 Agent 并返回回复。
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { // 只接受 POST 方法
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest // 解析请求体
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "Invalid request body"}) // 无效 JSON
		return
	}
	if req.Message == "" { // 消息不能为空
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "Message is required"})
		return
	}

	s.mu.Lock()         // 加锁保护 Agent（Agent 不是并发安全的）
	defer s.mu.Unlock() // 函数退出时释放锁

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute) // 设置 5 分钟超时，满足多步工具调用场景
	defer cancel()                                                 // 释放超时资源

	resp, err := s.agent.Run(ctx, req.Message) // 调用 Agent 处理消息
	if err != nil {
		s.logger.Error("agent error", "error", err) // 记录错误日志
		writeJSON(w, http.StatusInternalServerError, chatResponse{Error: fmt.Sprintf("Agent error: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{Response: resp}) // 返回 Agent 回复
}

// handleModels 处理 /api/models 请求：
//
//	GET  → 返回所有可用模型和当前模型
//	POST → 切换当前模型
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet: // 获取模型列表
		s.mu.Lock()
		current := s.currentModel // 读取当前模型
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, modelsResponse{Models: s.models, Current: current}) // 返回列表

	case http.MethodPost: // 切换模型
		var req switchRequest // 解析请求体
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		newLLM, err := s.createLLM(req.Name) // 创建新的 LLM 客户端
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}) // 创建失败
			return
		}
		s.agent.SetLLM(newLLM)                                              // 热切换 Agent 的 LLM 客户端
		s.currentModel = req.Name                                           // 更新当前模型名
		writeJSON(w, http.StatusOK, map[string]string{"current": req.Name}) // 返回切换结果

	default: // 不支持的 HTTP 方法
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// writeJSON 将数据序列化为 JSON 并写入 HTTP 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json") // 设置响应内容类型
	w.WriteHeader(status)                              // 写入 HTTP 状态码
	json.NewEncoder(w).Encode(v)                       // 序列化并写入响应体
}
