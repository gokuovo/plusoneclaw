package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// RegisterBuiltins 向注册中心注册所有内置工具（shell、read_file、write_file）。
// 将内置工具集中管理，避免 main.go 膨胀。
func RegisterBuiltins(r *Registry) {
	r.Register(shellTool())
	r.Register(readFileTool())
	r.Register(writeFileTool())
}

// shellTool 允许 Agent 在本机执行任意 shell 命令。
func shellTool() *FuncTool {
	return &FuncTool{
		ToolName: "shell",
		Desc:     "Execute a shell command and return the output.",
		Params: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "The shell command to execute"}
			},
			"required": ["command"]
		}`),
		Fn: func(ctx context.Context, params json.RawMessage) (string, error) {
			var p struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return "", err
			}
			out, err := exec.CommandContext(ctx, "sh", "-c", p.Command).CombinedOutput()
			if err != nil {
				return fmt.Sprintf("Error: %v\nOutput: %s", err, string(out)), nil
			}
			return string(out), nil
		},
	}
}

// readFileTool 允许 Agent 读取指定文件的内容。
func readFileTool() *FuncTool {
	return &FuncTool{
		ToolName: "read_file",
		Desc:     "Read the contents of a file.",
		Params: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path to read"}
			},
			"required": ["path"]
		}`),
		Fn: func(ctx context.Context, params json.RawMessage) (string, error) {
			var p struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return "", err
			}
			data, err := os.ReadFile(p.Path)
			if err != nil {
				return fmt.Sprintf("Error: %v", err), nil
			}
			return string(data), nil
		},
	}
}

// writeFileTool 允许 Agent 将内容写入文件（不存在时自动创建）。
func writeFileTool() *FuncTool {
	return &FuncTool{
		ToolName: "write_file",
		Desc:     "Write content to a file. Creates the file if it does not exist.",
		Params: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path":    {"type": "string", "description": "File path to write"},
				"content": {"type": "string", "description": "Content to write"}
			},
			"required": ["path", "content"]
		}`),
		Fn: func(ctx context.Context, params json.RawMessage) (string, error) {
			var p struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return "", err
			}
			if err := os.WriteFile(p.Path, []byte(p.Content), 0644); err != nil {
				return fmt.Sprintf("Error: %v", err), nil
			}
			return fmt.Sprintf("Written %d bytes to %s", len(p.Content), p.Path), nil
		},
	}
}
