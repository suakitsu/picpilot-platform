(() => {
  const script = document.currentScript;
  const app = script?.dataset.app === 'canvas' ? 'canvas' : 'studio';
  script?.remove();
  const title = app === 'canvas' ? 'PicPilot 画布' : 'PicPilot AI 创作工作台';
  const hiddenAttribute = 'data-picpilot-hidden';
  const exactText = app === 'canvas'
    ? new Map([
        ['无限画布', 'PicPilot 画布'],
        ['Infinite Canvas', 'PicPilot Canvas']
      ])
    : new Map([
        ['Nova Image', 'PicPilot'],
        ['Nova Image Studio', 'PicPilot'],
        ['批量 API 图像生成器', 'AI 图像创作工作台']
      ]);

  function replacementFor(value) {
    const exact = exactText.get(value);
    if (exact) return exact;
    if (app !== 'canvas') return '';
    if (/^无限画布\s+\d+$/.test(value)) return value.replace(/^无限画布/, 'PicPilot 画布');
    if (/^Infinite Canvas\s+\d+$/.test(value)) return value.replace(/^Infinite Canvas/, 'PicPilot Canvas');
    return '';
  }

  function replaceTextNode(node) {
    const value = node.nodeValue || '';
    const trimmed = value.trim();
    const replacement = replacementFor(trimmed);
    if (!replacement) return;
    node.nodeValue = value.replace(trimmed, replacement);
  }

  function brandElement(element) {
    if (!(element instanceof Element)) return;

    for (const attribute of ['aria-label', 'alt', 'title']) {
      const value = element.getAttribute(attribute);
      if (!value) continue;
      let next = value;
      exactText.forEach((replacement, source) => {
        next = next.replaceAll(source, replacement);
      });
      const wholeValueReplacement = replacementFor(value.trim());
      if (wholeValueReplacement) next = value.replace(value.trim(), wholeValueReplacement);
      if (next !== value) element.setAttribute(attribute, next);
    }

    if (element instanceof HTMLImageElement && /\/favicon\.png(?:\?|$)/.test(element.src)) {
      element.src = '/picpilot-brand.png';
    }

    const style = element.getAttribute('style');
    if (style?.includes('/logo.svg')) {
      element.setAttribute('style', style.replaceAll('/logo.svg', '/picpilot-brand.png'));
    }
  }

  function normalizedText(element) {
    return (element?.textContent || '').replace(/\s+/g, ' ').trim();
  }

  function controlLabel(element) {
    return normalizedText(element)
      || (element?.getAttribute?.('aria-label') || '').trim()
      || (element?.getAttribute?.('title') || '').trim();
  }

  function hide(element) {
    if (!(element instanceof HTMLElement)) return;
    element.setAttribute(hiddenAttribute, 'true');
    element.setAttribute('aria-hidden', 'true');
  }

  function show(element) {
    if (!(element instanceof HTMLElement)) return;
    element.removeAttribute(hiddenAttribute);
    element.removeAttribute('aria-hidden');
  }

  function hideInfiniteStandaloneControls() {
    const videoEnabled = document.documentElement.dataset.picpilotVideoEnabled === 'true';
    document.querySelectorAll('a').forEach(link => {
      const href = link.getAttribute('href') || '';
      if (href === '/config' || href.includes('docs.canvas.best') || href.includes('github.com/basketikun')) hide(link);
      if (href === '/video') videoEnabled ? show(link) : hide(link);
    });

    document.querySelectorAll('[role="complementary"], aside').forEach(panel => {
      const text = normalizedText(panel);
      if (text.includes('连接本地 Agent') || text.includes('Connect token') || text.includes('Codex 插件')) hide(panel);
    });

    document.querySelectorAll('button, [role="menuitem"], [role="tab"]').forEach(control => {
      const text = controlLabel(control);
      if (text === '视频') {
        videoEnabled ? show(control) : hide(control);
        return;
      }
      if (text === '配置'
        || text === 'Agent'
        || text === '打开 Agent'
        || text === '节点插件'
        || text.startsWith('Codex ')
        || /^v\d/.test(text)
        || text === '音频'
        || text === 'AI 音频'
        || text === '音频生成') hide(control);
    });
  }

  function collapseInfiniteMobilePanelOnce() {
    if (app !== 'canvas'
      || window.innerWidth > 760
      || document.documentElement.dataset.picpilotMobilePanelCollapsed === 'true') return;

    const collapseButton = Array.from(document.querySelectorAll('button')).find(button => (
      controlLabel(button) === '收起面板'
      && button.getClientRects().length > 0
    ));
    if (!(collapseButton instanceof HTMLButtonElement)) return;

    document.documentElement.dataset.picpilotMobilePanelCollapsed = 'true';
    window.setTimeout(() => collapseButton.click(), 0);
  }

  function simplifyNovaWorkspace() {
    document.querySelectorAll('button').forEach(button => {
      const text = controlLabel(button);
      if (text === '设置') hide(button);
    });

    let workbench = null;
    document.querySelectorAll('[role="tab"]').forEach(tab => {
      const text = controlLabel(tab);
      if (text === '生图工作台') workbench = tab;
    });
    if (workbench instanceof HTMLElement
      && workbench.getAttribute('aria-selected') !== 'true'
      && document.documentElement.dataset.picpilotWorkbenchSelected !== 'true') {
      document.documentElement.dataset.picpilotWorkbenchSelected = 'true';
      workbench.click();
    }

  }

  function revealEmbeddedUi() {
    if (app === 'studio'
      && document.documentElement.dataset.picpilotNovaBootstrapping !== 'false') return;
    if (document.documentElement.dataset.picpilotReady !== 'true') {
      document.documentElement.dataset.picpilotReady = 'true';
      if (window.parent !== window) {
        window.parent.postMessage({
          protocol: 'picpilot-canvas-v1',
          type: 'picpilot:canvas:ui-ready',
          app
        }, '*');
      }
    }
  }

  function simplifyEmbeddedUi() {
    if (!document.getElementById('picpilot-embedded-style')) {
      const style = document.createElement('style');
      style.id = 'picpilot-embedded-style';
      style.textContent = `
        [${hiddenAttribute}="true"] { display: none !important; }
        html[data-picpilot-branded="canvas"] body { overflow-x: hidden; }
        html[data-picpilot-branded="studio"] [role="tablist"] { justify-content: flex-start; }
      `;
      document.head.appendChild(style);
    }
    if (app === 'canvas') {
      hideInfiniteStandaloneControls();
      collapseInfiniteMobilePanelOnce();
    } else simplifyNovaWorkspace();
  }

  function brandTree(root) {
    if (root instanceof Element) {
      brandElement(root);
      root.querySelectorAll('*').forEach(brandElement);
    }

    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    let node = walker.nextNode();
    while (node) {
      replaceTextNode(node);
      node = walker.nextNode();
    }

    if (document.title !== title) document.title = title;
    if (document.documentElement.dataset.picpilotBranded !== app) {
      document.documentElement.dataset.picpilotBranded = app;
    }

    document.querySelectorAll('link[rel~="icon"]').forEach(link => {
      if (link.getAttribute('href') !== '/picpilot-brand.png') {
        link.setAttribute('href', '/picpilot-brand.png');
      }
    });
    document.querySelectorAll('link[rel="manifest"]').forEach(link => {
      if (link.getAttribute('href') !== '/picpilot-canvas-manifest.json') {
        link.setAttribute('href', '/picpilot-canvas-manifest.json');
      }
    });
    simplifyEmbeddedUi();
    revealEmbeddedUi();
  }

  let scheduled = false;
  function scheduleBranding() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(() => {
      scheduled = false;
      brandTree(document.documentElement);
    });
  }

  function startBranding() {
    new MutationObserver(scheduleBranding).observe(document.documentElement, {
      attributes: true,
      childList: true,
      subtree: true
    });
    window.addEventListener('resize', scheduleBranding, { passive: true });
    scheduleBranding();
  }

  if (document.readyState === 'complete') {
    window.setTimeout(startBranding, 1200);
  } else {
    window.addEventListener('load', () => window.setTimeout(startBranding, 1200), { once: true });
  }
})();
