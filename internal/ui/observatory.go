package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/zhanglvtao/cece/internal/protocol"
)

const observatoryPostMinInterval = 250 * time.Millisecond

func (m *Model) observatorySnapshot() protocol.ObservatorySnapshotEvent {
	phase := "idle"
	status := "idle"
	if m.modal.active() {
		phase = "waiting_modal"
		status = "waiting"
	} else if m.busy {
		phase = "busy"
		status = "active"
	}
	modal := m.observatoryModalKind()
	meta := map[string]string{
		"status":  m.status,
		"modal":   modal,
		"queued":  strconv.Itoa(len(m.queued)),
		"events":  strconv.Itoa(m.appliedEventCount),
		"session": m.currentSessionID,
	}
	return protocol.ObservatorySnapshotEvent{
		Scope:       "tui:client",
		Version:     1,
		CapturedAt:  time.Now(),
		ActivePhase: phase,
		Nodes: []protocol.ObservatoryNode{{
			ID:     "tui",
			Label:  "TUI Client",
			Kind:   "tui",
			Status: status,
			Meta:   meta,
		}},
		Edges: []protocol.ObservatoryEdge{{From: "tui", To: "runtime", Label: "input action", Status: status}},
		Metrics: []protocol.ObservatoryMetric{
			{Name: "tui_status", Value: m.status},
			{Name: "queued", Value: strconv.Itoa(len(m.queued))},
		},
	}
}

func (m *Model) maybePostObservatorySnapshot() tea.Cmd {
	if m.observatoryURL == "" {
		return nil
	}
	snapshot := m.observatorySnapshot()
	sig := observatorySnapshotSignature(snapshot)
	if sig == m.lastObservatorySig && time.Since(m.lastObservatoryPost) < observatoryPostMinInterval {
		return nil
	}
	if time.Since(m.lastObservatoryPost) < observatoryPostMinInterval {
		return nil
	}
	m.lastObservatorySig = sig
	m.lastObservatoryPost = time.Now()
	url := strings.TrimRight(m.observatoryURL, "/") + "/api/snapshot"
	return func() tea.Msg {
		body, err := json.Marshal(snapshot)
		if err != nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
		return nil
	}
}

func (m *Model) observatoryModalKind() string {
	switch m.modal.kind {
	case modalConfirmTools:
		return "confirm_tools"
	case modalApprovePlan:
		return "approve_plan"
	case modalQuestion:
		return "question"
	case modalModelPicker:
		return "model_picker"
	case modalSessionPicker:
		return "session_picker"
	case modalMCPPicker:
		return "mcp_picker"
	case modalRenameSession:
		return "rename_session"
	default:
		return ""
	}
}

func observatorySnapshotSignature(snapshot protocol.ObservatorySnapshotEvent) string {
	parts := []string{snapshot.Scope, snapshot.ActivePhase}
	if len(snapshot.Nodes) > 0 {
		n := snapshot.Nodes[0]
		parts = append(parts, n.Status)
		if n.Meta != nil {
			parts = append(parts, n.Meta["status"], n.Meta["modal"], n.Meta["queued"], n.Meta["events"], n.Meta["session"])
		}
	}
	return strings.Join(parts, "|")
}
