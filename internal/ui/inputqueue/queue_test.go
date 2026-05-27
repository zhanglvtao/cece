package inputqueue

import "testing"

func TestQueueAppendAndDrain(t *testing.T) {
	var q Queue
	if q.Len() != 0 {
		t.Fatalf("expected len 0, got %d", q.Len())
	}

	q.Append("hello")
	q.Append("world")
	if q.Len() != 2 {
		t.Fatalf("expected len 2, got %d", q.Len())
	}

	items := q.Drain()
	if len(items) != 2 || items[0] != "hello" || items[1] != "world" {
		t.Fatalf("expected [hello world], got %v", items)
	}

	if q.Len() != 0 {
		t.Fatalf("expected len 0 after drain, got %d", q.Len())
	}

	// Drain on empty queue returns nil
	items = q.Drain()
	if items != nil {
		t.Fatalf("expected nil, got %v", items)
	}
}

func TestQueueClear(t *testing.T) {
	var q Queue
	q.Append("a")
	q.Append("b")
	q.Clear()
	if q.Len() != 0 {
		t.Fatalf("expected len 0 after clear, got %d", q.Len())
	}
}

func TestQueueDrainClearsInternal(t *testing.T) {
	var q Queue
	q.Append("x")
	items := q.Drain()
	if len(items) != 1 || items[0] != "x" {
		t.Fatalf("expected [x], got %v", items)
	}
	// Mutating returned slice should not affect queue
	items[0] = "y"
	// Append after drain should work cleanly
	q.Append("z")
	if q.Len() != 1 {
		t.Fatalf("expected len 1, got %d", q.Len())
	}
}
