// logger.go 提供生产级 slog 全局配置。
// 调用 config.InitLogger() 后，整个程序的 slog.Info/Warn/Error/Debug 等
// 调用均使用此处配置的 handler，无需在其他文件中 import 本包。
package config

import (
	"log/slog"
	"os"
	"strings"
)

// InitLogger 根据 LogConfig 设置全局 slog handler。
// 应在 main() 最开头（config.Load 之后）调用，早于其他任何日志输出。
func InitLogger(cfg LogConfig) {
	level := parseLevel(cfg.Level)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: true, // 记录调用来源（文件名+行号），便于排查问题
	}

	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		// 开发环境：人类可读的 key=value 格式
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		// 生产环境（默认）：结构化 JSON，适合 ELK / Loki / CloudWatch 等聚合系统
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// parseLevel 将字符串解析为 slog.Level，未识别时默认 Info。
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
