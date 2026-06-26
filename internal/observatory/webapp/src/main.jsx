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
          <div className="label">Selected Agent</div>
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
            </>
          ) : (
            <div className="empty">no agent selected</div>
          )}
        </section>
      </main>
    </div>
  );
}

function MailboxPanel({ title, items }) {
  return (
    <section className="mailbox-panel">
      <div className="mailbox-title">{title}</div>
      {items.length === 0 ? (
        <div className="empty">empty</div>
      ) : (
        <div className="mailbox-list">
          {items.slice().reverse().map((item, index) => (
            <div className="mailbox-item" key={`${item.message_id}-${index}`}>
              <div className="mailbox-head">
                <span className="time">{formatTime(item.time)}</span>
                <span className="mailbox-kind">{item.kind}</span>
                {item.status_to ? <span className={`status-pill small ${statusClass(item.status_to)}`}>{item.status_to}</span> : null}
              </div>
              <div className="mailbox-detail">{summarizePayload(item.payload)}</div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function summarizePayload(payload) {
  if (!payload) return '';
  if (payload.summary) return String(payload.summary);
  if (payload.activity) return String(payload.activity);
  const entries = Object.entries(payload).slice(0, 3);
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
