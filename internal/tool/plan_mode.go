package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/zhanglvtao/cece/internal/skill"
)

const (
	EnterPlanModeToolName = "EnterPlanMode"
	ExitPlanModeToolName  = "ExitPlanMode"

	DefaultPlanModeMockupAllowPattern = ".superpowers/brainstorm/**/content/**"
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

func BuildFullPlanReminder(plansDir string, hasWritingPlanSkill bool, allowedWritePaths ...string) string {
	skillHint := ""
	if hasWritingPlanSkill {
		skillHint = "\n\n## Skill\n" +
			"Use the Skill tool to load the `writing-plan` skill for plan writing guidance.\n"
	}
	return "<system-reminder>\n" +
		"You are already in plan mode. You MUST NOT make code edits or run non-readonly commands.\n" +
		"You may only use Read, Grep, Glob, and Bash for read-only commands such as\n" +
		"ls, pwd, git status, git log, git diff, find, grep, cat, head, and tail.\n" +
		"Do not run commands that modify state, including mkdir, touch, rm, mv, cp,\n" +
		"redirection writes (> or >>), heredocs that write files, git add/commit/push/checkout/reset,\n" +
		"package installs, config changes, or generated-file commands.\n" +
		"\n" +
		"## Plan File\n" +
		planFileInfo(plansDir) + "\n" +
		"\n" +
		"## Allowed Plan-Mode Write Artifacts\n" +
		planModeWriteInfo(plansDir, allowedWritePaths) + "\n" +
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
		"### Asking Good Questions\n" +
		"- Never ask what you could find out by reading the code.\n" +
		"- Batch related questions together.\n" +
		"- Focus on things only the user can answer: requirements, preferences, tradeoffs, and edge-case priorities.\n" +
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
		"### Bugfix Plans\n" +
		"For bugfix tasks, include the failing behavior or reproduction matrix, root cause hypothesis, minimal fix location, and verification commands covering every concrete input shape from the issue.\n" +
		"\n" +
		"### When to Converge\n" +
		"Your plan is ready when it covers what to change, files to modify, existing code to reuse, risks or edge cases, and how to verify the result.\n" +
		"\n" +
		"### Ending Your Turn\n" +
		"Your turn should only end by either:\n" +
		"- Calling AskUserQuestion to ask the user a clarifying question\n" +
		"- Calling ExitPlanMode with the plan_file parameter when the plan is ready for approval\n" +
		"\n" +
		"**Important:** Use ExitPlanMode to request plan approval. Do NOT ask about\n" +
		"plan approval via text or AskUserQuestion.\n" +
		skillHint +
		"</system-reminder>"
}

func BuildSparsePlanReminder(plansDir string, hasWritingPlanSkill bool, allowedWritePaths ...string) string {
	skillHint := ""
	if hasWritingPlanSkill {
		skillHint = " Use the Skill tool to load `writing-plan` for guidance.\n"
	}
	return "<system-reminder>\n" +
		fmt.Sprintf("Plan mode still active. Read-only except plan files under %s/ and allowed artifacts: %s. DO NOT write to project root.\n", plansDir, strings.Join(planModeAllowedWriteLabels(plansDir, allowedWritePaths), ", ")) +
		"Continue iterative workflow. Converge only when the plan covers reuse, risks, and verification. End turns with AskUserQuestion or ExitPlanMode.\n" +
		skillHint +
		"</system-reminder>"
}

func planModeWriteInfo(plansDir string, allowedWritePaths []string) string {
	labels := planModeAllowedWriteLabels(plansDir, allowedWritePaths)
	var b strings.Builder
	for _, label := range labels {
		b.WriteString("- ")
		b.WriteString(label)
		b.WriteString("\n")
	}
	b.WriteString("Do not write outside these paths while plan mode is active.")
	return strings.TrimRight(b.String(), "\n")
}

func planModeAllowedWriteLabels(plansDir string, allowedWritePaths []string) []string {
	labels := []string{plansDir + "/**"}
	for _, p := range normalizePlanModeAllowPatterns(allowedWritePaths) {
		labels = append(labels, p)
	}
	return labels
}

func normalizePlanModeAllowPatterns(patterns []string) []string {
	seen := make(map[string]struct{}, len(patterns))
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(filepath.ToSlash(pattern))
		if pattern == "" {
			continue
		}
		for strings.HasPrefix(pattern, "./") {
			pattern = strings.TrimPrefix(pattern, "./")
		}
		if _, ok := seen[pattern]; ok {
			continue
		}
		seen[pattern] = struct{}{}
		out = append(out, pattern)
	}
	return out
}

func isPlanModeWriteAllowed(projectDir, plansDir string, patterns []string, target string) bool {
	if containsParentTraversal(target) {
		return false
	}
	targetAbs, ok := planModeAbsPath(projectDir, target)
	if !ok {
		return false
	}
	projectAbs, hasProject := planModeAbsDir(projectDir)
	if hasProject {
		resolvedProject := resolvePlanModeExistingPrefix(projectAbs)
		resolvedTarget := resolvePlanModeExistingPrefix(targetAbs)
		if !pathWithin(resolvedTarget, resolvedProject) {
			return false
		}
	}
	if plansDir != "" {
		if plansAbs, ok := planModeAbsDir(plansDir); ok && pathWithin(targetAbs, plansAbs) {
			return true
		}
	}
	if !hasProject {
		return false
	}
	rel, err := filepath.Rel(projectAbs, targetAbs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, pattern := range normalizePlanModeAllowPatterns(patterns) {
		pattern, ok := projectRelativeAllowPattern(projectAbs, pattern)
		if !ok {
			continue
		}
		if globMatch(pattern, rel) {
			return true
		}
	}
	return false
}

func planModeAbsPath(projectDir, p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) && strings.TrimSpace(projectDir) != "" {
		p = filepath.Join(projectDir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

func planModeAbsDir(dir string) (string, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

func projectRelativeAllowPattern(projectAbs, pattern string) (string, bool) {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	if pattern == "" || strings.Contains(pattern, "..") {
		return "", false
	}
	if filepath.IsAbs(filepath.FromSlash(pattern)) {
		abs, err := filepath.Abs(filepath.FromSlash(pattern))
		if err != nil || !pathWithin(abs, projectAbs) {
			return "", false
		}
		rel, err := filepath.Rel(projectAbs, abs)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return "", false
		}
		pattern = filepath.ToSlash(rel)
	}
	for strings.HasPrefix(pattern, "./") {
		pattern = strings.TrimPrefix(pattern, "./")
	}
	return pattern, true
}

func containsParentTraversal(p string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func resolvePlanModeExistingPrefix(p string) string {
	p = filepath.Clean(p)
	prefix := p
	var suffix []string
	for {
		if _, err := os.Stat(prefix); err == nil {
			if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
				for i := len(suffix) - 1; i >= 0; i-- {
					resolved = filepath.Join(resolved, suffix[i])
				}
				return filepath.Clean(resolved)
			}
			break
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
		suffix = append(suffix, filepath.Base(prefix))
		prefix = parent
	}
	return p
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path+string(os.PathSeparator), root+string(os.PathSeparator))
}

func globMatch(pattern, value string) bool {
	re, err := regexp.Compile(globRegex(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

func globRegex(pattern string) string {
	pattern = filepath.ToSlash(pattern)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '/':
			b.WriteByte('/')
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}

const ExitPlanModeReminderText = "<system-reminder>\n" +
	"Exited plan mode. You may now implement the approved plan.\n" +
	"</system-reminder>"

func ExitPlanModeReminder() string {
	return ExitPlanModeReminderText
}

// ── PlanModeState ───────────────────────────────────────────────────────────

type PlanModeState struct {
	mu                     sync.Mutex
	mode                   PermissionMode
	prePlanMode            PermissionMode
	plansDir               string // e.g. /xxx/.cece/plans
	reminderType           string // "full" | "sparse" | ""
	projectDir             string
	planWriteAllowPatterns []string
	exitTargetMode         PermissionMode // set before ApprovePlan; Exit() uses this instead of prePlanMode
	pendingModeReminder    string         // non-empty when mode changed; drained before next LLM call
	skillStore             *skill.Store   // optional; used to detect writing-plan skill
}

func NewPlanModeState() *PlanModeState {
	return &PlanModeState{
		mode:                   PermissionModeDefault,
		planWriteAllowPatterns: []string{DefaultPlanModeMockupAllowPattern},
	}
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
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	s.projectDir = dir
}

func (s *PlanModeState) ProjectDir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectDir
}

func (s *PlanModeState) SetPlanModeWriteAllowPatterns(patterns []string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.planWriteAllowPatterns = normalizePlanModeAllowPatterns(append([]string{DefaultPlanModeMockupAllowPattern}, patterns...))
}

func (s *PlanModeState) PlanModeWriteAllowPatterns() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.planWriteAllowPatterns) == 0 {
		return []string{DefaultPlanModeMockupAllowPattern}
	}
	return append([]string(nil), s.planWriteAllowPatterns...)
}

func (s *PlanModeState) PlanModeAllowedWriteLabels() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	patterns := s.planWriteAllowPatterns
	if len(patterns) == 0 {
		patterns = []string{DefaultPlanModeMockupAllowPattern}
	}
	return planModeAllowedWriteLabels(s.plansDir, patterns)
}

func (s *PlanModeState) IsPlanModeWriteAllowed(path string) bool {
	if s == nil || strings.TrimSpace(path) == "" {
		return false
	}
	s.mu.Lock()
	projectDir := s.projectDir
	plansDir := s.plansDir
	patterns := append([]string(nil), s.planWriteAllowPatterns...)
	s.mu.Unlock()
	if len(patterns) == 0 {
		patterns = []string{DefaultPlanModeMockupAllowPattern}
	}
	return isPlanModeWriteAllowed(projectDir, plansDir, patterns, path)
}

func (s *PlanModeState) SetSkillStore(store *skill.Store) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skillStore = store
}

// HasWritingPlanSkill reports whether the writing-plan skill exists and is enabled.
func (s *PlanModeState) HasWritingPlanSkill() bool {
	if s == nil || s.skillStore == nil {
		return false
	}
	sk, ok := s.skillStore.Get("writing-plan")
	return ok && s.skillStore.IsEnabled(sk.Name)
}

// SetExitTargetMode sets the target mode for Exit() to use instead of prePlanMode.
// Called before ApprovePlan to avoid SetMode racing with Exit().
func (s *PlanModeState) SetExitTargetMode(mode PermissionMode) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exitTargetMode = mode
}

// DrainModeReminder returns and clears the pending mode change reminder.
// Called before each LLM call in the agent loop.
func (s *PlanModeState) DrainModeReminder() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.pendingModeReminder
	s.pendingModeReminder = ""
	return r
}

// modeReminderFor returns the reminder text for a given mode transition.
func modeReminderFor(mode PermissionMode) string {
	switch mode {
	case PermissionModeAutoAccept:
		return "<system-reminder>\nSwitched to auto-accept mode. All tool calls are pre-approved.\n</system-reminder>"
	case PermissionModePlan:
		return "" // plan mode reminders are handled separately via reminderType
	default:
		return "<system-reminder>\nSwitched to default mode. Write-effect tools require confirmation.\n</system-reminder>"
	}
}

func (s *PlanModeState) SetMode(mode PermissionMode) PermissionMode {
	if s == nil {
		return PermissionModeDefault
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if mode != PermissionModeDefault && mode != PermissionModeAutoAccept && mode != PermissionModePlan {
		mode = PermissionModeDefault
	}
	if s.mode == "" {
		s.mode = PermissionModeDefault
	}
	if s.mode != PermissionModePlan && mode == PermissionModePlan {
		s.prePlanMode = s.mode
		dir := s.projectDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		s.plansDir = plansDirFor(dir)
		s.reminderType = "full"
		os.MkdirAll(s.plansDir, 0o755)
	}
	if s.mode == PermissionModePlan && mode != PermissionModePlan {
		s.prePlanMode = ""
		s.plansDir = ""
		s.reminderType = ""
	}
	oldMode := s.mode
	s.mode = mode
	// Only set reminder when mode actually changed.
	if oldMode != mode && modeReminderFor(mode) != "" {
		s.pendingModeReminder = modeReminderFor(mode)
	}
	return s.mode
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
	// Determine target mode: exitTargetMode overrides prePlanMode.
	targetMode := s.prePlanMode
	if s.exitTargetMode != "" && s.exitTargetMode != PermissionModePlan {
		targetMode = s.exitTargetMode
	}
	oldMode := s.mode
	s.mode = targetMode
	s.prePlanMode = ""
	s.exitTargetMode = ""
	s.plansDir = ""
	s.reminderType = ""
	// Set reminder when mode actually changed.
	if oldMode != targetMode && modeReminderFor(targetMode) != "" {
		s.pendingModeReminder = modeReminderFor(targetMode)
	}
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
	return Result{Content: BuildFullPlanReminder(t.state.PlansDir(), t.state.HasWritingPlanSkill(), t.state.PlanModeWriteAllowPatterns()...)}
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

	candidate := args.PlanFile
	if !filepath.IsAbs(candidate) && plansDir != "" {
		candidate = filepath.Join(plansDir, candidate)
	}
	abs, err := filepath.Abs(candidate)
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
