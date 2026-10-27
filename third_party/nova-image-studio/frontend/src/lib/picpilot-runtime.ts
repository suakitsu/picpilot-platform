import { CanvasBridge } from '../../../../../canvas-lab/bridge/client';
import { DurableNova } from '../../../../../canvas-lab/bridge/durable';
import { setStorageScope } from '../../../../../canvas-lab/bridge/storage';
import type { Runtime } from '../../../../../canvas-lab/bridge/protocol';
import type { NovaModelRegistry } from './nova-models';

let bridge: CanvasBridge | undefined;
let runtime: Runtime | undefined;
let transport: DurableNova | undefined;
let starting: Promise<Runtime> | undefined;

export function getCanvasBridge() { return bridge ||= new CanvasBridge('nova'); }
export function getCanvasRuntime() { return runtime; }
export function initializePicPilot(): Promise<Runtime> {
  return starting ||= getCanvasBridge().runtime().then(value => {
    runtime = value;
    setStorageScope(value.userScope, 'nova');
    transport = new DurableNova(getCanvasBridge(), value.userScope);
    return value;
  }).catch(error => { starting = undefined; throw error; });
}

export function getEmbeddedRegistry(): NovaModelRegistry {
  const image = runtime?.capabilities.image;
  const text = runtime?.capabilities.text;
  // The OpenAI transport wrapper is resolved by Go against the selected configured provider.
  const imageModels: NovaModelRegistry['imageModels'] = image?.enabled ? image.models.map(selectedImage => ({
    id: selectedImage.id, name: selectedImage.id, modelId: selectedImage.id,
    protocol: 'openai' as const, apiKey: '', baseUrl: '', builtinPreset: 'picpilot' as const,
    maxRefImages: 10, maxOutputSize: '4K' as const, supportsAdvancedParams: false,
  })) : [];
  const textModels: NovaModelRegistry['textModels'] = text?.enabled ? text.models.flatMap(model => {
    if (model.protocol !== 'openai_chat_completions' && model.protocol !== 'openai_responses') return [];
    return [{ id: model.id, name: model.id, modelId: model.id, apiKey: '', baseUrl: '',
      protocol: model.protocol === 'openai_responses' ? 'openai-responses' as const : 'openai-chat-completions' as const }];
  }) : [];
  const imageId = imageModels.find(model => model.id === image?.default_model)?.id || imageModels[0]?.id || '';
  const textId = textModels.find(model => model.id === text?.default_model)?.id || textModels[0]?.id || '';
  return { imageModels, textModels, defaults: {
    textToImage: imageId, imageToImage: imageId, reversePrompt: textId,
    agent: textId, promptOptimize: textId, imageDescribe: textId,
  } };
}

export function novaRequestBody(path: string, input?: Record<string, unknown>) {
  let body = input;
  if (body) {
    // Legacy call sites carry empty credential fields; they never cross the bridge.
    const { apiKey: _key, baseUrl: _url, ...safe } = body;
    body = safe;
    if (path === '/api/nova/tasks') {
      delete body.protocol;
      if (body.gptImageStyle && body.gptImageStyle !== 'auto') throw new Error('Go does not support this image style');
      delete body.gptImageStyle;
      if (body.outputSize === 'auto' || body.outputSize === '512') body.outputSize = '1K';
      if (body.aspectRatio === 'auto') body.aspectRatio = '1:1';
    }
  }
  return body;
}

export async function novaFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  await initializePicPilot();
  const path = typeof input === 'string' ? input : input instanceof URL ? input.pathname + input.search : input.url;
  const body = novaRequestBody(path, typeof init.body === 'string' ? JSON.parse(init.body) : undefined);
  return transport!.fetch({ path, method: init.method === 'POST' ? 'POST' : 'GET', body }, init.signal || undefined);
}
