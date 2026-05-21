package prompt

import "testing"

func TestContextLayerCacheControl(t *testing.T) {
	tests := []struct {
		layer ContextLayer
		want  map[string]string
	}{
		{ContextStable, map[string]string{"type": "ephemeral"}},
		{ContextSession, map[string]string{"type": "ephemeral"}},
		{ContextTurn, nil},
	}
	for _, tt := range tests {
		got := tt.layer.CacheControl()
		if tt.want == nil && got != nil {
			t.Errorf("ContextLayer(%d).CacheControl() = %v, want nil", tt.layer, got)
		}
		if tt.want != nil && got == nil {
			t.Errorf("ContextLayer(%d).CacheControl() = nil, want %v", tt.layer, tt.want)
		}
		if tt.want != nil && got["type"] != tt.want["type"] {
			t.Errorf("ContextLayer(%d).CacheControl()[\"type\"] = %q, want %q", tt.layer, got["type"], tt.want["type"])
		}
	}
}
