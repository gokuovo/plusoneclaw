// main.go 是 PlusOneClaw Agent 框架的入口，组装 LLM、Tool、Skill 和 Agent。
// 启动后 Web 服务器常驻后台，同时提供 REPL 交互模式。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"plusoneclaw/agent"
	"plusoneclaw/config"
	"plusoneclaw/llm"
	"plusoneclaw/tool"
	"plusoneclaw/web"
)

// ModelEntry 表示 models.json 中的一个模型配置项。
type ModelEntry struct {
	Name      string `json:"name"`        // 模型显示名
	Type      string `json:"type"`        // 客户端类型："openai"、"anthropic"、"gemini"、"minimax"、"kimi"、"glm"
	APIKey    string `json:"api_key"`     // 直接填写的 API Key（本地调试用）
	APIKeyEnv string `json:"api_key_env"` // 环境变量名，程序启动时读取（推荐生产用，避免泄露）
	BaseURL   string `json:"base_url"`    // API 端点基础地址
	Model     string `json:"model"`       // 模型名称
}

// ModelsConfig 表示 models.json 的完整结构。
type ModelsConfig struct {
	Models  []ModelEntry `json:"models"`  // 所有可选模型
	Default string       `json:"default"` // 默认模型名
}

// loadModelsConfig 从指定路径加载模型配置文件。
func loadModelsConfig(path string) (*ModelsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read models config: %w", err)
	}
	var cfg ModelsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse models config: %w", err)
	}
	return &cfg, nil
}

// findModel 在配置中按名称查找模型。
func findModel(cfg *ModelsConfig, name string) (*ModelEntry, bool) {
	for i := range cfg.Models {
		if cfg.Models[i].Name == name {
			return &cfg.Models[i], true
		}
	}
	return nil, false
}

// createLLM 根据模型配置项创建对应的原生 LLM 客户端。
// 支持 api_key 直接填写和 api_key_env 环境变量两种方式。
func createLLM(entry *ModelEntry) llm.LLM {
	apiKey := entry.APIKey // 优先使用直接填写的 key
	if apiKey == "" && entry.APIKeyEnv != "" {
		apiKey = os.Getenv(entry.APIKeyEnv) // 回退从环境变量读取
	}
	switch entry.Type {
	case "anthropic":
		return llm.NewAnthropic(llm.AnthropicConfig{
			APIKey:  apiKey,
			BaseURL: entry.BaseURL,
			Model:   entry.Model,
		})
	case "gemini":
		return llm.NewGemini(llm.GeminiConfig{
			APIKey:  apiKey,
			BaseURL: entry.BaseURL,
			Model:   entry.Model,
		})
	case "minimax":
		return llm.NewMiniMax(llm.MiniMaxConfig{
			APIKey:  apiKey,
			BaseURL: entry.BaseURL,
			Model:   entry.Model,
		})
	case "openai":
		return llm.NewOpenAI(llm.OpenAIConfig{
			APIKey:  apiKey,
			BaseURL: entry.BaseURL,
			Model:   entry.Model,
		})
	case "kimi":
		return llm.NewKimi(llm.KimiConfig{
			APIKey:  apiKey,
			BaseURL: entry.BaseURL,
			Model:   entry.Model,
		})
	case "glm":
		return llm.NewGLM(llm.GLMConfig{
			APIKey:  apiKey,
			BaseURL: entry.BaseURL,
			Model:   entry.Model,
		})
	default:
		slog.Error("Unknown model type", "type", entry.Type)
		os.Exit(1)
		return nil
	}
}

func main() {
	// --- 加载 YAML 配置文件（环境变量可覆盖，见 config/config.go）---
	cfgPath := "config.yaml"
	if v := os.Getenv("CONFIG_FILE"); v != "" {
		cfgPath = v
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	config.InitLogger(cfg.Log) // 初始化全局 slog（级别/格式来自配置文件，可被环境变量覆盖）

	// 子命令识别：
	//   plusoneclaw        → Web 服务器守护进程
	//   plusoneclaw repl   → 连接到已运行的 Web 服务器的 REPL 客户端
	if len(os.Args) > 1 && os.Args[1] == "repl" {
		replAddr := cfg.Server.Addr // 使用配置文件中的服务器地址
		if replAddr == "" {
			replAddr = ":8323" // 默认端口
		}
		runREPLClient("http://localhost" + replAddr) // 连接到本地服务器
		return
	}

	// --- 加载模型配置 ---
	modelsCfg, err := loadModelsConfig("models.json")
	if err != nil {
		slog.Error("Failed to load models config", "error", err)
		os.Exit(1)
	}

	// 选择默认模型
	currentModelName := modelsCfg.Default
	entry, ok := findModel(modelsCfg, currentModelName)
	if !ok {
		slog.Error("Default model not found in models.json", "name", currentModelName)
		os.Exit(1)
	}
	client := createLLM(entry)

	// --- Tool 注册：向注册中心添加可执行工具（通过 LLM function calling 调用）---
	tools := tool.NewRegistry()  // 创建空的工具注册中心
	tool.RegisterBuiltins(tools) // 注册内置工具（shell、read_file、write_file），详见 tool/builtin.go

	// --- 加载 System Prompt：支持从文件加载（长提示词）或内联配置 ---
	systemPrompt := cfg.Agent.SystemPrompt
	if cfg.Agent.SystemPromptFile != "" {
		data, err := os.ReadFile(cfg.Agent.SystemPromptFile)
		if err != nil {
			slog.Error("Failed to load system prompt file", "path", cfg.Agent.SystemPromptFile, "error", err)
			os.Exit(1)
		}
		systemPrompt = string(data)
	}

	// --- Agent 构建：组装所有组件 ---
	maxIterations := cfg.Agent.MaxIterations
	if maxIterations == 0 {
		maxIterations = 15
	}
	a := agent.New(agent.Config{
		LLM:          client,
		Tools:        tools,
		SkillsDir:    cfg.Agent.SkillsDir,
		SystemPrompt: systemPrompt,
		MaxMessages:  cfg.Agent.MaxMessages,
		MemoryPath:   cfg.Agent.MemoryPath,
		Control: &agent.Control{
			MaxIterations: maxIterations,
			OnError: func(err error) agent.ErrorAction {
				slog.Error("Agent error", "error", err)
				// 认证/授权错误立即终止，避免无意义的重试
				e := err.Error()
				if strings.Contains(e, "401") || strings.Contains(e, "403") ||
					strings.Contains(e, "login fail") || strings.Contains(e, "Unauthorized") ||
					strings.Contains(e, "api key") || strings.Contains(e, "API key") ||
					strings.Contains(e, "Prompt exceeds max length") || strings.Contains(e, "context length") ||
					strings.Contains(e, "max context") || strings.Contains(e, "[1261]") ||
					strings.Contains(e, "context deadline exceeded") || strings.Contains(e, "context canceled") ||
					strings.Contains(e, "quota") || strings.Contains(e, "Quota") ||
					strings.Contains(e, "rate limit") || strings.Contains(e, "Rate limit") {
					return agent.ErrorAbort
				}
				return agent.ErrorContinue
			},
		},
	})

	// --- 启动 Web 服务器（始终常驻）---
	webAddr := cfg.Server.Addr
	if webAddr == "" {
		webAddr = ":8323"
	}
	webModels := make([]web.ModelEntry, len(modelsCfg.Models))
	for i, m := range modelsCfg.Models {
		webModels[i] = web.ModelEntry{
			Name:    m.Name,
			Type:    m.Type,
			BaseURL: m.BaseURL,
			Model:   m.Model,
		}
	}
	srv := web.NewServer(web.Config{
		Agent:        a,
		Models:       webModels,
		CurrentModel: currentModelName,
		CreateLLM: func(name string) (llm.LLM, error) {
			e, ok := findModel(modelsCfg, name)
			if !ok {
				return nil, fmt.Errorf("model '%s' not found", name)
			}
			return createLLM(e), nil
		},
	})

	// 守护模式：启动 Web 服务器并阻塞
	fmt.Println("PlusOneClaw Agent")
	fmt.Printf("Model: %s (%s)\n", currentModelName, entry.Model)
	fmt.Printf("Web:   http://localhost%s\n", webAddr)
	fmt.Println("\nUsage: plusoneclaw repl  (connect interactive REPL)")
	if err := srv.ListenAndServe(webAddr); err != nil {
		slog.Error("Web server stopped", "error", err)
		os.Exit(1)
	}
}

// runREPLClient 作为 HTTP 客户端连接到已运行的 PlusOneClaw 服务器。
func runREPLClient(serverURL string) {
	// 获取当前模型信息
	type modelsResp struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
		Current string `json:"current"`
	}

	getModels := func() (*modelsResp, error) {
		resp, err := http.Get(serverURL + "/api/models")
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var r modelsResp
		json.NewDecoder(resp.Body).Decode(&r)
		return &r, nil
	}

	models, err := getModels()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to PlusOneClaw server at %s\n", serverURL)
		fmt.Fprintln(os.Stderr, "Make sure the server is running: plusoneclaw")
		os.Exit(1)
	}

	currentModel := models.Current
	fmt.Println("PlusOneClaw REPL")
	fmt.Printf("Connected to: %s\n", serverURL)
	fmt.Printf("Model: %s\n", currentModel)
	fmt.Println("Commands: /model [name], /quit")
	fmt.Println(strings.Repeat("-", 40))

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" || input == "exit" || input == "/quit" {
			break
		}

		// /model 命令
		if input == "/model" {
			m, _ := getModels()
			if m != nil {
				fmt.Println("Available models:")
				for _, model := range m.Models {
					marker := "  "
					if model.Name == m.Current {
						marker = "* "
					}
					fmt.Printf("  %s%s (%s)\n", marker, model.Name, model.Model)
				}
			}
			continue
		}
		if strings.HasPrefix(input, "/model ") {
			name := strings.TrimSpace(strings.TrimPrefix(input, "/model "))
			body, _ := json.Marshal(map[string]string{"name": name})
			resp, err := http.Post(serverURL+"/api/models", "application/json", bytes.NewReader(body))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				currentModel = name
				fmt.Printf("Switched to: %s\n", name)
			} else {
				fmt.Printf("Failed to switch model (status %d)\n", resp.StatusCode)
			}
			continue
		}

		// 发送消息到服务器
		body, _ := json.Marshal(map[string]string{"message": input})
		resp, err := http.Post(serverURL+"/api/chat", "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		var result struct {
			Response string `json:"response"`
			Error    string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else {
			fmt.Printf("\n%s\n", result.Response)
		}
		_ = currentModel
	}
}
