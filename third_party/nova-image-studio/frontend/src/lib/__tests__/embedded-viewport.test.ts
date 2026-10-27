import { describe, expect, it, vi } from 'vitest';

import {
  installEmbeddedViewportTracking,
  updateEmbeddedViewportHeight,
  type EmbeddedViewportEnvironment,
} from '@/lib/embedded-viewport';

function createEnvironment(height = 520) {
  const root = document.createElement('html');
  const windowListeners = new Map<string, EventListener>();
  const viewportListeners = new Map<string, EventListener>();
  const visualViewport = {
    height,
    width: 360,
    offsetTop: 12,
    offsetLeft: 4,
    addEventListener: vi.fn((name: string, listener: EventListener) => viewportListeners.set(name, listener)),
    removeEventListener: vi.fn((name: string) => viewportListeners.delete(name)),
  };
  const windowTarget = {
    innerHeight: 800,
    innerWidth: 412,
    addEventListener: vi.fn((name: string, listener: EventListener) => windowListeners.set(name, listener)),
    removeEventListener: vi.fn((name: string) => windowListeners.delete(name)),
  };
  const environment: EmbeddedViewportEnvironment = { root, windowTarget, visualViewport };
  return { environment, root, windowTarget, visualViewport, viewportListeners, windowListeners };
}

describe('embedded viewport tracking', () => {
  it('uses the visual viewport height when the soft keyboard changes the usable area', () => {
    const { environment, root, visualViewport, viewportListeners } = createEnvironment();
    const dispose = installEmbeddedViewportTracking(true, environment);

    expect(root.dataset.picpilotEmbedded).toBe('true');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-height')).toBe('520px');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-width')).toBe('360px');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-top')).toBe('12px');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-left')).toBe('4px');

    visualViewport.height = 284;
    viewportListeners.get('resize')?.(new Event('resize'));
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-height')).toBe('284px');

    dispose();
    expect(root.dataset.picpilotEmbedded).toBeUndefined();
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-height')).toBe('');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-width')).toBe('');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-top')).toBe('');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-left')).toBe('');
  });

  it('falls back to the window height and rejects a zero-height measurement', () => {
    const { environment, root, windowTarget } = createEnvironment();
    environment.visualViewport = null;
    windowTarget.innerHeight = 0;

    expect(updateEmbeddedViewportHeight(environment)).toBe(1);
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-height')).toBe('1px');
    expect(root.style.getPropertyValue('--picpilot-visual-viewport-width')).toBe('412px');
  });
});
