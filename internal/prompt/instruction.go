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

// Load 读取项目根目录的 CLAUDE.md；文件不存在返回空串（非 error）。
func (l *InstructionLoader) Load() (string, error) {
	path := filepath.Join(l.repoRoot, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
