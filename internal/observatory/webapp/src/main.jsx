import React, { StrictMode, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './styles.css';

function App() {
  const [state, setState] = useState(null);
  const [connection, setConnection] = useState('waiting');
  const [selectedAgentId, setSelectedAgentId] = useState('');

  useEffect(() => {
    let cancelled = false;
    fetch('/api/state')
      .then((response) => response.json())
      .then((next) => {
        if (!cancelled) setState(next);
      })
      .catch(() => setConnection('offline'));

    const events = new EventSource('/api/events');
    const applyEvent = (event) => {
      try {
        const payload = JSON.parse(event.data);
        if (payload.state) setState(payload.state);
        setConnection('live');
      } catch (_) {
        setConnection('offline');
      }
    };
    events.onopen = () => setConnection('live');
    events.onerror = () => setConnection('reconnecting');
    events.addEventListener('state', applyEvent);
    events.addEventListener('event', applyEvent);
    events.onmessage = applyEvent;

    return () => {
      cancelled = true;
      events.close();
    };
  }, []);

  const agents = state?.agents || [];

  useEffect(() => {
    if (!agents.length) {
      if (selectedAgentId !== '') setSelectedAgentId('');
      return;
    }
    const exists = agents.some((agent) => agent.id === selectedAgentId);
    if (!selectedAgentId || !exists) {
      setSelectedAgentId(agents[0].id);
    }
  }, [agents, selectedAgentId]);

  const selectedAgent = useMemo(
    () => agents.find((agent) => agent.id === selectedAgentId) || agents[0] || null,
    [agents, selectedAgentId],
  );

  return (
    <div className="shell">
      <header className="top">
        <div className="title">Cece Agent Observatory</div>
        <div className="meta">
          <span>{state?.server?.url || window.location.origin}</span>
          <span>·</span>
          <span className={`live ${connection}`}>{connection} ●</span>
          <span>·</span>
          <span>subs {state?.subscribers ?? 0}</span>
          <span>·</span>
          <span>{state?.updated_at ? `last ${formatTime(state.updated_at)}` : 'waiting'}</span>
        </div>
      </header>

      <section className="switcher-row panel">
        <label htmlFor="agent-switcher">Agent</label>
        <select
          id="agent-switcher"
          value={selectedAgent?.id || ''}
          onChange={(event) => setSelectedAgentId(event.target.value)}
        >
          {agents.map((agent) => (
            <option key={agent.id} value={agent.id}>
              {agent.id}
            </option>
          ))}
        </select>
      </section>

      <main className="simple-grid">
        <section className="panel left-panel">
          <div className="label">Orchestrator / Agents</div>
          <div className="orchestrator-card">
            <div className="node-title">Orchestrator</div>
            <div className="node-subtitle">scheduler</div>
          </div>
          <div className="agent-list">
            {agents.length === 0 ? (
              <div className="empty">waiting for agents</div>
            ) : (
              agents.map((agent) => (
                <button
                  key={agent.id}
                  className={`agent-item ${selectedAgent?.id === agent.id ? 'selected' : ''}`}
                  onClick={() => setSelectedAgentId(agent.id)}
                  type="button"
                >
                  <div className="agent-item-title">{agent.id}</div>
                  <div className={`agent-item-status ${statusClass(agent.status)}`}>{agent.status || 'idle'}</div>
                </button>
              ))
            )}
          </div>
        </section>

        <section className="panel right-panel">
          <div className="right-scroll">
            {selectedAgent ? (
              <>
                <div className="status-card">
                  <div className="status-card-title">{selectedAgent.id}</div>
                  <div className="status-card-subtitle">{selectedAgent.description || 'agent'}</div>
                  <div className={`status-pill ${statusClass(selectedAgent.status)}`}>{selectedAgent.status || 'idle'}</div>
                </div>

                <div className="mailbox-grid">
                  <MailboxPanel title="Inbox" items={selectedAgent.inbox || []} />
                  <MailboxPanel title="Outbox" items={selectedAgent.outbox || []} />
                </div>

                <RuntimePhases phases={state?.phases || []} />
                <MetricsPanel metrics={state?.metrics || []} />
                <EvidencePanel evidence={state?.evidence || []} />
                <TopologyPanel nodes={state?.nodes || []} edges={state?.edges || []} />
                <SnapshotsPanel snapshots={state?.snapshots || {}} />
              </>
            ) : (
              <div className="empty">no agent selected</div>
            )}
          </div>
        </section>
      </main>
    </div>
  );
}

function MailboxPanel({ title, items }) {
  return (
    <section className="mailbox-panel">
      <div className="mailbox-title">{title} ({items.length})</div>
      {items.length === 0 ? (
        <div className="empty">empty</div>
      ) : (
        <div className="mailbox-list">
          {items.slice().reverse().map((item, index) => (
            <div className="mailbox-item" key={`${item.message_id}-${index}`}>
              <div className="mailbox-head">
                <span className="time">{formatTime(item.time)}</span>
                <span className="mailbox-id">{item.message_id}</span>
                <span className="mailbox-kind">{item.kind}</span>
                {item.lane ? <span className="mailbox-lane">{item.lane}</span> : null}
                {item.status_to ? <span className={`status-pill small ${statusClass(item.status_to)}`}>{item.status_to}</span> : null}
              </div>
              <div className="mailbox-detail">{summarizePayload(item.payload)}</div>
              {item.payload?.result_path ? (
                <div className="mailbox-result-path">{String(item.payload.result_path)}</div>
              ) : null}
              {item.payload?.error ? (
                <div className="mailbox-error">{String(item.payload.error)}</div>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function RuntimePhases({ phases }) {
  if (!phases || phases.length === 0) return null;
  return (
    <section className="obs-section">
      <div className="obs-title">Runtime Phases</div>
      <div className="phase-list">
        {phases.map((p) => (
          <div className="phase-row" key={p.id}>
            <span className="phase-id">{p.label || p.id}</span>
            <span className={`status-pill small ${statusClass(p.status)}`}>{p.status}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function MetricsPanel({ metrics }) {
  if (!metrics || metrics.length === 0) return null;
  return (
    <section className="obs-section">
      <div className="obs-title">Metrics</div>
      <div className="kv-list">
        {metrics.map((m) => (
          <div className="kv-row" key={m.name}>
            <span className="kv-key">{m.name}</span>
            <span className="kv-value">{m.value}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function EvidencePanel({ evidence }) {
  if (!evidence || evidence.length === 0) return null;
  const recent = evidence.slice(-20).reverse();
  return (
    <section className="obs-section">
      <div className="obs-title">Evidence ({evidence.length})</div>
      <div className="evidence-list">
        {recent.map((item, i) => (
          <div className="evidence-item" key={i}>
            <div className="evidence-head">
              <span className="time">{formatTime(item.time)}</span>
              <span className="evidence-kind">{item.kind}</span>
            </div>
            <div className="evidence-text">{item.text}</div>
            {item.detail ? <div className="evidence-detail">{item.detail}</div> : null}
          </div>
        ))}
      </div>
    </section>
  );
}

function TopologyPanel({ nodes, edges }) {
  if ((!nodes || nodes.length === 0) && (!edges || edges.length === 0)) return null;
  return (
    <section className="obs-section">
      <div className="obs-title">Topology</div>
      {nodes && nodes.length > 0 ? (
        <div className="topo-sub">
          <div className="obs-subtitle">Nodes ({nodes.length})</div>
          <div className="topo-list">
            {nodes.map((n) => (
              <div className="topo-row" key={n.id}>
                <span className="topo-id">{n.id}</span>
                <span className="topo-label">{n.label || n.id}</span>
                <span className={`status-pill small ${statusClass(n.status)}`}>{n.status}</span>
              </div>
            ))}
          </div>
        </div>
      ) : null}
      {edges && edges.length > 0 ? (
        <div className="topo-sub">
          <div className="obs-subtitle">Edges ({edges.length})</div>
          <div className="topo-list">
            {edges.map((e, i) => (
              <div className="topo-row" key={i}>
                <span className="topo-id">{e.from} → {e.to}</span>
                <span className="topo-label">{e.label}</span>
                <span className={`status-pill small ${statusClass(e.status)}`}>{e.status}</span>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  );
}

function SnapshotsPanel({ snapshots }) {
  const keys = Object.keys(snapshots || {});
  if (keys.length === 0) return null;
  return (
    <section className="obs-section">
      <div className="obs-title">Snapshots ({keys.length})</div>
      {keys.map((scope) => {
        const snap = snapshots[scope];
        return (
          <div className="snap-item" key={scope}>
            <div className="snap-head">
              <span className="snap-scope">{scope}</span>
              <span className="snap-version">v{snap.version}</span>
              <span className="time">{formatTime(snap.captured_at)}</span>
            </div>
            {snap.active_phase ? <div className="snap-phase">phase: {snap.active_phase}</div> : null}
          </div>
        );
      })}
    </section>
  );
}

function summarizePayload(payload) {
  if (!payload) return '';
  if (payload.summary) return String(payload.summary);
  if (payload.activity) return String(payload.activity);
  const entries = Object.entries(payload).filter(([k]) => k !== 'result_path' && k !== 'error');
  if (entries.length === 0) return '';
  return entries.map(([key, value]) => `${key}=${String(value)}`).join(' ');
}

function statusClass(status) {
  if (status === 'running' || status === 'active') return 'active';
  if (status === 'waiting_input' || status === 'waiting_confirm' || status === 'waiting_plan' || status === 'waiting') return 'waiting';
  if (status === 'completed' || status === 'done') return 'done';
  if (status === 'failed' || status === 'cancelled') return 'failed';
  return 'idle';
}

function formatTime(value) {
  if (!value) return '';
  return new Date(value).toLocaleTimeString();
}

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
);