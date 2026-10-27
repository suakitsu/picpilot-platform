import { PROTOCOL, type Actions, type CanvasApp, type NovaRequest, type Runtime, type WireResponse, validateNovaRequest } from './protocol';

export class CanvasBridge {
  private pending = new Map<string, { resolve: (value: unknown) => void; reject: (reason: Error) => void; timer: ReturnType<typeof setTimeout> }>();
  private readonly origin: string;
  private readonly parent: Window;
  constructor(readonly app: CanvasApp, private readonly host: Window = window) {
    if (host.parent === host || !/^https?:$/.test(host.location.protocol)) throw new Error('Open this canvas inside PicPilot');
    this.origin = host.location.origin;
    this.parent = host.parent;
    host.addEventListener('message', this.receive);
  }
  private receive = (event: MessageEvent) => {
    if (event.origin !== this.origin || event.source !== this.parent) return;
    const message = event.data;
    if (!message || message.protocol !== PROTOCOL || message.type !== 'response' || message.app !== this.app) return;
    const pending = this.pending.get(message.requestId);
    if (!pending) return;
    clearTimeout(pending.timer);
    this.pending.delete(message.requestId);
    if (message.ok === true) pending.resolve(message.data);
    else pending.reject(new Error(typeof message.error === 'string' ? message.error : 'PicPilot request failed'));
  };
  request<K extends keyof Actions>(action: K, payload: Actions[K]['input'], signal?: AbortSignal): Promise<Actions[K]['output']> {
    if (signal?.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'));
    const requestId = crypto.randomUUID();
    return new Promise((resolve, reject) => {
      const finish = (error: Error) => {
        const entry = this.pending.get(requestId);
        if (entry) clearTimeout(entry.timer);
        this.pending.delete(requestId);
        signal?.removeEventListener('abort', abort);
        reject(error);
      };
      const abort = () => finish(new DOMException('Aborted', 'AbortError'));
      const timer = setTimeout(() => finish(new Error('PicPilot did not respond. The task may still be running; retry with the same submission key.')), 90_000);
      this.pending.set(requestId, { timer, reject: finish, resolve: value => {
        signal?.removeEventListener('abort', abort);
        resolve(value as Actions[K]['output']);
      } });
      signal?.addEventListener('abort', abort, { once: true });
      this.parent.postMessage({ protocol: PROTOCOL, type: 'request', app: this.app, requestId, action, payload }, this.origin);
    });
  }
  async nova(input: NovaRequest, signal?: AbortSignal): Promise<Response> {
    const result: WireResponse = await this.request('nova.request', validateNovaRequest(input), signal);
    return new Response(result.body, { status: result.status, headers: result.headers });
  }
  runtime(): Promise<Runtime> { return this.request('runtime.read', {}); }
  dispose() {
    this.host.removeEventListener('message', this.receive);
    for (const entry of [...this.pending.values()]) entry.reject(new Error('Canvas closed; backend tasks continue'));
    this.pending.clear();
  }
}
