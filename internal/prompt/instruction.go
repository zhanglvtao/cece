package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

type InstructionLoader struct {
	repoRoot string
}

func NewInstructionLoader(repoRoot string) *InstructionLoader {
	return &InstructionLoader{repoRoot: repoRoot}
}

// Load 优先读取项目根目录的 AGENTS.md，不存在则读取 CLAUDE.md。
// 两个文件都不存在返回空串（非 error）。
func (l *InstructionLoader) Load() (string, error) {
	// 优先 AGENTS.md
	data, err := l.readFile("AGENTS.md")
	if err != nil {
		return "", err
	}
	if data != "" {
		return data, nil
	}
	// 退回 CLAUDE.md
	return l.readFile("CLAUDE.md")
}

func (l *InstructionLoader) readFile(name string) (string, error) {
	path := filepath.Join(l.repoRoot, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
