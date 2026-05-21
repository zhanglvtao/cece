package ui

import (
	"strings"
	"testing"
)

func testDiffStyles() DiffStyles {
	return DefaultStyles().Chat.Diff
}

func TestRenderDiffSummary(t *testing.T) {
	diff := "--- a/test.txt\n+++ b/test.txt\n@@ -1,3 +1,3 @@\n line1\n-old line2\n+new line2\n line3\n"
	result := RenderDiff(diff, testDiffStyles(), 80)
	if !strings.Contains(result, "Added 1 lines, removed 1 lines") {
		t.Fatalf("missing summary, got: %q", result)
	}
}

func TestRenderDiffLineNumbers(t *testing.T) {
	diff := "--- a/test.txt\n+++ b/test.txt\n@@ -1,3 +1,3 @@\n line1\n-old line2\n+new line2\n line3\n"
	result := RenderDiff(diff, testDiffStyles(), 80)
	if !strings.Contains(result, "1") {
		t.Fatalf("missing line number 1, got: %q", result)
	}
}

func TestRenderDiffMarkers(t *testing.T) {
	diff := "--- a/test.txt\n+++ b/test.txt\n@@ -1,3 +1,3 @@\n line1\n-old line2\n+new line2\n line3\n"
	result := RenderDiff(diff, testDiffStyles(), 80)
	if !strings.Contains(result, "+") || !strings.Contains(result, "-") {
		t.Fatalf("missing + or - markers, got: %q", result)
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	result := RenderDiff("", testDiffStyles(), 80)
	if result != "" {
		t.Fatalf("expected empty result for empty diff, got: %q", result)
	}
}

func TestRenderDiffInsertOnly(t *testing.T) {
	diff := "--- a/test.txt\n+++ b/test.txt\n@@ -0,0 +1,2 @@\n+line1\n+line2\n"
	result := RenderDiff(diff, testDiffStyles(), 80)
	if !strings.Contains(result, "Added 2 lines, removed 0 lines") {
		t.Fatalf("expected insert-only summary, got: %q", result)
	}
}

func TestRenderDiffDeleteOnly(t *testing.T) {
	diff := "--- a/test.txt\n+++ b/test.txt\n@@ -1,2 +0,0 @@\n-line1\n-line2\n"
	result := RenderDiff(diff, testDiffStyles(), 80)
	if !strings.Contains(result, "Added 0 lines, removed 2 lines") {
		t.Fatalf("expected delete-only summary, got: %q", result)
	}
}

func TestParseUnifiedDiffContextLines(t *testing.T) {
	diff := "--- a/test.txt\n+++ b/test.txt\n@@ -1,3 +1,3 @@\n context\n-deleted\n+inserted\n"
	lines := parseUnifiedDiff(diff)

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0].kind != "context" || lines[0].lineNum != 1 {
		t.Fatalf("line 0: expected context lineNum=1, got %+v", lines[0])
	}
	if lines[1].kind != "delete" || lines[1].lineNum != 2 {
		t.Fatalf("line 1: expected delete lineNum=2, got %+v", lines[1])
	}
	if lines[2].kind != "insert" || lines[2].lineNum != 2 {
		t.Fatalf("line 2: expected insert lineNum=2, got %+v", lines[2])
	}
}
