import { describe, expect, it } from 'vitest';
import { novaRequestBody } from '@/lib/picpilot-runtime';
import { saveRegistry } from '@/lib/nova-models';

describe('Go canvas image transport', () => {
  it('preserves the selected configured model and does not forward client credentials or style', () => {
    const body = novaRequestBody('/api/nova/tasks', { model: 'owned-model', protocol: 'openai', apiKey: '', baseUrl: '', gptImageStyle: 'auto', prompt: 'product', gptImageQuality: 'high', gptImageBackground: 'transparent', customSize: '2048x1536' });
    expect(body).toEqual({ model: 'owned-model', prompt: 'product', gptImageQuality: 'high', gptImageBackground: 'transparent', customSize: '2048x1536' });
  });
  it.each(['natural', 'vivid'])('rejects unsupported %s style before a request', style => {
    expect(() => novaRequestBody('/api/nova/tasks', { gptImageStyle: style })).toThrow('does not support');
  });
  it('retains full structured text/tool requests', () => {
    const requestBody = { messages: [{ role: 'user', content: 'image prompt' }], tools: [{ type: 'function', function: { name: 'draw', parameters: { type: 'object' } } }] };
    expect(novaRequestBody('/api/nova/proxy/text', { protocol: 'openai-chat-completions', model: 'text-owned', requestBody, stream: true, apiKey: '', baseUrl: '' }))
      .toEqual({ protocol: 'openai-chat-completions', model: 'text-owned', requestBody, stream: true });
  });
  it('rejects local provider registry writes instead of storing credentials', () => {
    expect(() => saveRegistry({ imageModels: [], textModels: [], defaults: {} } as never)).toThrow('PicPilot settings');
  });
});
