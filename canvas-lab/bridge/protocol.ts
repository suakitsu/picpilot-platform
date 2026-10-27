export const PROTOCOL = 'picpilot-canvas-v2';
export type CanvasApp = 'nova' | 'infinite';
export type Capability = { enabled: boolean; default_model: string; models: { id: string; protocol: string }[] };
export type Runtime = {
  userScope: string;
  edition: 'personal' | 'platform';
  billingEnabled: boolean;
  credits: number | null;
  capabilities: Record<'image' | 'text' | 'video', Capability>;
  video: { enabled: boolean; model_configs: Record<string, unknown>[] };
};
export type NovaRequest = { path: string; method?: 'GET' | 'POST'; body?: unknown; idempotencyKey?: string };
export type WireResponse = { status: number; body: string; headers: Record<string, string> };
export type Actions = {
  'runtime.read': { input: Record<string, never>; output: Runtime };
  'nova.request': { input: NovaRequest; output: WireResponse };
  'video.submit': { input: { prompt: string; images: string[]; modelCode: string; params: Record<string, unknown>; idempotencyKey: string }; output: { id: string } };
  'video.status': { input: { taskId: string }; output: { id: string; status: string; result_url?: string; safe_error?: string } };
  'settings.open': { input: Record<string, never>; output: null };
};

const task = '[A-Za-z0-9_-]{1,128}';
const read = new RegExp(`^/api/nova/(?:config|prompts|blacklist|proxy/models|queue-status|tasks(?:/${task}(?:/response(?:\\?stream=(?:true|false))?)?)?)$`);
const write = new RegExp(`^/api/nova/(?:tasks(?:/${task}/(?:ack|cancel))?|proxy/text|prompt-gallery/verify)$`);
const credentialNames = new Set(['apikey', 'api_key', 'baseurl', 'base_url', 'authorization', 'access_token', 'provider_url']);

export function validateNovaRequest(input: NovaRequest): NovaRequest {
  if (!input || typeof input.path !== 'string' || !['GET', 'POST'].includes(input.method || 'GET')) throw new Error('Invalid canvas request');
  const method = input.method || 'GET';
  if (!(method === 'GET' ? read : write).test(input.path)) throw new Error('Canvas endpoint is not allowed');
  const encoded = JSON.stringify(input.body ?? null);
  if (encoded.length > 32 * 1024 * 1024) throw new Error('Canvas request is too large');
  const scan = (value: unknown, depth = 0) => {
    if (depth > 32) throw new Error('Canvas request is too deep');
    if (!value || typeof value !== 'object') return;
    for (const [name, child] of Object.entries(value)) {
      if (credentialNames.has(name.toLowerCase())) throw new Error('Provider credentials and URLs belong in PicPilot settings');
      scan(child, depth + 1);
    }
  };
  scan(input.body);
  if (method === 'GET' && input.body !== undefined) throw new Error('Read requests cannot contain a body');
  if (method === 'POST' && ['/api/nova/tasks', '/api/nova/proxy/text'].includes(input.path)
    && !/^[\x21-\x7e]{1,128}$/.test(input.idempotencyKey || '')) throw new Error('A stable submission key is required');
  return { path: input.path, method, body: input.body, idempotencyKey: input.idempotencyKey };
}
