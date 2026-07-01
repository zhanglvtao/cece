import React, { StrictMode, useEffect, useMemo, useRef, useState } from 'react';
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

      <main className="simple-grid">
        <section className="panel left-panel">
          <div className="label">Agents</div>
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
                  <div className="agent-item-title">{agentDisplayName(agent)}</div>
                  {agent.description ? (
                    <div className="agent-item-desc">{agent.description}</div>
                  ) : null}
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
                  <div className="status-card-title">{agentDisplayName(selectedAgent)}</div>
                  <div className="status-card-subtitle">{selectedAgent.description || 'agent'}</div>
                  <div className={`status-pill ${statusClass(selectedAgent.status)}`}>{selectedAgent.status || 'idle'}</div>
                </div>

                <TranscriptPanel transcript={selectedAgent.transcript || []} />

                <div className="mailbox-grid">
                  <MailboxPanel title="Inbox" hint="received" items={selectedAgent.inbox || []} />
                  <MailboxPanel title="Outbox" hint="sent" items={selectedAgent.outbox || []} />
                  <MailboxPanel title="Lifecycle" hint="scheduler" items={selectedAgent.lifecycle || []} />
                </div>

                <EvidencePanel evidence={state?.evidence || []} />
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

function TranscriptPanel({ transcript }) {
  const scrollRef = useRef(null);
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [transcript.length]);

  return (
    <section className="obs-section transcript-section">
      <div className="obs-title">Transcript ({transcript.length})</div>
      {transcript.length === 0 ? (
        <div className="empty">no messages yet</div>
      ) : (
        <div className="transcript-list" ref={scrollRef}>
          {transcript.map((item, index) => (
            <div className={`transcript-item role-${item.role}`} key={index}>
              <div className="transcript-head">
                <span className={`transcript-role role-${item.role}`}>{item.role}</span>
                {item.tool ? <span className="transcript-tool">{item.tool}</span> : null}
                {item.status ? (
                  <span className={`transcript-status ${item.status}`}>{item.status}</span>
                ) : null}
                <span className="time">{formatTime(item.time)}</span>
              </div>
              {item.text ? <div className="transcript-text">{item.text}</div> : null}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function MailboxPanel({ title, hint, items }) {
  return (
    <section className="mailbox-panel">
      <div className="mailbox-title">
        {title} ({items.length})
        {hint ? <span className="mailbox-hint"> {hint}</span> : null}
      </div>
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
              {item.payload?.result_path ? (
                <div className="mailbox-result-path">{String(item.payload.result_path)}</div>
              ) : null}
              {item.payload?.error ? (
                <div className="mailbox-error">{String(item.payload.error)}</div>
              ) : null}
              {item.message_id ? <div className="mailbox-id">{item.message_id}</div> : null}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function EvidencePanel({ evidence }) {
  const [open, setOpen] = useState(false);
  if (!evidence || evidence.length === 0) return null;
  const recent = evidence.slice(-40).reverse();
  return (
    <section className="obs-section evidence-section">
      <button className="obs-title collapsible" type="button" onClick={() => setOpen((v) => !v)}>
        <span className="caret">{open ? '▾' : '▸'}</span>
        System event log ({evidence.length})
      </button>
      {open ? (
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
      ) : null}
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

function agentDisplayName(agent) {
  if (!agent) return '';
  if (agent.id === 'interactive-root') return 'Current Agent';
  return agent.id;
}

function statusClass(status) {
  if (status === 'running' || status === 'active' || status === 'starting') return 'active';
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
