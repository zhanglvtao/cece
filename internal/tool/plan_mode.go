package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	EnterPlanModeToolName = "EnterPlanMode"
	ExitPlanModeToolName  = "ExitPlanMode"
)

type PermissionMode string

const (
	PermissionModeDefault    PermissionMode = "default"
	PermissionModeAutoAccept PermissionMode = "auto-accept"
	PermissionModePlan       PermissionMode = "plan"
)

// ── Plan file path helpers ──────────────────────────────────────────────────

func plansDirFor(projectDir string) string {
	return filepath.Join(projectDir, ".cece", "plans")
}

// ── Prompt builders ─────────────────────────────────────────────────────────

func planFileInfo(plansDir string) string {
	return fmt.Sprintf(
		"**Plan file path: %s/** (e.g., %s/add-auth.md)\n"+
			"Use the Write tool with a path UNDER this directory. "+
			"DO NOT write the plan to the project root (e.g., PLAN.md at project root is WRONG).",
		plansDir, plansDir,
	)
}

func BuildFullPlanReminder(plansDir string) string {
	return "<system-reminder>\n" +
		"Plan mode is active. You MUST NOT make any edits or run non-readonly commands.\n" +
		"You may only use Read, Grep, Glob, and Bash for read-only commands such as\n" +
		"ls, pwd, git status, git log, git diff, find, grep, cat, head, and tail.\n" +
		"Do not run commands that modify state, including mkdir, touch, rm, mv, cp,\n" +
		"redirection writes (> or >>), heredocs that write files, git add/commit/push/checkout/reset,\n" +
		"package installs, config changes, or generated-file commands.\n" +
		"\n" +
		"## Plan File\n" +
		planFileInfo(plansDir) + "\n" +
		"This is the ONLY file you are allowed to edit.\n" +
		"\n" +
		"## Iterative Planning Workflow\n" +
		"You are pair-planning with the user. Explore the code, ask questions when you\n" +
		"hit decisions you can't make alone, and write findings into the plan file.\n" +
		"\n" +
		"### The Loop\n" +
		"1. **Explore** — Use Read, Grep, Glob, and read-only Bash to understand the code.\n" +
		"2. **Update the plan file** — After each discovery, immediately write what you\n" +
		"   learned. Don't wait until the end.\n" +
		"3. **Ask the user** — When you hit an ambiguity only the user can resolve, use\n" +
		"   the AskUserQuestion tool. Then go back to step 1.\n" +
		"\n" +
		"### First Turn\n" +
		"Quickly scan key files to understand the task scope. Then write a skeleton plan\n" +
		"(headers and rough notes) and ask the user your first questions using\n" +
		"AskUserQuestion. Don't explore exhaustively before engaging the user.\n" +
		"\n" +
		"### Plan File Structure\n" +
		"- **Context**: Why this change is being made\n" +
		"- **Approach**: Your recommended implementation approach\n" +
		"- **Files to modify**: Paths and what changes in each\n" +
		"- **Reuse**: Existing functions/utilities to reuse, with file paths\n" +
		"- **Verification**: How to test the changes end-to-end\n" +
		"\n" +
		"### Ending Your Turn\n" +
		"Your turn should only end by either:\n" +
		"- Calling AskUserQuestion to ask the user a clarifying question\n" +
		"- Calling ExitPlanMode with the plan_file parameter when the plan is ready for approval\n" +
		"\n" +
		"**Important:** Use ExitPlanMode to request plan approval. Do NOT ask about\n" +
		"plan approval via text or AskUserQuestion.\n" +
		"</system-reminder>"
}

func BuildSparsePlanReminder(plansDir string) string {
	return "<system-reminder>\n" +
		fmt.Sprintf("Plan mode still active. Read-only except plan files under %s/. DO NOT write to project root.\n", plansDir) +
		"Continue iterative workflow. End turns with AskUserQuestion or ExitPlanMode.\n" +
		"</system-reminder>"
}

const ExitPlanModeReminderText = "<system-reminder>\n" +
	"Exited plan mode. You may now implement the approved plan.\n" +
	"</system-reminder>"

func ExitPlanModeReminder() string {
	return ExitPlanModeReminderText
}

// ── PlanModeState ───────────────────────────────────────────────────────────

type PlanModeState struct {
	mu           sync.Mutex
	mode         PermissionMode
	prePlanMode  PermissionMode
	plansDir     string // e.g. /xxx/.cece/plans
	reminderType string // "full" | "sparse" | ""
	projectDir   string
}

func NewPlanModeState() *PlanModeState {
	return &PlanModeState{mode: PermissionModeDefault}
}

func (s *PlanModeState) Mode() PermissionMode {
	if s == nil {
		return PermissionModeDefault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode == "" {
		return PermissionModeDefault
	}
	return s.mode
}

func (s *PlanModeState) PlansDir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plansDir
}

func (s *PlanModeState) ReminderType() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reminderType
}

func (s *PlanModeState) SetReminderType(t string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reminderType = t
}

func (s *PlanModeState) SetProjectDir(dir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectDir = dir
}

func (s *PlanModeState) Enter() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode == PermissionModePlan {
		return false
	}
	if s.mode == "" {
		s.mode = PermissionModeDefault
	}
	s.prePlanMode = s.mode
	s.mode = PermissionModePlan

	dir := s.projectDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	s.plansDir = plansDirFor(dir)
	s.reminderType = "full"

	os.MkdirAll(s.plansDir, 0o755)

	return true
}

func (s *PlanModeState) Exit() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode != PermissionModePlan {
		return false
	}
	if s.prePlanMode == "" || s.prePlanMode == PermissionModePlan {
		s.prePlanMode = PermissionModeDefault
	}
	s.mode = s.prePlanMode
	s.prePlanMode = ""
	s.plansDir = ""
	s.reminderType = ""
	return true
}

// CycleMode advances to the next mode in the cycle:
// Default -> AutoAccept -> Plan -> Default.
// It handles plan-mode side effects (plansDir, reminderType) but does NOT
// use the prePlanMode save/restore pattern — that is reserved for
// LLM-triggered Enter/Exit.
func (s *PlanModeState) CycleMode() PermissionMode {
	if s == nil {
		return PermissionModeDefault
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var next PermissionMode
	switch s.mode {
	case PermissionModeAutoAccept:
		next = PermissionModePlan
	case PermissionModePlan:
		next = PermissionModeDefault
	default: // PermissionModeDefault or ""
		next = PermissionModeAutoAccept
	}

	// Handle plan-mode entry side effects
	if s.mode != PermissionModePlan && next == PermissionModePlan {
		s.prePlanMode = s.mode
		dir := s.projectDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		s.plansDir = plansDirFor(dir)
		s.reminderType = "full"
		os.MkdirAll(s.plansDir, 0o755)
	}

	// Handle plan-mode exit side effects
	if s.mode == PermissionModePlan && next != PermissionModePlan {
		s.prePlanMode = ""
		s.plansDir = ""
		s.reminderType = ""
	}

	s.mode = next
	return next
}

// ── EnterPlanMode tool ──────────────────────────────────────────────────────

type enterPlanModeTool struct {
	state *PlanModeState
}

func NewEnterPlanMode(state *PlanModeState) Tool {
	return enterPlanModeTool{state: state}
}

func (enterPlanModeTool) Effect() Effect { return EffectMode }

func (enterPlanModeTool) Info() Definition {
	return Definition{
		Name:        EnterPlanModeToolName,
		Description: "Request permission to enter plan mode before making non-trivial code changes. In plan mode, explore the codebase read-only and design an implementation plan for user approval.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t enterPlanModeTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.state == nil {
		return Result{Content: "plan mode state is not configured", IsError: true}
	}
	if !t.state.Enter() {
		return Result{Content: "Already in plan mode.", IsError: true}
	}
	return Result{Content: BuildFullPlanReminder(t.state.PlansDir())}
}

// ── ExitPlanMode tool ───────────────────────────────────────────────────────

type exitPlanModeTool struct {
	state *PlanModeState
}

func NewExitPlanMode(state *PlanModeState) Tool {
	return exitPlanModeTool{state: state}
}

func (exitPlanModeTool) Effect() Effect { return EffectMode }

func (exitPlanModeTool) Info() Definition {
	return Definition{
		Name:        ExitPlanModeToolName,
		Description: "Use this tool when plan mode is active and you have finished writing the implementation plan to the plan file. It asks the user to approve the plan before any code changes are made.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_file": map[string]any{
					"type":        "string",
					"description": "The path of the plan file you wrote (must be under the plans directory).",
				},
			},
			"required": []string{"plan_file"},
		},
	}
}

func (t exitPlanModeTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.state == nil {
		return Result{Content: "plan mode state is not configured", IsError: true}
	}
	plansDir := t.state.PlansDir()

	var args struct {
		PlanFile string `json:"plan_file"`
	}
	if err := json.Unmarshal(input, &args); err != nil || args.PlanFile == "" {
		return Result{Content: "plan_file parameter is required. Provide the path of the plan file you wrote.", IsError: true}
	}

	abs, err := filepath.Abs(args.PlanFile)
	if err != nil {
		return Result{Content: fmt.Sprintf("Invalid plan_file path: %s", args.PlanFile), IsError: true}
	}
	if !(strings.HasPrefix(abs+string(os.PathSeparator), plansDir+string(os.PathSeparator)) || abs == plansDir) {
		return Result{Content: fmt.Sprintf("plan_file must be under %s/. Got: %s", plansDir, args.PlanFile), IsError: true}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{Content: fmt.Sprintf("Failed to read plan file %s: %v", args.PlanFile, err), IsError: true}
	}
	plan := string(data)
	if strings.TrimSpace(plan) == "" {
		return Result{Content: fmt.Sprintf("Plan file %s is empty. Write your plan before calling ExitPlanMode.", args.PlanFile), IsError: true}
	}

	if !t.state.Exit() {
		return Result{Content: "ExitPlanMode can only be used while plan mode is active.", IsError: true}
	}
	return Result{Content: ExitPlanModeReminderText + "\n\n## Approved Plan:\n" + plan}
}
