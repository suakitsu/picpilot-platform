import { getToken } from '@/lib/api';
import { assertApiAllowed } from '@/lib/runtime';
import { validateNovaRequest, type NovaRequest, type WireResponse } from '../../../../../canvas-lab/bridge/protocol';

export async function forwardNovaRequest(payload: NovaRequest): Promise<WireResponse> {
  const request = validateNovaRequest(payload);
  await assertApiAllowed(request.path);
  const token = getToken();
  if (!token) throw new Error('Sign in to PicPilot before using the canvas');
  const response = await fetch(request.path, {
    method: request.method,
    credentials: 'same-origin',
    cache: 'no-store',
    redirect: 'error',
    headers: {
      Authorization: `Bearer ${token}`,
      ...(request.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...(request.idempotencyKey ? { 'Idempotency-Key': request.idempotencyKey } : {}),
      Prefer: 'wait=0',
    },
    body: request.body === undefined ? undefined : JSON.stringify(request.body),
    signal: AbortSignal.timeout(60_000),
  });
  const body = await response.text();
  if (getToken() !== token) throw new Error('PicPilot session changed; reopen the canvas');
  if (body.length > 32 * 1024 * 1024) throw new Error('Canvas response is too large');
  const headers: Record<string, string> = {};
  for (const name of ['Content-Type', 'Retry-After', 'X-Nova-Task-Id']) {
    const value = response.headers.get(name);
    if (value) headers[name] = value;
  }
  return { status: response.status, body, headers };
}
