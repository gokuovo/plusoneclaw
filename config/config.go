// Package config 负责加载和解析 YAML 配置文件。
// 环境变量可覆盖对应字段（见各字段注释），优先级：环境变量 > 配置文件 > 零值。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是整个应用的根配置。
type Config struct {
	Server ServerConfig `yaml:"server"`
	Log    LogConfig    `yaml:"log"`
	Agent  AgentConfig  `yaml:"agent"`
}

// ServerConfig 控制 HTTP 服务器行为。
type ServerConfig struct {
	// Addr 是监听地址，如 ":8080"。
	// 环境变量 SERVER_ADDR 可覆盖。
	Addr string `yaml:"addr"`
}

// LogConfig 控制 slog 全局日志格式与级别。
type LogConfig struct {
	// Level 日志级别：debug | info | warn | error（默认 info）。
	// 环境变量 LOG_LEVEL 可覆盖。
	Level string `yaml:"level"`
	// Format 日志格式：json | text（默认 json）。
	// 环境变量 LOG_FORMAT 可覆盖。
	Format string `yaml:"format"`
}

// AgentConfig 控制 Agent 的行为参数。
type AgentConfig struct {
	// SystemPromptFile 系统提示词文件路径；非空时从文件加载 system prompt。
	// 优先级高于 SystemPrompt。
	SystemPromptFile string `yaml:"system_prompt_file"`
	// SystemPrompt 内联系统提示词；仅在 SystemPromptFile 为空时使用。
	SystemPrompt string `yaml:"system_prompt"`
	// SkillsDir 技能发现目录路径。
	SkillsDir string `yaml:"skills_dir"`
	// MaxMessages 对话历史最大保留条数；0 时使用 Agent 默认值。
	MaxMessages int `yaml:"max_messages"`
	// MemoryPath 持久化记忆文件路径。
	MemoryPath string `yaml:"memory_path"`
	// MaxIterations Agent Loop 最大迭代次数；0 时使用默认值。
	MaxIterations int `yaml:"max_iterations"`
}

// Load 从 path 加载 YAML 配置文件，再用环境变量覆盖相关字段。
// 若文件不存在，返回包含零值的 Config（调用方应提供 defaults）。
func Load(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config file %q: %w", path, err)
		}
	}

	// 环境变量覆盖——允许在容器/CI 中无需修改配置文件即可调整关键参数
	if v := os.Getenv("SERVER_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}

	return cfg, nil
}
