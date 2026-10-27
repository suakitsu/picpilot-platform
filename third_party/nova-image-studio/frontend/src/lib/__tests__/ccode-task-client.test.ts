import { afterEach, describe, expect, it, vi } from 'vitest';
import { createNovaTask, getNovaTask } from '@/lib/ccode-task-client';
vi.mock('@/lib/picpilot-runtime', () => ({ novaFetch: (...args: Parameters<typeof fetch>) => fetch(...args), getEmbeddedRegistry: () => ({ imageModels: [], textModels: [], defaults: {} }) }));

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('PicPilot task request retry safety', () => {
  it('retries a transient task status read once', async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: 'busy' }, 503))
      .mockResolvedValueOnce(jsonResponse({ id: 'task-1', status: 'processing' }));
    vi.stubGlobal('fetch', fetchMock);

    const request = getNovaTask('task-1');
    await vi.advanceTimersByTimeAsync(300);

    await expect(request).resolves.toMatchObject({ id: 'task-1', status: 'processing' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('retries a transient network failure for a task status read once', async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(jsonResponse({ id: 'task-2', status: 'queued' }));
    vi.stubGlobal('fetch', fetchMock);

    const request = getNovaTask('task-2');
    await vi.advanceTimersByTimeAsync(300);

    await expect(request).resolves.toMatchObject({ id: 'task-2', status: 'queued' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('never retries an uncertain generation submission', async () => {
    const fetchMock = vi.fn().mockRejectedValueOnce(new TypeError('Failed to fetch'));
    vi.stubGlobal('fetch', fetchMock);

    await expect(createNovaTask({
      apiKey: 'test-key',
      baseUrl: 'https://example.invalid',
      protocol: 'openai',
      mode: 'text-to-image',
      prompt: '商品主图',
      outputSize: '1K',
      aspectRatio: '1:1',
      temperature: 1,
      model: 'gpt-image-2',
      parallelCount: 1,
      images: [],
    })).rejects.toThrow('任务提交结果尚未确认');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
