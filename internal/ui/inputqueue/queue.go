// Package inputqueue provides a simple FIFO queue for user inputs while the
// assistant is busy processing a previous request.
package inputqueue

import "sync"

// Queue is a thread-safe FIFO queue for user input strings.
type Queue struct {
	mu    sync.Mutex
	items []string
}

// Append adds an input to the end of the queue.
func (q *Queue) Append(input string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, input)
}

// Drain removes and returns all queued inputs, clearing the queue.
func (q *Queue) Drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}

// Len returns the number of queued inputs.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Clear removes all queued inputs.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
}
