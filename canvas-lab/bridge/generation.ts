import { CanvasBridge } from './client';
import { DurableNova, abortableDelay } from './durable';
import type { Runtime } from './protocol';

export type TextMessage = { role: string; content: unknown };
export type ImageOptions = { model: string; prompt: string; images?: string[]; count?: number; size?: string; quality?: string; background?: string };
export function safeAssetUrl(value: string): string {
  if (/^data:image\/(png|jpeg|webp);base64,/i.test(value)) return value;
  const url = new URL(value, location.origin);
  if (url.origin !== location.origin || !['http:', 'https:', 'blob:'].includes(url.protocol)) throw new Error('Only owned same-origin canvas assets are supported');
  return url.href;
}
export async function imageReference(value: string, signal?: AbortSignal) {
  const url = safeAssetUrl(value);
  const response = await fetch(url, { signal, credentials: 'same-origin', redirect: 'error' });
  if (!response.ok) throw new Error(`Asset read failed (${response.status})`);
  const blob = await response.blob();
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(blob.type) || blob.size > 8 * 1024 * 1024) throw new Error('Reference must be PNG, JPEG or WebP, at most 8 MiB');
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = '';
  for (let i = 0; i < bytes.length; i += 32768) binary += String.fromCharCode(...bytes.subarray(i, i + 32768));
  return { mimeType: blob.type, data: btoa(binary) };
}
async function readJson(response: Response) {
  const value = await response.json();
  if (!response.ok) throw new Error(value.message || value.error || `Request failed (${response.status})`);
  return value;
}

export class CanvasModels {
  readonly nova: DurableNova;
  constructor(readonly bridge: CanvasBridge, readonly runtime: Runtime) {
    this.nova = new DurableNova(bridge, runtime.userScope);
  }
  async image(options: ImageOptions, signal?: AbortSignal): Promise<string[]> {
    if (!this.runtime.capabilities.image.enabled) throw new Error('No image provider is enabled in PicPilot');
    if (!this.runtime.capabilities.image.models.some(model => model.id === options.model)) throw new Error('The selected image model is not available in PicPilot');
    const count = options.count ?? 1;
    if (!Number.isInteger(count) || count < 1 || count > 4) throw new Error('Choose 1 to 4 images per task');
    const images = await Promise.all((options.images || []).map(value => imageReference(value, signal)));
    const size = options.size || '1:1';
    const body = {
      mode: images.length ? 'image-to-image' : 'text-to-image', prompt: options.prompt, model: options.model,
      images, parallelCount: count, outputSize: '1K',
      ...(/^\d+x\d+$/.test(size) ? { customSize: size } : { aspectRatio: size === 'auto' ? '1:1' : size }),
      ...(options.quality && options.quality !== 'auto' ? { gptImageQuality: options.quality } : {}),
      ...(options.background ? { gptImageBackground: options.background } : {}),
    };
    const created = await readJson(await this.nova.fetch({ path: '/api/nova/tasks', method: 'POST', body }, signal));
    if (typeof created.taskId !== 'string') throw new Error('Go did not return a task ID');
    for (;;) {
      await abortableDelay(1500, signal);
      let response: Response;
      try { response = await this.nova.fetch({ path: `/api/nova/tasks/${created.taskId}` }, signal); }
      catch (error) { if (signal?.aborted) throw error; await abortableDelay(3000, signal); continue; }
      if ([429, 503, 504].includes(response.status)) continue;
      const task = await readJson(response);
      if (task.status === 'completed') {
        const results: string[] = (task.result?.images || []).map((value: string) => safeAssetUrl(value.startsWith('URL:') ? value.slice(4) : value));
        if (!results.length) throw new Error('Task completed without an image');
        return results;
      }
      if (['failed', 'cancelled', 'expired'].includes(task.status)) throw new Error(task.error || `Task ${task.status}`);
    }
  }
  async text(modelId: string, messages: TextMessage[], signal?: AbortSignal): Promise<string> {
    const capability = this.runtime.capabilities.text;
    const model = capability.models.find(item => item.id === modelId);
    if (!capability.enabled || !model) throw new Error('The selected text model is not available in PicPilot');
    const responses = model.protocol === 'openai_responses';
    if (!responses && model.protocol !== 'openai_chat_completions') throw new Error('Unsupported text protocol');
    const input = responses ? messages.map(message => ({ ...message, content: Array.isArray(message.content) ? message.content.map(part => {
      const p = part as { type?: string; text?: string; image_url?: { url?: string } };
      if (p.type === 'text') return { type: 'input_text', text: p.text };
      if (p.type === 'image_url') return { type: 'input_image', image_url: p.image_url?.url };
      throw new Error('Unsupported text content type');
    }) : message.content })) : messages;
    const response = await this.nova.fetch({ path: '/api/nova/proxy/text', method: 'POST', body: {
      protocol: model.protocol, model: model.id, stream: false,
      requestBody: responses ? { input } : { messages: input },
    } }, signal);
    const result = await readJson(response);
    const text = responses
      ? result.output_text || (result.output || []).flatMap((item: { content?: { text?: string }[] }) => item.content || []).map((item: { text?: string }) => item.text || '').join('')
      : result.choices?.[0]?.message?.content;
    if (typeof text !== 'string' || !text) throw new Error('Model returned no text');
    return text;
  }
}
