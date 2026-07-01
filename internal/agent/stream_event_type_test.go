package agent

import "testing"

func TestStreamEventTypeWireValues(t *testing.T) {
	cases := []struct {
		got  StreamEventType
		want string
	}{
		{EventMessageStart, "message_start"},
		{EventMessageDelta, "message_delta"},
		{EventMessageStop, "message_stop"},
		{EventContentBlockStart, "content_block_start"},
		{EventContentBlockDelta, "content_block_delta"},
		{EventContentBlockStop, "content_block_stop"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("StreamEventType = %q, want %q", string(c.got), c.want)
		}
	}
}
