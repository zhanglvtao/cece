package observatory

import (
	"sync"

	"github.com/zhanglvtao/cece/internal/protocol"
)

const subscriberBuffer = 4096

type Hub struct {
	store *Store

	mu          sync.Mutex
	subscribers map[int]chan protocol.Event
	nextID      int
	closed      bool

	closeOnce sync.Once
}

func NewHub() *Hub {
	return &Hub{store: NewStore(), subscribers: make(map[int]chan protocol.Event)}
}

func (h *Hub) Store() *Store { return h.store }

func (h *Hub) State() State { return h.store.State() }

func (h *Hub) Observe(ev protocol.Event) {
	if ev == nil {
		return
	}
	h.store.Apply(ev)
	h.broadcast(ev)
}

func (h *Hub) ObserveSnapshot(snapshot protocol.ObservatorySnapshotEvent) {
	h.Observe(snapshot)
}

func (h *Hub) Subscribe() (<-chan protocol.Event, func()) {
	ch := make(chan protocol.Event, subscriberBuffer)
	h.mu.Lock()
	if h.closed {
		close(ch)
		h.mu.Unlock()
		return ch, func() {}
	}
	h.nextID++
	id := h.nextID
	h.subscribers[id] = ch
	subscriberCount := len(h.subscribers)
	h.mu.Unlock()
	h.store.SetSubscriberCount(subscriberCount)
	state := h.store.State()
	if state.Server.URL != "" {
		ch <- protocol.ObservatoryServerStartedEvent{URL: state.Server.URL, Host: state.Server.Host, Port: state.Server.Port}
	}
	cancel := func() {
		h.mu.Lock()
		if current, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(current)
			subscriberCount := len(h.subscribers)
			h.mu.Unlock()
			h.store.SetSubscriberCount(subscriberCount)
			return
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		for id, ch := range h.subscribers {
			close(ch)
			delete(h.subscribers, id)
		}
		h.mu.Unlock()
		h.store.SetSubscriberCount(0)
	})
}

func (h *Hub) broadcast(ev protocol.Event) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	for id, ch := range h.subscribers {
		select {
		case ch <- ev:
		default:
			close(ch)
			delete(h.subscribers, id)
		}
	}
	subscriberCount := len(h.subscribers)
	h.mu.Unlock()
	h.store.SetSubscriberCount(subscriberCount)
}
