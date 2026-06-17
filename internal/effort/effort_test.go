package effort

import "testing"

func TestSelectAuto_SubAgent(t *testing.T) {
	if got := SelectAuto(true, "anything"); got != Low {
		t.Fatalf("sub-agent: got %q, want %q", got, Low)
	}
	if got := SelectAuto(true, "debug this"); got != Low {
		t.Fatalf("sub-agent with debug: got %q, want %q", got, Low)
	}
	if got := SelectAuto(true, "search query"); got != Low {
		t.Fatalf("sub-agent with search: got %q, want %q", got, Low)
	}
}

func TestSelectAuto_XHighKeywords(t *testing.T) {
	for _, input := range []string{
		"debug this crash",
		"Error: timeout",
		"fix this bug",
		"there is a BUG here",
		"Debug output",
	} {
		if got := SelectAuto(false, input); got != XHigh {
			t.Fatalf("input %q: got %q, want %q", input, got, XHigh)
		}
	}
}

func TestSelectAuto_LowKeywords(t *testing.T) {
	for _, input := range []string{
		"search for the file",
		"lookup docs",
		"SearchQuery",
	} {
		if got := SelectAuto(false, input); got != Low {
			t.Fatalf("input %q: got %q, want %q", input, got, Low)
		}
	}
}

func TestSelectAuto_DefaultHigh(t *testing.T) {
	for _, input := range []string{
		"hello",
		"write a test",
		"refactor this module",
		"",
	} {
		if got := SelectAuto(false, input); got != High {
			t.Fatalf("input %q: got %q, want %q", input, got, High)
		}
	}
}

func TestResolve(t *testing.T) {
	// Explicit values pass through
	if got := Resolve(Low, false, ""); got != Low {
		t.Fatalf("explicit low: got %q", got)
	}
	if got := Resolve(High, false, ""); got != High {
		t.Fatalf("explicit high: got %q", got)
	}
	if got := Resolve(XHigh, false, ""); got != XHigh {
		t.Fatalf("explicit xhigh: got %q", got)
	}
	if got := Resolve(Medium, false, ""); got != Medium {
		t.Fatalf("explicit medium: got %q", got)
	}

	// Auto resolves
	if got := Resolve("", false, "debug this"); got != XHigh {
		t.Fatalf("empty+debug: got %q, want %q", got, XHigh)
	}
	if got := Resolve(Auto, false, "search"); got != Low {
		t.Fatalf("auto+search: got %q, want %q", got, Low)
	}
	if got := Resolve(Auto, false, "hello"); got != High {
		t.Fatalf("auto+hello: got %q, want %q", got, High)
	}

	// Auto with sub-agent
	if got := Resolve(Auto, true, "debug"); got != Low {
		t.Fatalf("auto+subagent: got %q, want %q", got, Low)
	}
}

func TestValid(t *testing.T) {
	for _, v := range []string{"low", "medium", "high", "xhigh", "auto"} {
		if !Valid(v) {
			t.Fatalf("Valid(%q) should be true", v)
		}
	}
	for _, v := range []string{"", "max", "off", "invalid"} {
		if Valid(v) {
			t.Fatalf("Valid(%q) should be false", v)
		}
	}
}
