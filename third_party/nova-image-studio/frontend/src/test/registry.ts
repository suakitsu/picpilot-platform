import { vi } from 'vitest';
import * as runtime from '@/lib/picpilot-runtime';
import type { NovaModelRegistry } from '@/lib/nova-models';
export function setTestRegistry(registry: NovaModelRegistry) {
  vi.spyOn(runtime, 'getEmbeddedRegistry').mockReturnValue(registry);
}
