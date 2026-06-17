package contract_test

import (
	"testing"

	"github.com/zhanglvtao/cece/internal/evals"
	"github.com/zhanglvtao/cece/internal/testkit"
)

func TestAgentContract_ShortInlineResult(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.TextTurn("short answer"))
	h := testkit.NewHarness(t, llm)
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "short-preview-no-read",
		Layer:       evals.LayerContract,
		Description: "short result should not require expansion",
		Prompt:      "give me a short answer",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  false,
			ExpectedFinalAnswer: "short answer",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}

func TestAgentContract_LongResultRequiresExpansion(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/result.txt": "full long result",
	})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("read-1", "Read", `{"file_path":"/tmp/result.txt"}`),
		testkit.TextTurn("expanded answer"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "long-preview-requires-read",
		Layer:       evals.LayerContract,
		Description: "long result should trigger expansion via Read",
		Prompt:      "read the artifact and continue",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "expanded answer",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}

func TestAgentContract_TailAnswerOnlyReadableAfterRead(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/tail.txt": "header\nheader\nTHE_ANSWER=42",
	})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("read-1", "Read", `{"file_path":"/tmp/tail.txt"}`),
		testkit.TextTurn("42"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "tail-answer-only-readable-after-read",
		Layer:       evals.LayerContract,
		Description: "answer hidden in tail requires Read expansion",
		Prompt:      "find the final answer from the artifact",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "42",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}
