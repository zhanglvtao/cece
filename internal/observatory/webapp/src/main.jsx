import React, { StrictMode, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Background,
  BaseEdge,
  Controls,
  EdgeLabelRenderer,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  getSmoothStepPath,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import dagre from 'dagre';
import './styles.css';

const NODE_WIDTH = 176;
const NODE_HEIGHT = 76;
const MAIN_ORDER = new Map([
  ['user', 0],
  ['tui', 1],
  ['runtime', 2],
  ['hub', 3],
  ['engine', 4],
  ['model', 5],
  ['orchestrator', 6],
]);
const NODE_TYPES = { status: StatusNode };
const EDGE_TYPES = { status: StatusEdge };

function App() {
  const [state, setState] = useState(null);
  const [connection, setConnection] = useState('waiting');

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

  const topology = useMemo(() => buildTopology(state), [state]);
  const metrics = useMemo(() => metricMap(state), [state]);
  const activeNode = firstByStatus(state?.nodes, ['active', 'waiting']);
  const activeEdge = firstByStatus(state?.edges, ['active', 'waiting']);
  const phases = state?.phases || [];
  const evidence = (state?.evidence || []).slice(-12).reverse();

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

      <main className="grid">
        <section>
          <div className="canvas">
            <ReactFlow
              nodes={topology.nodes}
              edges={topology.edges}
              nodeTypes={NODE_TYPES}
              edgeTypes={EDGE_TYPES}
              fitView
              fitViewOptions={{ padding: 0.12 }}
              minZoom={0.35}
              maxZoom={1.4}
              nodesDraggable={false}
              nodesConnectable={false}
              elementsSelectable={false}
            >
              <Background color="#222" gap={28} size={1} />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>
          <Panel title="Phase rail" className="phase-panel">
            <div className="phases">
              {phases.map((phase) => (
                <span className={`chip ${statusClass(phase.status)}`} key={phase.id}>
                  {phase.label} {phaseMark(phase.status)}
                </span>
              ))}
            </div>
          </Panel>
        </section>

        <aside className="side">
          <Panel title="Inspector">
            <div className="kv">
              <KeyValue label="active node" value={activeNode?.label || ''} />
              <KeyValue label="active edge" value={edgeText(activeEdge)} />
              <KeyValue label="phase" value={activePhase(phases)} />
              <KeyValue label="model" value={metrics.model || ''} />
              <KeyValue label="tokens" value={[metrics.input_tokens, metrics.context_window].filter(Boolean).join(' / ')} />
              <KeyValue label="subscribers" value={state?.subscribers ?? 0} />
            </div>
          </Panel>
          <Panel title="Evidence" className="evidence-panel">
            <div className="evidence">
              {evidence.map((item, index) => (
                <div key={`${item.time}-${index}`}>
                  <span className="time">{formatTime(item.time)}</span> {item.text}
                </div>
              ))}
            </div>
          </Panel>
          <div className="data-path">Data path: protocol.Event / protocol.Action → Observatory Hub → SSE → Web topology</div>
        </aside>
      </main>
    </div>
  );
}

function buildTopology(state) {
  const rawNodes = [...(state?.nodes || [])].sort(compareNodes);
  const nodeIDs = new Set(rawNodes.map((node) => node.id));
  const rawEdges = [...(state?.edges || [])]
    .filter((edge) => nodeIDs.has(edge.from) && nodeIDs.has(edge.to))
    .sort(compareEdges);
  const parallel = parallelEdgeMap(rawEdges);
  const graph = new dagre.graphlib.Graph({ multigraph: true });
  graph.setGraph({ rankdir: 'LR', ranksep: 96, nodesep: 42, marginx: 28, marginy: 28 });
  graph.setDefaultEdgeLabel(() => ({}));

  rawNodes.forEach((node) => graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT }));
  rawEdges.forEach((edge) => graph.setEdge(edge.from, edge.to, {}, edgeID(edge)));
  dagre.layout(graph);

  return {
    nodes: rawNodes.map((node) => {
      const point = graph.node(node.id) || { x: 0, y: 0 };
      return {
        id: node.id,
        type: 'status',
        position: { x: point.x - NODE_WIDTH / 2, y: point.y - NODE_HEIGHT / 2 },
        data: node,
      };
    }),
    edges: rawEdges.map((edge) => {
      const key = `${edge.from}->${edge.to}`;
      const group = parallel.get(key) || [];
      const index = group.findIndex((item) => edgeID(item) === edgeID(edge));
      const color = statusColor(edge.status);
      return {
        id: edgeID(edge),
        type: 'status',
        source: edge.from,
        target: edge.to,
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 14, height: 14 },
        data: {
          label: edge.label || '',
          status: edge.status || 'idle',
          parallelIndex: index < 0 ? 0 : index,
          parallelCount: group.length || 1,
        },
      };
    }),
  };
}

function StatusNode({ data }) {
  const meta = firstMetaValue(data.meta) || data.status || '';
  return (
    <div className={`status-node ${statusClass(data.status)} kind-${data.kind || 'unknown'}`}>
      <Handle className="node-handle" type="target" position={Position.Left} />
      <div className="node-title">{data.label || data.id}</div>
      <div className="node-subtitle">{data.kind || 'node'} · {data.status || 'idle'}</div>
      <div className="node-meta">{meta}</div>
      <Handle className="node-handle" type="source" position={Position.Right} />
    </div>
  );
}

function StatusEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, markerEnd }) {
  const status = data?.status || 'idle';
  const count = data?.parallelCount || 1;
  const index = data?.parallelIndex || 0;
  const offset = (index - (count - 1) / 2) * 18;
  const color = statusColor(status);
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY: sourceY + offset,
    sourcePosition,
    targetX,
    targetY: targetY + offset,
    targetPosition,
    borderRadius: 8,
  });

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: color,
          strokeWidth: status === 'active' ? 2 : 1.3,
          strokeDasharray: status === 'waiting' ? '5 4' : undefined,
        }}
      />
      {data?.label ? (
        <EdgeLabelRenderer>
          <div
            className={`edge-label ${statusClass(status)}`}
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
          >
            {data.label}
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}

function Panel({ title, className = '', children }) {
  return (
    <section className={`panel ${className}`}>
      <div className="label">{title}</div>
      {children}
    </section>
  );
}

function KeyValue({ label, value }) {
  return (
    <>
      <div className="k">{label}</div>
      <div>{value}</div>
    </>
  );
}

function compareNodes(a, b) {
  const orderA = MAIN_ORDER.has(a.id) ? MAIN_ORDER.get(a.id) : 100 + kindOrder(a.kind);
  const orderB = MAIN_ORDER.has(b.id) ? MAIN_ORDER.get(b.id) : 100 + kindOrder(b.kind);
  if (orderA !== orderB) return orderA - orderB;
  return String(a.id).localeCompare(String(b.id));
}

function compareEdges(a, b) {
  const source = compareNodes({ id: a.from }, { id: b.from });
  if (source !== 0) return source;
  const target = compareNodes({ id: a.to }, { id: b.to });
  if (target !== 0) return target;
  return String(a.label || '').localeCompare(String(b.label || ''));
}

function kindOrder(kind) {
  if (kind === 'tool') return 0;
  if (kind === 'agent') return 1;
  return 2;
}

function parallelEdgeMap(edges) {
  const grouped = new Map();
  edges.forEach((edge) => {
    const key = `${edge.from}->${edge.to}`;
    const list = grouped.get(key) || [];
    list.push(edge);
    grouped.set(key, list);
  });
  return grouped;
}

function edgeID(edge) {
  return `${edge.from}->${edge.to}:${edge.label || ''}`;
}

function firstByStatus(items = [], statuses = []) {
  for (const status of statuses) {
    const found = items.find((item) => item.status === status);
    if (found) return found;
  }
  return null;
}

function metricMap(state) {
  const result = {};
  (state?.metrics || []).forEach((metric) => {
    result[metric.name] = metric.value;
  });
  return result;
}

function firstMetaValue(meta) {
  if (!meta) return '';
  const key = Object.keys(meta).sort()[0];
  return key ? meta[key] : '';
}

function edgeText(edge) {
  return edge?.from ? `${edge.from} → ${edge.to}` : '';
}

function activePhase(phases) {
  const phase = phases.find((item) => item.status === 'active') || phases.find((item) => item.status === 'waiting');
  return phase?.label || '';
}

function phaseMark(status) {
  if (status === 'active') return '●';
  if (status === 'waiting') return '◐';
  if (status === 'done') return '✓';
  if (status === 'failed') return '✗';
  return '○';
}

function statusClass(status) {
  if (status === 'active' || status === 'waiting' || status === 'done' || status === 'failed') return status;
  return 'idle';
}

function statusColor(status) {
  if (status === 'active') return '#7ee787';
  if (status === 'waiting') return '#d29922';
  if (status === 'done') return '#6e7681';
  if (status === 'failed') return '#f85149';
  return '#444';
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
