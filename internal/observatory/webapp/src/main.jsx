import React, { StrictMode, useEffect, useMemo, useRef, useState } from 'react';
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
  ReactFlowProvider,
  getSmoothStepPath,
  useReactFlow,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import './styles.css';

const MAIN_LAYOUT = new Map([
  ['user', [0, 120]],
  ['tui', [250, 120]],
  ['runtime', [500, 120]],
  ['hub', [750, 120]],
  ['engine', [1000, 120]],
  ['model', [1250, 120]],
  ['orchestrator', [1250, 330]],
]);
const NODE_TYPES = { status: StatusNode };
const EDGE_TYPES = { status: StatusEdge };

function AppFrame() {
  return (
    <ReactFlowProvider>
      <App />
    </ReactFlowProvider>
  );
}

function App() {
  const [state, setState] = useState(null);
  const [connection, setConnection] = useState('waiting');
  const fittedRef = useRef(false);
  const topologyKey = topologySignature(state);
  const metricsKey = metricsSignature(state);
  const { fitView } = useReactFlow();

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

  const topology = useMemo(() => buildTopology(state), [topologyKey]);
  const metrics = useMemo(() => metricMap(state), [metricsKey]);
  const activeNode = firstByStatus(state?.nodes, ['active', 'waiting']);
  const activeEdge = firstByStatus(state?.edges, ['active', 'waiting']);
  const phases = state?.phases || [];
  const evidence = (state?.evidence || []).slice(-40).reverse();

  useEffect(() => {
    if (!fittedRef.current && topology.nodes.length > 0) {
      fittedRef.current = true;
      requestAnimationFrame(() => fitView({ padding: 0.12, duration: 0 }));
    }
  }, [fitView, topology.nodes.length]);

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
            <EvidenceList evidence={evidence} />
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
  const dynamicPositions = dynamicLayout(rawNodes);

  return {
    nodes: rawNodes.map((node) => ({
      id: node.id,
      type: 'status',
      position: nodePosition(node, dynamicPositions),
      data: node,
    })),
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

function nodePosition(node, dynamicPositions) {
  const point = MAIN_LAYOUT.get(node.id) || dynamicPositions.get(node.id) || [0, 0];
  return { x: point[0], y: point[1] };
}

function dynamicLayout(nodes) {
  const positions = new Map();
  nodes
    .filter((node) => node.kind === 'tool')
    .sort(compareNodes)
    .forEach((node, index) => {
      const column = index % 3;
      const row = Math.floor(index / 3);
      positions.set(node.id, [940 + column * 230, 510 + row * 112]);
    });
  nodes
    .filter((node) => node.kind === 'agent')
    .sort(compareNodes)
    .forEach((node, index) => {
      positions.set(node.id, [1250, 330 + index * 112]);
    });
  return positions;
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

function EvidenceList({ evidence }) {
  if (evidence.length === 0) {
    return <div className="evidence-empty">waiting for events</div>;
  }
  return (
    <div className="evidence">
      {evidence.map((item, index) => (
        <div className="evidence-item" key={`${item.time}-${item.kind}-${index}`} title={item.detail || item.text}>
          <div className="evidence-head">
            <span className="time">{formatTime(item.time)}</span>
            <span className="evidence-kind">{item.kind}</span>
          </div>
          <div className="evidence-text">{item.text}</div>
          {item.detail ? <div className="evidence-detail">{item.detail}</div> : null}
        </div>
      ))}
    </div>
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

function topologySignature(state) {
  if (!state) return '';
  return JSON.stringify({
    nodes: (state.nodes || [])
      .map((node) => [node.id, node.label, node.kind, node.status, stableMeta(node.meta)])
      .sort((a, b) => String(a[0]).localeCompare(String(b[0]))),
    edges: (state.edges || [])
      .map((edge) => [edge.from, edge.to, edge.label, edge.status])
      .sort((a, b) => String(a.join('\u0000')).localeCompare(String(b.join('\u0000')))),
  });
}

function stableMeta(meta) {
  if (!meta) return null;
  return Object.keys(meta)
    .sort()
    .map((key) => [key, meta[key]]);
}

function metricsSignature(state) {
  if (!state) return '';
  return JSON.stringify(
    (state.metrics || [])
      .map((metric) => [metric.name, metric.value])
      .sort((a, b) => String(a[0]).localeCompare(String(b[0]))),
  );
}

function compareNodes(a, b) {
  const orderA = MAIN_LAYOUT.has(a.id) ? mainOrder(a.id) : 100 + kindOrder(a.kind);
  const orderB = MAIN_LAYOUT.has(b.id) ? mainOrder(b.id) : 100 + kindOrder(b.kind);
  if (orderA !== orderB) return orderA - orderB;
  return String(a.id).localeCompare(String(b.id));
}

function mainOrder(id) {
  let index = 0;
  for (const key of MAIN_LAYOUT.keys()) {
    if (key === id) return index;
    index += 1;
  }
  return 100;
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
    <AppFrame />
  </StrictMode>,
);
