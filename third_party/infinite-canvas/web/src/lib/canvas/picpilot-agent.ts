import type { CanvasAgentOp } from './canvas-agent-ops';

// Model output is data. Only bounded canvas operations can reach the editor.
export function parseAgentReply(text: string): { reply: string; ops: CanvasAgentOp[] } {
  let value: unknown;
  try { value = JSON.parse(text.replace(/^```(?:json)?\s*/, '').replace(/\s*```$/, '')); }
  catch { return { reply: text, ops: [] }; }
  if (!value || typeof value !== 'object') return { reply: text, ops: [] };
  const result = value as { reply?: unknown; ops?: unknown };
  const ops: CanvasAgentOp[] = [];
  const string = (v: unknown, max = 200) => typeof v === 'string' && v.length <= max ? v : '';
  const number = (v: unknown, min = -100000, max = 100000) => typeof v === 'number' && Number.isFinite(v) && v >= min && v <= max ? v : 0;
  if (Array.isArray(result.ops)) for (const raw of result.ops.slice(0, 50)) {
    if (!raw || typeof raw !== 'object') continue;
    const op = raw as Record<string, unknown>;
    const id = string(op.id);
    if (op.type === 'add_node') {
      const nodeType = string(op.nodeType);
      if (!['text', 'image', 'video', 'audio', 'group', 'picpilot:html'].includes(nodeType)) continue;
      const metadata = op.metadata && typeof op.metadata === 'object' ? op.metadata as Record<string, unknown> : {};
      ops.push({ type: 'add_node', id: id || undefined, nodeType, title: string(op.title), x: number(op.x), y: number(op.y),
        metadata: { content: ['text', 'picpilot:html'].includes(nodeType) ? string(metadata.content, 32000) : '', prompt: string(metadata.prompt, 16000) } });
    } else if (op.type === 'update_node' && id) {
      const patch = op.patch && typeof op.patch === 'object' ? op.patch as Record<string, unknown> : {};
      const metadata = op.metadata && typeof op.metadata === 'object' ? op.metadata as Record<string, unknown> : {};
      ops.push({ type: 'update_node', id, patch: { ...(typeof patch.title === 'string' ? { title: string(patch.title) } : {}) }, metadata: { ...(typeof metadata.prompt === 'string' ? { prompt: string(metadata.prompt, 16000) } : {}) } });
    } else if (op.type === 'delete_node' && id) ops.push({ type: 'delete_node', id });
    else if (op.type === 'connect_nodes' && string(op.fromNodeId) && string(op.toNodeId)) ops.push({ type: 'connect_nodes', fromNodeId: string(op.fromNodeId), toNodeId: string(op.toNodeId) });
    else if (op.type === 'select_nodes' && Array.isArray(op.ids)) ops.push({ type: 'select_nodes', ids: op.ids.map(id => string(id)).filter(Boolean).slice(0, 100) });
    else if (op.type === 'run_generation' && string(op.nodeId) && ['image', 'text', 'video'].includes(String(op.mode))) ops.push({ type: 'run_generation', nodeId: string(op.nodeId), mode: op.mode as 'image' | 'text' | 'video', prompt: string(op.prompt, 16000) });
  }
  return { reply: string(result.reply, 64000) || text, ops };
}
