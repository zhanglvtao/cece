package tool

import (
	"fmt"
	"strings"
)

// UnifiedDiff generates a unified diff between old and new content.
// oldPath/newPath are used in the ---/+++ headers.
func UnifiedDiff(oldPath, newPath, oldContent, newContent string) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	ops := lcsDiff(oldLines, newLines)

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", oldPath)
	fmt.Fprintf(&b, "+++ b/%s\n", newPath)

	// Group ops into hunks with context lines.
	hunks := groupHunks(ops, 3)
	for _, hunk := range hunks {
		oldStart, oldCount, newStart, newCount := hunkBounds(hunk)
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, op := range hunk {
			switch op.kind {
			case opEqual:
				fmt.Fprintf(&b, " %s\n", op.line)
			case opDelete:
				fmt.Fprintf(&b, "-%s\n", op.line)
			case opInsert:
				fmt.Fprintf(&b, "+%s\n", op.line)
			}
		}
	}

	return b.String()
}

// diffOp represents a single line in the diff output.
type diffOpKind int

const (
	opEqual  diffOpKind = iota
	opDelete            // line from old
	opInsert            // line from new
)

type diffOp struct {
	kind diffOpKind
	line string
}

// lcsDiff computes the edit operations using the LCS algorithm.
func lcsDiff(old, new []string) []diffOp {
	m, n := len(old), len(new)

	// DP table for LCS length
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if old[i-1] == new[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	// Backtrack to produce ops in reverse order.
	var ops []diffOp
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && old[i-1] == new[j-1] {
			ops = append(ops, diffOp{opEqual, old[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, diffOp{opInsert, new[j-1]})
			j--
		} else {
			ops = append(ops, diffOp{opDelete, old[i-1]})
			i--
		}
	}

	// Reverse
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}

	return ops
}

// hunkEntry is a diffOp grouped into a hunk.
type hunkEntry struct {
	diffOp
	oldLine int // 1-based line number in old (0 if insert)
	newLine int // 1-based line number in new (0 if delete)
}

// groupHunks groups ops into hunks with context lines.
// Adjacent changes within context lines of each other are merged.
func groupHunks(ops []diffOp, context int) [][]hunkEntry {
	// Assign line numbers.
	entries := make([]hunkEntry, len(ops))
	oldLine, newLine := 1, 1
	for i, op := range ops {
		e := hunkEntry{diffOp: op}
		switch op.kind {
		case opEqual:
			e.oldLine = oldLine
			e.newLine = newLine
			oldLine++
			newLine++
		case opDelete:
			e.oldLine = oldLine
			oldLine++
		case opInsert:
			e.newLine = newLine
			newLine++
		}
		entries[i] = e
	}

	// Find indices of change ops.
	var changeIdx []int
	for i, e := range entries {
		if e.kind != opEqual {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return nil
	}

	// Group change indices into hunks (merge if within 2*context of each other).
	var groups [][]int
	groupStart := changeIdx[0]
	groupEnd := changeIdx[0]
	for _, idx := range changeIdx[1:] {
		if idx-groupEnd <= 2*context {
			groupEnd = idx
		} else {
			groups = append(groups, []int{groupStart, groupEnd})
			groupStart = idx
			groupEnd = idx
		}
	}
	groups = append(groups, []int{groupStart, groupEnd})

	// Expand each group into a hunk with context.
	var hunks [][]hunkEntry
	for _, g := range groups {
		start := g[0] - context
		if start < 0 {
			start = 0
		}
		end := g[1] + context
		if end >= len(entries) {
			end = len(entries) - 1
		}
		hunk := make([]hunkEntry, end-start+1)
		copy(hunk, entries[start:end+1])
		hunks = append(hunks, hunk)
	}

	return hunks
}

func hunkBounds(hunk []hunkEntry) (oldStart, oldCount, newStart, newCount int) {
	if len(hunk) == 0 {
		return 1, 0, 1, 0
	}
	oldStart = 0
	newStart = 0
	for _, e := range hunk {
		if e.kind == opEqual || e.kind == opDelete {
			if oldStart == 0 || e.oldLine < oldStart {
				oldStart = e.oldLine
			}
			oldCount++
		}
		if e.kind == opEqual || e.kind == opInsert {
			if newStart == 0 || e.newLine < newStart {
				newStart = e.newLine
			}
			newCount++
		}
	}
	if oldStart == 0 {
		oldStart = 1
	}
	if newStart == 0 {
		newStart = 1
	}
	return
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty line from trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
