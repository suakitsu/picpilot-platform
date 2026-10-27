import { describe, expect, it } from 'vitest';

import { resolveAgentModel, type AgentModelCatalogEntry } from '@/lib/model-capabilities';

const models: AgentModelCatalogEntry[] = [{
  id: 'picpilot-image',
  name: 'PicPilot 生图',
  maxOutputSize: '4K',
}];

describe('resolveAgentModel', () => {
  it('replaces a stale persisted model with the first configured model', () => {
    expect(resolveAgentModel(
      'gemini-3-pro-image-preview',
      undefined,
      undefined,
      models,
    )).toBe('picpilot-image');
  });

  it('keeps the configured current model when it is available', () => {
    expect(resolveAgentModel(
      'picpilot-image',
      undefined,
      '1K',
      models,
    )).toBe('picpilot-image');
  });
});
