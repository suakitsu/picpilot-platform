const VIEWPORT_HEIGHT_PROPERTY = '--picpilot-visual-viewport-height';
const VIEWPORT_WIDTH_PROPERTY = '--picpilot-visual-viewport-width';
const VIEWPORT_TOP_PROPERTY = '--picpilot-visual-viewport-top';
const VIEWPORT_LEFT_PROPERTY = '--picpilot-visual-viewport-left';

type ViewportEventSource = Pick<EventTarget, 'addEventListener' | 'removeEventListener'> & {
  height: number;
  width: number;
  offsetTop?: number;
  offsetLeft?: number;
};

export interface EmbeddedViewportEnvironment {
  root: HTMLElement;
  windowTarget: Pick<Window, 'innerHeight' | 'innerWidth' | 'addEventListener' | 'removeEventListener'>;
  visualViewport?: ViewportEventSource | null;
}

export function updateEmbeddedViewportHeight(environment: EmbeddedViewportEnvironment): number {
  const viewport = environment.visualViewport;
  const measuredHeight = viewport?.height ?? environment.windowTarget.innerHeight;
  const measuredWidth = viewport?.width ?? environment.windowTarget.innerWidth;
  const height = Math.max(1, Math.round(measuredHeight));
  const width = Math.max(1, Math.round(measuredWidth));
  const top = Math.max(0, Math.round(viewport?.offsetTop ?? 0));
  const left = Math.max(0, Math.round(viewport?.offsetLeft ?? 0));
  environment.root.style.setProperty(VIEWPORT_HEIGHT_PROPERTY, `${height}px`);
  environment.root.style.setProperty(VIEWPORT_WIDTH_PROPERTY, `${width}px`);
  environment.root.style.setProperty(VIEWPORT_TOP_PROPERTY, `${top}px`);
  environment.root.style.setProperty(VIEWPORT_LEFT_PROPERTY, `${left}px`);
  return height;
}

export function installEmbeddedViewportTracking(
  enabled: boolean,
  environment: EmbeddedViewportEnvironment,
): () => void {
  const { root, windowTarget, visualViewport } = environment;

  if (!enabled) {
    delete root.dataset.picpilotEmbedded;
    root.style.removeProperty(VIEWPORT_HEIGHT_PROPERTY);
    root.style.removeProperty(VIEWPORT_WIDTH_PROPERTY);
    root.style.removeProperty(VIEWPORT_TOP_PROPERTY);
    root.style.removeProperty(VIEWPORT_LEFT_PROPERTY);
    return () => undefined;
  }

  root.dataset.picpilotEmbedded = 'true';
  const update = () => updateEmbeddedViewportHeight(environment);
  update();
  windowTarget.addEventListener('resize', update);
  visualViewport?.addEventListener('resize', update);
  visualViewport?.addEventListener('scroll', update);

  return () => {
    windowTarget.removeEventListener('resize', update);
    visualViewport?.removeEventListener('resize', update);
    visualViewport?.removeEventListener('scroll', update);
    delete root.dataset.picpilotEmbedded;
    root.style.removeProperty(VIEWPORT_HEIGHT_PROPERTY);
    root.style.removeProperty(VIEWPORT_WIDTH_PROPERTY);
    root.style.removeProperty(VIEWPORT_TOP_PROPERTY);
    root.style.removeProperty(VIEWPORT_LEFT_PROPERTY);
  };
}
