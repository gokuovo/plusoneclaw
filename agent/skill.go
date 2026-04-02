// skill.go 实现基于目录的技能系统，遵循 OpenClaw Skills 规范。
//
// 技能（Skill）与工具（Tool）不同：
//   - 工具通过 LLM 的 function calling API 调用，是可执行的原子操作（如执行命令、读写文件）。
//   - 技能是包含 SKILL.md 的目录，通过 system prompt 注入指令，指导 Agent 完成特定领域任务。
//
// OpenClaw Skills 支持两种激活模式：
//  1. 自动激活：当用户输入匹配 triggers 关键词时，自动将技能正文注入 system prompt
//  2. 手动激活：Agent 判断任务匹配后，通过 read_file 工具读取 SKILL.md 获取完整指令
//
// 目录约定：
//
//	skills/<skill-name>/
//	├── SKILL.md          必需 - 技能定义（YAML frontmatter + Markdown 正文）
//	├── examples/         可选 - 示例文件（few-shot 示例）
//	├── templates/        可选 - 输出模板
//	├── scripts/          可选 - 可执行脚本
//	└── references/       可选 - 参考资料
package agent

import (
	"bufio"         // 逐行扫描 SKILL.md
	"fmt"           // 错误格式化
	"os"            // 文件和目录读取
	"path/filepath" // 路径拼接
	"strings"       // 字符串处理
	"sync"          // 读写锁，保证注册中心并发安全
)

// Skill 表示一个 Agent Skill，对应磁盘上包含 SKILL.md 的目录。
// 遵循 OpenClaw Skills 规范：frontmatter 包含元数据，Markdown 正文为完整指令。
type Skill struct {
	Name        string   // 技能唯一标识名（来自 YAML frontmatter），必须与目录名一致
	Description string   // 技能描述，Agent 据此判断何时激活（来自 YAML frontmatter）
	Version     string   // 技能版本号（语义化版本），可选
	Author      string   // 技能作者，可选
	Tags        []string // 分类标签，便于组织和检索，可选
	Tools       []string // 该技能依赖的工具列表，可选
	Triggers    []string // 自动激活触发词：用户输入包含这些词时自动注入技能正文
	Dir         string   // 技能目录的绝对路径，用于解析相对引用（scripts/ 等）
	skillPath   string   // SKILL.md 文件的完整路径，用于懒加载正文
}

// LoadBody 从磁盘动态读取 SKILL.md 的正文内容（激活阶段调用）。
// 每次调用都重新读取，确保文件修改后立即生效，无需重启 Agent。
func (s *Skill) LoadBody() (string, error) {
	data, err := os.ReadFile(s.skillPath)
	if err != nil {
		return "", fmt.Errorf("load skill body %s: %w", s.Name, err)
	}
	_, body, err := parseFrontmatter(string(data))
	if err != nil {
		return "", err
	}
	return body, nil
}

// ReadFile 读取技能目录中的相对路径文件，用于加载 scripts/ 或 references/ 下的资源。
func (s *Skill) ReadFile(relPath string) (string, error) {
	fullPath := filepath.Join(s.Dir, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SkillRegistry 是线程安全的技能注册中心，管理所有已发现的技能。
type SkillRegistry struct {
	mu     sync.RWMutex      // 读写锁
	skills map[string]*Skill // 以技能名为 key 的技能映射表
	dir    string            // 技能发现目录路径；非空时支持 Reload()
}

// NewSkillRegistry 创建一个空的技能注册中心。
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{skills: make(map[string]*Skill)}
}

// Discover 扫描指定目录下的所有子目录，查找包含 SKILL.md 的技能目录并自动注册。
// 目录结构示例（OpenClaw Skills 规范）：
//
//	skills/
//	├── code-review/
//	│   └── SKILL.md
//	└── data-analysis/
//	    ├── SKILL.md
//	    ├── examples/
//	    │   └── sample.md
//	    └── scripts/
//	        └── analyze.py
//
// Discover 扫描指定目录，注册其中所有包含 SKILL.md 的子目录。
// 调用后会记住该目录路径，后续可通过 Reload() 重新扫描以拾取新增技能。
// 传入的路径会被规范化为绝对路径，确保 location 字段在任何工作目录下都有效。
func (r *SkillRegistry) Discover(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve skills directory: %w", err)
	}
	r.mu.Lock()
	r.dir = absDir // 记住绝对路径以支持 Reload
	r.mu.Unlock()
	return r.scan(absDir)
}

// Reload 重新扫描技能目录，拾取新增的技能，更新已修改技能的元数据。
// 已注册但目录已删除的技能会被移除。
func (r *SkillRegistry) Reload() error {
	r.mu.RLock()
	dir := r.dir
	r.mu.RUnlock()
	if dir == "" {
		return nil // 未通过 Discover 初始化目录，跳过
	}

	// 重新扫描，构建新快照
	newSkills := make(map[string]*Skill)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read skills directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		skillPath := filepath.Join(skillDir, "SKILL.md")
		s, err := loadSkill(skillPath, skillDir)
		if err != nil {
			continue
		}
		newSkills[s.Name] = s
	}

	// 原子替换：保留手动注册的技能（无 skillPath 的条目）
	r.mu.Lock()
	for name, s := range r.skills {
		if s.skillPath == "" { // 手动注册的技能（Register() 调用），不覆盖
			newSkills[name] = s
		}
	}
	r.skills = newSkills
	r.mu.Unlock()
	return nil
}

// scan 内部扫描实现，被 Discover 和 Reload 共用。
func (r *SkillRegistry) scan(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read skills directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		skillPath := filepath.Join(skillDir, "SKILL.md")
		s, err := loadSkill(skillPath, skillDir)
		if err != nil {
			continue
		}
		r.mu.Lock()
		r.skills[s.Name] = s
		r.mu.Unlock()
	}
	return nil
}

// Register 手动注册一个 Skill（可用于编程式注册，无需磁盘文件）。
func (r *SkillRegistry) Register(s *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name] = s
}

// Get 按名称获取技能。
func (r *SkillRegistry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// All 返回所有已注册的技能列表。
func (r *SkillRegistry) All() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		all = append(all, s)
	}
	return all
}

// Names 返回所有已注册技能名称的逗号分隔字符串。
func (r *SkillRegistry) Names() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// AvailableSkillsPrompt 生成 <openclaw_skills> XML 块，用于注入 system prompt。
// 遵循 OpenClaw Skills 规范，包含 name、description、tags 和 location（SKILL.md 路径）。
// Agent 判断任务匹配时，用 read_file 工具读取 location 指向的文件以获取完整指令。
func (r *SkillRegistry) AvailableSkillsPrompt() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<openclaw_skills>\n")
	for _, s := range r.skills {
		b.WriteString("<skill>\n")
		b.WriteString("  <name>")
		b.WriteString(s.Name)
		b.WriteString("</name>\n")
		b.WriteString("  <description>")
		b.WriteString(s.Description)
		b.WriteString("</description>\n")
		if len(s.Tags) > 0 {
			b.WriteString("  <tags>")
			b.WriteString(strings.Join(s.Tags, ", "))
			b.WriteString("</tags>\n")
		}
		if len(s.Tools) > 0 {
			b.WriteString("  <tools>")
			b.WriteString(strings.Join(s.Tools, ", "))
			b.WriteString("</tools>\n")
		}
		b.WriteString("  <location>")
		b.WriteString(s.skillPath)
		b.WriteString("</location>\n")
		b.WriteString("</skill>\n")
	}
	b.WriteString("</openclaw_skills>")
	return b.String()
}

// MatchTriggers 检查用户输入是否匹配某个技能的 triggers 关键词（大小写不敏感）。
// 返回所有匹配的技能列表。
func (r *SkillRegistry) MatchTriggers(input string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lower := strings.ToLower(input)
	var matched []*Skill
	for _, s := range r.skills {
		for _, trigger := range s.Triggers {
			if strings.Contains(lower, strings.ToLower(trigger)) {
				matched = append(matched, s)
				break // 一个技能只需匹配一个 trigger 即可
			}
		}
	}
	return matched
}

// ActivatedSkillsPrompt 将通过 trigger 自动激活的技能正文拼接为 prompt 块。
// 当用户输入命中技能 triggers 时，直接注入完整指令，无需 Agent 手动 read_file。
func (r *SkillRegistry) ActivatedSkillsPrompt(input string) string {
	matched := r.MatchTriggers(input)
	if len(matched) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<activated_skills>\n")
	for _, s := range matched {
		body, err := s.LoadBody()
		if err != nil {
			continue
		}
		b.WriteString("<skill name=\"")
		b.WriteString(s.Name)
		b.WriteString("\">\n")
		b.WriteString(body)
		b.WriteString("\n</skill>\n")
	}
	b.WriteString("</activated_skills>")
	return b.String()
}

// loadSkill 从 SKILL.md 文件加载一个技能的元数据。
// 解析 OpenClaw Skills 规范的 frontmatter 字段：name、description、version、author、tags、tools、triggers。
func loadSkill(path, dir string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fields, body, err := parseFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	name := fields["name"]
	desc := fields["description"]
	if name == "" {
		return nil, fmt.Errorf("parse %s: missing required 'name' field in frontmatter", path)
	}
	if desc == "" {
		return nil, fmt.Errorf("parse %s: missing required 'description' field in frontmatter", path)
	}
	_ = body // body 在激活时通过 LoadBody() 按需读取
	return &Skill{
		Name:        name,
		Description: desc,
		Version:     fields["version"],
		Author:      fields["author"],
		Tags:        splitList(fields["tags"]),
		Tools:       splitList(fields["tools"]),
		Triggers:    splitList(fields["triggers"]),
		Dir:         dir,
		skillPath:   path,
	}, nil
}

// splitList 将逗号分隔的字符串拆分为去空白的字符串切片。
// 支持 YAML 列表语法 "[a, b, c]" 和普通逗号分隔 "a, b, c"。
func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// 去除方括号（YAML 列表语法）
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseFrontmatter 解析 SKILL.md 的 YAML frontmatter 和 Markdown 正文。
// 返回 frontmatter 中所有键值对的 map 和正文内容。
// 支持 --- 分隔的 YAML frontmatter。
func parseFrontmatter(content string) (fields map[string]string, body string, err error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", fmt.Errorf("missing YAML frontmatter (first line must be ---)")
	}

	// 查找 frontmatter 结束标记
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return nil, "", fmt.Errorf("unclosed YAML frontmatter (missing closing ---)")
	}

	// 解析 frontmatter 中的键值对
	fields = parseSimpleYAML(lines[1:endIdx])

	if fields["name"] == "" {
		return nil, "", fmt.Errorf("missing required 'name' field in frontmatter")
	}
	if fields["description"] == "" {
		return nil, "", fmt.Errorf("missing required 'description' field in frontmatter")
	}

	// frontmatter 之后的内容为 body
	if endIdx+1 < len(lines) {
		body = strings.TrimSpace(strings.Join(lines[endIdx+1:], "\n"))
	}

	return fields, body, nil
}

// parseSimpleYAML 解析简单的 YAML 键值对，支持多行值（> 和 | 块标量）。
// 这是一个轻量级实现，无需引入外部 YAML 库，仅处理 SKILL.md frontmatter 中常见的格式。
func parseSimpleYAML(lines []string) map[string]string {
	fields := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n")))

	var currentKey string
	var currentValue strings.Builder
	var multiLine bool

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// 跳过空行和注释
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 检查是否为续行（以空格或制表符开头）
		if multiLine && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")) {
			if currentValue.Len() > 0 {
				currentValue.WriteString(" ")
			}
			currentValue.WriteString(trimmed)
			continue
		}

		// 保存上一个 key 的值
		if currentKey != "" {
			fields[currentKey] = currentValue.String()
		}

		// 解析新的 key: value
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		currentKey = strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		currentValue.Reset()

		// 处理 YAML 块标量指示符（> 折叠，| 保留换行）
		if val == ">" || val == "|" {
			multiLine = true
			continue
		}

		multiLine = true
		// 去除引号
		val = strings.Trim(val, "\"'")
		currentValue.WriteString(val)
	}

	// 保存最后一个 key
	if currentKey != "" {
		fields[currentKey] = currentValue.String()
	}

	return fields
}
