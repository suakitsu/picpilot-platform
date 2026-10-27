import type { CanvasBridge } from './client';
import type { NovaRequest } from './protocol';

type Entry = { key: string; taskId?: string; created: number };
type Journal = Record<string, Entry>;
function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  if (value && typeof value === 'object') return `{${Object.entries(value).filter(([, v]) => v !== undefined).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${JSON.stringify(k)}:${canonical(v)}`).join(',')}}`;
  return JSON.stringify(value) ?? 'null';
}

export function abortableDelay(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) { reject(new DOMException('Aborted', 'AbortError')); return; }
    const onAbort = () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); };
    const timer = setTimeout(() => { signal?.removeEventListener('abort', onAbort); resolve(); }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

// Only task IDs, digests and idempotency keys persist; never prompts, auth or provider keys.
export class DurableNova {
  constructor(private bridge: CanvasBridge, private scope: string, private storage: Storage = localStorage) {}
  private get storageKey() { return `picpilot:canvas:tasks:${this.scope}:${this.bridge.app}`; }
  private read(): Journal { return JSON.parse(this.storage.getItem(this.storageKey) || '{}'); }
  private write(journal: Journal) { this.storage.setItem(this.storageKey, JSON.stringify(journal)); }
  async fetch(input: NovaRequest, signal?: AbortSignal): Promise<Response> {
    const creates = input.method === 'POST' && ['/api/nova/tasks', '/api/nova/proxy/text'].includes(input.path);
    if (!creates) {
      const response = await this.bridge.nova(input, signal);
      if (input.method !== 'POST' && /^\/api\/nova\/tasks\/[\w-]+$/.test(input.path) && response.ok) {
        const task = await response.clone().json();
        if (['completed', 'failed', 'cancelled', 'expired'].includes(task.status)) this.forgetTask(task.id);
      }
      return response;
    }
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input.path + canonical(input.body)));
    const fingerprint = Array.from(new Uint8Array(digest), n => n.toString(16).padStart(2, '0')).join('');
    const journal = this.read();
    const entry = journal[fingerprint] || { key: input.idempotencyKey || crypto.randomUUID(), created: Date.now() };
    journal[fingerprint] = entry;
    this.write(journal); // Failure to checkpoint must prevent an uncertain billable submission.
    let response: Response;
    if (entry.taskId) response = new Response(JSON.stringify({ taskId: entry.taskId }), { status: 202, headers: { 'Content-Type': 'application/json' } });
    else {
      response = await this.bridge.nova({ ...input, idempotencyKey: entry.key }, signal);
      if (response.status >= 400 && response.status < 500 && response.status !== 429) {
        const current = this.read(); delete current[fingerprint]; this.write(current);
        return response;
      }
      if (!response.ok) return response;
      const taskId = response.headers.get('X-Nova-Task-Id') || (response.status === 202 ? (await response.clone().json()).taskId : undefined);
      if (typeof taskId === 'string') { entry.taskId = taskId; this.write({ ...this.read(), [fingerprint]: entry }); }
    }
    if (input.path !== '/api/nova/proxy/text') return response;
    if (response.status !== 202) {
      if (response.ok && entry.taskId) this.forgetTask(entry.taskId);
      return response;
    }
    if (!entry.taskId) throw new Error('Go did not return a durable text task ID');
    const stream = (input.body as { stream?: boolean })?.stream === true;
    for (;;) {
      await abortableDelay(1500, signal);
      try { response = await this.bridge.nova({ path: `/api/nova/tasks/${entry.taskId}/response?stream=${stream}` }, signal); }
      catch (error) { if (signal?.aborted) throw error; await abortableDelay(3000, signal); continue; }
      if (response.status === 202 || response.status === 503 || response.status === 504 || response.status === 429) continue;
      if (response.ok || response.status < 500 || response.status === 502) this.forgetTask(entry.taskId);
      return response;
    }
  }
  private forgetTask(taskId: string) {
    const journal = this.read();
    for (const [fingerprint, entry] of Object.entries(journal)) if (entry.taskId === taskId) delete journal[fingerprint];
    this.write(journal);
  }
}
