package prompt

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"cece/internal/tool"
)

// ToolListProvider abstracts tool definition retrieval.
// This avoids the prompt package directly constructing a tool.Registry.
type ToolListProvider interface {
	Definitions() []tool.Definition
}

// SkillListProvider abstracts skill listing retrieval.
type SkillListProvider interface {
	Listing() string
}

// DefaultSessionCollector gathers session-level context:
// environment info, project instructions, and tool summaries.
type DefaultSessionCollector struct {
	repoRoot      string
	toolProvider  ToolListProvider
	skillProvider SkillListProvider
	loader        *InstructionLoader
}

func NewDefaultSessionCollector(repoRoot string, tp ToolListProvider) *DefaultSessionCollector {
	return &DefaultSessionCollector{
		repoRoot:     repoRoot,
		toolProvider: tp,
		loader:       NewInstructionLoader(repoRoot),
	}
}

func (d *DefaultSessionCollector) SetSkillProvider(sp SkillListProvider) {
	d.skillProvider = sp
}

func (d *DefaultSessionCollector) Collect(ctx context.Context) (SessionContext, error) {
	sc := SessionContext{
		RepoRoot:  d.repoRoot,
		IsGitRepo: d.isGitRepo(),
		OSName:    runtime.GOOS,
		OSVersion: d.osVersion(),
	}

	if sc.IsGitRepo {
		sc.SessionStartBranch = d.gitBranch()
	}

	claudemd, err := d.loader.Load()
	if err != nil {
		return sc, err
	}
	sc.CLAUDEmd = claudemd

	if d.toolProvider != nil {
		defs := d.toolProvider.Definitions()
		sc.ToolDescriptions = FormatToolDescriptionsText(defs)
	}

	if d.skillProvider != nil {
		sc.SkillListing = d.skillProvider.Listing()
	}

	return sc, nil
}

func (d *DefaultSessionCollector) isGitRepo() bool {
	_, err := os.Stat(d.repoRoot + "/.git")
	return err == nil
}

func (d *DefaultSessionCollector) gitBranch() string {
	cmd := exec.Command("git", "-C", d.repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (d *DefaultSessionCollector) osVersion() string {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return runtime.GOOS
}
