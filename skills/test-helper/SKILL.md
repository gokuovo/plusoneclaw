---
name: test-helper
description: Helps write unit tests and test cases for Go code. Use when asked to write tests, add test coverage, or explain how to test a specific function or module.
version: "1.0"
tags: [testing, go, unit-test]
tools: [read_file, write_file, shell]
triggers: [test, unit test, 测试, 单元测试, test coverage, 测试覆盖]
---

# Test Helper

当被要求编写测试代码时，遵循以下步骤：

## Step 1: 读取目标代码

使用 `read_file` 工具读取要测试的源文件，理解：
- 函数签名和返回值
- 边界条件和可能的错误路径
- 依赖关系（是否需要 mock）

## Step 2: 确定测试策略

### 纯函数（首选）
直接测试输入输出，无需 mock：
```go
func TestParseXxx(t *testing.T) {
    cases := []struct {
        name  string
        input string
        want  string
    }{
        {"normal", "hello", "HELLO"},
        {"empty", "", ""},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := ParseXxx(tc.input)
            if got != tc.want {
                t.Errorf("got %q, want %q", got, tc.want)
            }
        })
    }
}
```

### 依赖外部资源的函数
使用接口 + 临时目录/文件，避免污染真实环境：
```go
func TestWriteFile(t *testing.T) {
    dir := t.TempDir() // 测试结束自动清理
    path := filepath.Join(dir, "out.txt")
    // ...
}
```

## Step 3: 覆盖以下场景

| 场景 | 举例 |
|------|------|
| 正常路径 | 有效输入 → 预期输出 |
| 边界值 | 空字符串、nil、0、最大值 |
| 错误路径 | 非法输入 → 返回 error |
| 并发安全 | 多 goroutine 同时调用 |

## Step 4: 输出格式

生成的测试文件放在与源文件同一目录，命名为 `xxx_test.go`，包名加 `_test` 后缀（黑盒测试）或同包名（白盒测试）。

## 本项目中值得测试的模块

- `skill/skill.go` → 解析 YAML frontmatter、`AvailableSkillsPrompt()` 输出格式
- `agent/parser.go` → 文本 tool call 解析、native tool call 解析
- `agent/memory.go` → Save/Load/Dump
- `tool/tool.go` → Register/Get/Execute
- `web/web.go` → HTTP handler 用 `httptest` 包测试
