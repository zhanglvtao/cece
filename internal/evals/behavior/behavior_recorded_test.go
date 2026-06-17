package behavior_test

import (
	"testing"

	"github.com/zhanglvtao/cece/internal/evals"
	"github.com/zhanglvtao/cece/internal/evals/recording"
	"github.com/zhanglvtao/cece/internal/testkit"
)

func TestAgentBehavior_ReplayShortAnswer(t *testing.T) {
	// Build a cassette: one turn with a short answer
	cassette := &recording.Cassette{
		Turns: []recording.CassetteTurn{
			{Events: []recording.CassetteEvent{
				{EventType: "message_start", InputTokens: 8},
				{EventType: "content_block_start", Index: 0, Detail: "text"},
				{Delta: "short answer is enough", Detail: "text_delta"},
				{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 4},
				{Done: true, EventType: "message_stop"},
			}},
		},
	}
	path := t.TempDir() + "/test.cassette.json"
	if err := recording.Save(path, cassette); err != nil {
		t.Fatal(err)
	}

	out := evals.NewRunner(t).RunWithReplay(evals.Case{
		Name:        "replay-short-answer",
		Layer:       evals.LayerBehavior,
		Description: "replayed short answer should not trigger unnecessary Read",
		Prompt:      "answer directly from the available preview",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  false,
			ExpectedFinalAnswer: "short answer is enough",
		},
	}, path, testkit.WithExtraTools(testkit.NewFakeRead(testkit.NewFakeFS(nil))))
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}

func TestAgentBehavior_ReplayExpandOnDemand(t *testing.T) {
	// Build a cassette: tool use turn followed by text answer
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/report.txt": "summary only in preview\n\nFINAL_ANSWER=venus",
	})
	cassette := &recording.Cassette{
		Turns: []recording.CassetteTurn{
			{
				Events: []recording.CassetteEvent{
					{EventType: "message_start", InputTokens: 8},
					{EventType: "content_block_start", Index: 0, Detail: "tool_use", ToolCallID: "read-1", ToolCallName: "Read"},
					{ToolCallID: "read-1", ToolCallInput: `{"file_path":"/tmp/report.txt"}`, Detail: "input_json_delta"},
					{EventType: "content_block_stop", Index: 0, ToolCallID: "read-1"},
					{EventType: "message_delta", StopReason: "tool_use", OutputTokens: 4},
					{Done: true, EventType: "message_stop"},
				},
			},
			{
				Events: []recording.CassetteEvent{
					{EventType: "message_start", InputTokens: 12},
					{EventType: "content_block_start", Index: 0, Detail: "text"},
					{Delta: "venus", Detail: "text_delta"},
					{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 2},
					{Done: true, EventType: "message_stop"},
				},
			},
		},
	}
	path := t.TempDir() + "/test.cassette.json"
	if err := recording.Save(path, cassette); err != nil {
		t.Fatal(err)
	}

	out := evals.NewRunner(t).RunWithReplay(evals.Case{
		Name:        "replay-expand-on-demand",
		Layer:       evals.LayerBehavior,
		Description: "replayed tool use should trigger expansion",
		Prompt:      "inspect the report artifact and give me the final answer",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "venus",
		},
	}, path, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
	if !out.Metrics.ExpandedOnDemand {
		t.Fatalf("expected ExpandedOnDemand=true, metrics=%+v", out.Metrics)
	}
}