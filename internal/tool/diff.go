package tool

import (
	udiff "github.com/aymanbagabas/go-udiff"
)

// UnifiedDiff generates a unified diff between old and new content.
// oldPath/newPath are used in the ---/+++ headers.
//
// It delegates to go-udiff, which implements the Myers diff algorithm
// (linear space, O((m+n)·d) time in the amount of change d). This avoids the
// O(m·n) full DP table an LCS implementation would allocate over the entire
// file — a single edit to a large file no longer risks OOM.
func UnifiedDiff(oldPath, newPath, oldContent, newContent string) string {
	out := udiff.Unified("a/"+oldPath, "b/"+newPath, oldContent, newContent)
	if out == "" {
		// go-udiff returns "" when the inputs are identical. Preserve the
		// historical contract (and keep the UI diff detector happy) by
		// emitting the header with no hunks.
		return "--- a/" + oldPath + "\n+++ b/" + newPath + "\n"
	}
	return out
}
