import { CanvasBridge } from '../../../../../canvas-lab/bridge/client';
import { CanvasModels } from '../../../../../canvas-lab/bridge/generation';
import { setStorageScope } from '../../../../../canvas-lab/bridge/storage';
import type { Runtime } from '../../../../../canvas-lab/bridge/protocol';

export { canvasStorage, storageName } from '../../../../../canvas-lab/bridge/storage';
export { abortableDelay } from '../../../../../canvas-lab/bridge/durable';
export { safeAssetUrl } from '../../../../../canvas-lab/bridge/generation';
let bridge: CanvasBridge | undefined;
let models: CanvasModels | undefined;
let runtime: Runtime | undefined;
export function getBridge() { return bridge ||= new CanvasBridge('infinite'); }
export function getRuntime() {
  if (!runtime) throw new Error('PicPilot identity is not ready');
  return runtime;
}
export function getModels() {
  if (!models) throw new Error('PicPilot providers are not ready');
  return models;
}
export async function initializePicPilot() {
  runtime = await getBridge().runtime();
  setStorageScope(runtime.userScope, 'infinite');
  models = new CanvasModels(getBridge(), runtime);
}
