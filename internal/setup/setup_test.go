package setup

import "testing"

func TestProtocolsIncludeTraeCLI(t *testing.T) {
	for _, protocol := range protocols {
		if protocol.id == "traecli" {
			return
		}
	}

	t.Fatalf("expected setup protocols to include traecli; got %#v", protocols)
}
