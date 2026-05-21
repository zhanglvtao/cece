package chat

import (
	"testing"

	"cece/internal/prompt"
)

func TestAssembleResultToSystemPrompt(t *testing.T) {
	result := prompt.AssembleResult{
		Segments: []prompt.PromptSegment{
			{Content: "stable text", Layer: prompt.ContextStable},
			{Content: "", Layer: prompt.ContextSession},
			{Content: "session text", Layer: prompt.ContextSession},
			{Content: "turn text", Layer: prompt.ContextTurn},
		},
	}

	sp := AssembleResultToSystemPrompt(result)
	if len(sp.Blocks) != 3 {
		t.Fatalf("AssembleResultToSystemPrompt() returned %d blocks, want 3", len(sp.Blocks))
	}

	if sp.Blocks[0].Text != "stable text" {
		t.Errorf("block[0].Text = %q, want %q", sp.Blocks[0].Text, "stable text")
	}
	if sp.Blocks[0].CacheControl == nil || sp.Blocks[0].CacheControl["type"] != "ephemeral" {
		t.Errorf("block[0].CacheControl = %v, want ephemeral", sp.Blocks[0].CacheControl)
	}

	if sp.Blocks[1].Text != "session text" {
		t.Errorf("block[1].Text = %q, want %q", sp.Blocks[1].Text, "session text")
	}
	if sp.Blocks[1].CacheControl == nil || sp.Blocks[1].CacheControl["type"] != "ephemeral" {
		t.Errorf("block[1].CacheControl = %v, want ephemeral", sp.Blocks[1].CacheControl)
	}

	if sp.Blocks[2].Text != "turn text" {
		t.Errorf("block[2].Text = %q, want %q", sp.Blocks[2].Text, "turn text")
	}
	if sp.Blocks[2].CacheControl != nil {
		t.Errorf("block[2].CacheControl = %v, want nil", sp.Blocks[2].CacheControl)
	}
}

func TestAssembleResultToSystemPromptEmpty(t *testing.T) {
	result := prompt.AssembleResult{}
	sp := AssembleResultToSystemPrompt(result)
	if len(sp.Blocks) != 0 {
		t.Errorf("empty AssembleResult should produce 0 blocks, got %d", len(sp.Blocks))
	}
}
