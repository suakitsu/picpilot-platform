import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { crc32, deflateSync } from 'node:zlib';

// These credentials belong ONLY to a disposable mock installation.
const email = 'smoke-owner@example.invalid';
const password = 'Fixture-only-smoke-password-2026!';
const base = new URL(process.env.PICPILOT_SMOKE_BASE_URL || 'http://127.0.0.1:18082');
assert.ok(['localhost', '127.0.0.1', 'api'].includes(base.hostname), 'Smoke tests require an isolated local endpoint.');
let token = '';
let checks = 0;
async function request(path, { method = 'GET', body, key, authorize = true, status = 200 } = {}) {
  const headers = new Headers();
  if (authorize && token) headers.set('Authorization', `Bearer ${token}`);
  if (key) headers.set('Idempotency-Key', key);
  if (body && !(body instanceof FormData)) {
    headers.set('Content-Type', 'application/json');
    body = JSON.stringify(body);
  }
  const response = await fetch(new URL(path, base), { method, headers, body, redirect: 'error', signal: AbortSignal.timeout(15000) });
  assert.equal(response.status, status, `${method} ${path} returned unexpected status`);
  checks++;
  if (status !== 200) return null;
  const envelope = await response.json();
  assert.equal(envelope.success, true, `${method} ${path} failed`);
  return envelope.data;
}

const runtime = await request('/api/public/config', { authorize: false });
assert.equal(runtime.demo_mode, true, 'Refusing to test an installation that is not explicitly in mock mode.');
assert.ok(['platform', 'personal'].includes(runtime.edition));
const setup = await request('/api/setup/status', { authorize: false });
if (setup.needs_setup) {
  const setupToken = process.env.PICPILOT_SMOKE_SETUP_TOKEN;
  assert.ok(setupToken?.length >= 32, 'Provide the disposable setup token through the environment, never argv.');
  const session = await request('/api/setup', { method: 'POST', authorize: false, body: { email, password, confirm_password: password, setup_token: setupToken, nickname: 'Mock acceptance owner' } });
  token = session.access_token;
} else {
  const session = await request('/api/auth/login', { method: 'POST', authorize: false, body: { email, password } });
  token = session.access_token;
}
assert.ok(token);
const before = await request('/api/me');
assert.equal(before.email_display, email);
assert.equal(before.role, runtime.edition === 'platform' ? 'super_admin' : 'owner');
await request('/api/me', { authorize: false, status: 401 });

const prefix = runtime.edition === 'platform' ? '/api/admin/providers' : '/api/me/providers';
const providerID = `fixture-${randomUUID()}`;
await request(`${prefix}/${providerID}`, { method: 'PUT', body: {
  name: 'Fixture disabled provider', capability: 'image', protocol: 'openai_images_generations', profile: 'openai',
  base_url: 'https://model.example.invalid/v1', api_key: 'fixture-only-not-a-real-provider-key', model: 'gpt-image-2',
  enabled: false, priority: 100, execution_mode: 'http', max_concurrency: 1, timeout_seconds: 60,
} });
const providers = await request(prefix);
assert.ok(!JSON.stringify(providers).includes('fixture-only-not-a-real-provider-key'), 'Provider key leaked.');
assert.equal(providers.providers.find(p => p.id === providerID)?.has_api_key, true);
await request(`${prefix}/${providerID}/check`, { method: 'POST', body: {} });

const form = new FormData();
function pngChunk(type, content) {
  const body = Buffer.concat([Buffer.from(type), content]);
  const size = Buffer.alloc(4);
  size.writeUInt32BE(content.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(body));
  return Buffer.concat([size, body, checksum]);
}
const pngHeader = Buffer.alloc(13);
pngHeader.writeUInt32BE(1, 0);
pngHeader.writeUInt32BE(1, 4);
pngHeader[8] = 8;
pngHeader[9] = 2;
const png = Buffer.concat([
  Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), pngChunk('IHDR', pngHeader),
  pngChunk('IDAT', deflateSync(Buffer.from([0, 42, 128, 192]))), pngChunk('IEND', Buffer.alloc(0)),
]);
form.set('file', new Blob([png], { type: 'image/png' }), 'synthetic-fixture.png');
const asset = await request('/api/assets', { method: 'POST', body: form });
assert.equal(asset.width, 1);
assert.equal(asset.height, 1);
const key = `smoke-image-${randomUUID()}`;
const body = { capability: 'image', title: 'Mock image acceptance', payload: { version: 1, image: { prompt: 'Mock image fixture; never call a model.', size: '1024x1024', reference_asset_ids: [asset.id] } } };
const created = await request('/api/tasks', { method: 'POST', key, body });
const repeated = await request('/api/tasks', { method: 'POST', key, body });
assert.equal(created.task.id, repeated.task.id, 'Idempotent request created a second task.');
assert.equal(repeated.created, false);
async function waitTask(id) {
  const deadline = Date.now() + 45000;
  do {
    const task = await request(`/api/tasks/${id}`);
    if (task.status === 'succeeded') return task;
    assert.ok(!['failed', 'cancelled', 'uncertain'].includes(task.status), `Mock task entered ${task.status}`);
    await new Promise(resolve => setTimeout(resolve, 400));
  } while (Date.now() < deadline);
  assert.fail('Mock task did not complete before deadline.');
}
const image = await waitTask(created.task.id);
assert.equal(image.artifact.width, 1024);
assert.equal(image.artifact.height, 1024);
const signed = new URL(image.result_url);
const output = await fetch(new URL(signed.pathname + signed.search, base));
assert.equal(output.status, 200);
assert.equal(output.headers.get('content-type'), 'image/png');
assert.ok((await output.arrayBuffer()).byteLength > 100);
assert.equal((await fetch(new URL(signed.pathname, base))).status, 404, 'Unsigned asset was public.');

const text = await request('/api/tasks', { method: 'POST', key: `smoke-text-${randomUUID()}`, body: {
  capability: 'text', payload: { version: 1, text: { messages: [{ role: 'user', content: 'Mock text acceptance.' }] } },
} });
const textResult = await waitTask(text.task.id);
assert.equal(textResult.result.mock, true);
await request('/api/tasks?page=1&pageSize=2');
assert.equal((await request('/api/me')).credits, before.credits, 'Mock task altered credits.');
await request(`${prefix}/${providerID}`, { method: 'DELETE' });
await request('/api/auth/logout', { method: 'POST', body: {} });
await request('/api/me', { status: 401 });
token = '';
const relogin = await request('/api/auth/login', { method: 'POST', body: { email, password }, authorize: false });
assert.ok(relogin.access_token);
console.log(`PASS: ${runtime.edition}, ${checks} HTTP checks; setup/auth/providers/upload/image/text/idempotency/signed files/logout. No real model or payment was called.`);
