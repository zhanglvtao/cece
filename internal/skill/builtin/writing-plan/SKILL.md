---
name: writing-plan
description: Plan mode 写作指导 — 教你编写清晰、结构化、可执行的实施计划
---

# Writing a Plan

When writing an implementation plan, follow these principles:

## Structure

Every plan must include these sections:

- **Context**: Why this change is needed. One paragraph max.
- **Approach**: Your recommended implementation strategy. Be specific about the "how", not just the "what".
- **Files to modify**: List each file path and describe what changes in each. Use relative paths from project root.
- **Reuse**: Existing functions, utilities, or patterns to reuse — with file paths. Don't reinvent.
- **Verification**: How to test the changes end-to-end. Include exact commands if possible.

## Writing Style

- **Concrete over vague**: "Add a `validateEmail` function in `internal/validate/email.go`" beats "Add email validation".
- **Minimal over exhaustive**: Only include changes needed for the task. No speculative improvements.
- **Dependencies first**: If step B depends on step A, state that explicitly.
- **One approach**: Pick the best approach and commit to it. Don't list alternatives unless the user asked for them.

## Anti-patterns to Avoid

- Don't write a plan that just restates the task without adding implementation details.
- Don't leave "TBD" sections — if you don't know, ask the user with AskUserQuestion.
- Don't propose changes to files you haven't read in this conversation.
- Don't plan cosmetic changes (comments, refactoring) beyond what the task requires.

## Before Finishing

Before calling ExitPlanMode, verify:

1. Every file mentioned has been read or explored.
2. The plan is specific enough that someone could implement it without asking clarifying questions.
3. No step depends on unwritten helper functions without specifying where they live.
