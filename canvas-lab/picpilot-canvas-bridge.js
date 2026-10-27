(() => {
  const currentScript = document.currentScript;
  const app = currentScript?.dataset.app === 'canvas' ? 'canvas' : 'studio';
  currentScript?.remove();
  document.documentElement.dataset.picpilotCanvasBridge = 'true';
  const protocol = 'picpilot-canvas-v1';
  const cancelledCode = 'PICPILOT_REQUEST_CANCELLED';
  const parentOriginStorageKey = 'picpilot:canvas:parent-origin';
  const pending = new Map();
  const requestTimeoutMs = 20 * 60 * 1000;
  let runtimeConfig = null;

  if (app === 'studio') {
    window.setTimeout(() => {
      if (document.documentElement.dataset.picpilotNovaBootstrapping === 'false') return;
      document.documentElement.dataset.picpilotNovaBootstrapping = 'false';
      window.dispatchEvent(new CustomEvent('picpilot:nova:model-timeout'));
    }, 8000);
  }

  function id() {
    if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
    return `canvas-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  }

  function parentOrigin() {
    try {
      if (document.referrer) {
        const referrerOrigin = new URL(document.referrer).origin;
        if (referrerOrigin !== window.location.origin) {
          sessionStorage.setItem(parentOriginStorageKey, referrerOrigin);
          return referrerOrigin;
        }
      }
      return sessionStorage.getItem(parentOriginStorageKey) || '*';
    } catch {
      return '*';
    }
  }

  function request(action, payload = {}) {
    if (window.parent === window) {
      return Promise.reject(new Error('请从 PicPilot 工作台打开此功能。'));
    }
    const requestId = id();
    return new Promise((resolve, reject) => {
      const timeout = window.setTimeout(() => {
        pending.delete(requestId);
        reject(new Error('PicPilot 任务等待超时，请到历史记录确认任务状态。'));
      }, requestTimeoutMs);
      pending.set(requestId, { resolve, reject, timeout });
      window.parent.postMessage({
        protocol,
        type: 'picpilot:canvas:request',
        app,
        requestId,
        action,
        payload
      }, parentOrigin());
    });
  }

  window.PicPilotCanvasBridge = Object.freeze({
    request,
    getConfig: () => runtimeConfig
  });

  function imageModelScript() {
    return `const result = await window.PicPilotCanvasBridge.request("image.generate", { prompt, images, params, model });
return result.images || [];`;
  }

  function textModelScript() {
    return `const result = await window.PicPilotCanvasBridge.request("text.generate", { messages, params, model, systemPrompt, reasoningEffort });
const text = String(result.text || "");
if (onDelta && text) onDelta(text);
return text;`;
  }

  function videoModelScript(modelCode) {
    return `const result = await window.PicPilotCanvasBridge.request("video.generate", { prompt, images, params, model, modelCode: ${JSON.stringify(modelCode)} });
return { url: result.url };`;
  }

  function fallbackRuntimeConfig() {
    return {
      image: {
        enabled: true,
        model: 'gpt-image-2',
        prices: { '1K': 6, '2K': 24, '4K': 30 }
      },
      text: { enabled: true, model: 'PicPilot 文案', price: 0 },
      video: { enabled: false, models: [] }
    };
  }

  function readRuntimeConfig() {
    try {
      const bootstrap = new URL(window.location.href).searchParams.get('picpilotConfig');
      if (bootstrap) {
        const parsed = JSON.parse(bootstrap);
        if (parsed && typeof parsed === 'object') return parsed;
      }
      const stored = sessionStorage.getItem('picpilot:canvas:runtime-config');
      return stored ? JSON.parse(stored) : fallbackRuntimeConfig();
    } catch {
      return fallbackRuntimeConfig();
    }
  }

  function persistedInfiniteState() {
    try {
      return JSON.parse(localStorage.getItem('infinite-canvas:ai_config_store') || '{}');
    } catch {
      return {};
    }
  }

  function configureInfinite(config) {
    const existing = persistedInfiniteState();
    const previous = existing?.state?.config || {};
    const imageModel = {
      name: 'PicPilot 生图',
      capability: 'image',
      script: imageModelScript()
    };
    const textModel = {
      name: 'PicPilot 文案',
      capability: 'text',
      script: textModelScript()
    };
    const videoModels = (config.video?.models || [])
      .filter(model => model?.enabled)
      .map(model => ({
        name: `PicPilot 视频 · ${model.name || model.code}`,
        capability: 'video',
        script: videoModelScript(model.code)
      }));
    document.documentElement.dataset.picpilotVideoEnabled = videoModels.length ? 'true' : 'false';
    const models = [imageModel, textModel, ...videoModels];
    const values = models.map(model => `picpilot::${model.name}`);
    const firstVideo = videoModels[0]?.name || '';
    const nextConfig = {
      ...previous,
      channelMode: 'local',
      baseUrl: 'https://bridge.invalid',
      apiKey: 'picpilot-bridge',
      apiFormat: 'openai',
      channels: [{
        id: 'picpilot',
        name: 'PicPilot',
        baseUrl: 'https://bridge.invalid',
        apiKey: 'picpilot-bridge',
        apiFormat: 'openai',
        models
      }],
      model: 'picpilot::PicPilot 生图',
      imageModel: 'picpilot::PicPilot 生图',
      textModel: 'picpilot::PicPilot 文案',
      videoModel: firstVideo ? `picpilot::${firstVideo}` : '',
      audioModel: '',
      models: values,
      quality: previous.quality || 'low',
      size: previous.size || '1:1',
      count: '1',
      canvasImageCount: '1',
      videoSeconds: previous.videoSeconds || '10',
      vquality: previous.vquality || '720',
      videoGenerateAudio: previous.videoGenerateAudio || 'true',
      videoWatermark: 'false'
    };
    localStorage.setItem('infinite-canvas:ai_config_store', JSON.stringify({
      ...existing,
      state: {
        ...(existing.state || {}),
        config: nextConfig,
        webdav: {
          url: '',
          username: '',
          password: '',
          directory: 'picpilot-canvas',
          lastSyncedAt: ''
        }
      },
      version: Number.isFinite(existing.version) ? existing.version : 0
    }));
    window.dispatchEvent(new CustomEvent('picpilot:canvas:config-applied'));
  }

  function configureNova() {
    const registry = {
      imageModels: [{
        id: 'picpilot-image',
        protocol: 'openai',
        name: 'PicPilot 生图',
        modelId: 'gpt-image-2',
        apiKey: 'picpilot-bridge',
        baseUrl: 'https://bridge.invalid',
        builtinPreset: 'gpt-image-2',
        maxRefImages: 10,
        maxOutputSize: '4K',
        supportsAdvancedParams: true
      }],
      textModels: [{
        id: 'picpilot-text',
        protocol: 'openai-chat-completions',
        name: 'PicPilot 文案',
        modelId: 'gpt-5.5',
        apiKey: 'picpilot-bridge',
        baseUrl: 'https://bridge.invalid',
        note: '由 PicPilot 后台统一路由'
      }],
      defaults: {
        textToImage: 'picpilot-image',
        imageToImage: 'picpilot-image',
        reversePrompt: 'picpilot-text',
        agent: 'picpilot-text',
        promptOptimize: 'picpilot-text',
        imageDescribe: 'picpilot-text'
      }
    };
    localStorage.setItem('nova-model-registry', JSON.stringify(registry));
    for (const key of ['nova-image-generation-settings', 'nova-t2i-settings', 'nova-i2i-settings']) {
      let value = {};
      try {
        value = JSON.parse(localStorage.getItem(key) || '{}');
      } catch {
        value = {};
      }
      localStorage.setItem(key, JSON.stringify({ ...value, model: 'picpilot-image' }));
    }
  }

  function novaControlLabel(element) {
    return (element?.textContent || '').replace(/\s+/g, ' ').trim()
      || (element?.getAttribute?.('aria-label') || '').trim()
      || (element?.getAttribute?.('title') || '').trim();
  }

  function findNovaButton(scope, label) {
    if (!scope) return null;
    return [...scope.querySelectorAll('button')]
      .find(button => novaControlLabel(button) === label);
  }

  function selectNovaPicPilotModel(attempt = 0) {
    const modelButton = [...document.querySelectorAll('button')]
      .find(button => novaControlLabel(button) === '模型选择');
    if (!modelButton) {
      if (attempt < 30) window.setTimeout(() => selectNovaPicPilotModel(attempt + 1), 100);
      return;
    }
    modelButton.click();

    window.setTimeout(() => {
      const option = [...document.querySelectorAll('button')]
        .find(button => button !== modelButton
          && button.getClientRects().length > 0
          && novaControlLabel(button) === 'PicPilot 生图');
      if (!option) {
        if (attempt < 30) selectNovaPicPilotModel(attempt + 1);
        return;
      }
      option.click();
      document.documentElement.dataset.picpilotNovaBootstrapping = 'false';
      window.dispatchEvent(new CustomEvent('picpilot:nova:model-ready'));
    }, 100);
  }

  function saveNovaSettings(attempt = 0) {
    const settings = [...document.querySelectorAll('[role="dialog"]')]
      .find(dialog => (dialog.textContent || '').includes('模型级独立配置'));
    if (!settings) {
      if (attempt < 30) window.setTimeout(() => saveNovaSettings(attempt + 1), 100);
      return;
    }

    const saveButton = findNovaButton(settings, '保存设置');
    if (!saveButton) {
      if (attempt < 30) window.setTimeout(() => saveNovaSettings(attempt + 1), 100);
      return;
    }
    saveButton.click();
    window.setTimeout(() => {
      const closeButton = [...document.querySelectorAll('button')]
        .find(button => novaControlLabel(button) === 'Close');
      closeButton?.click();
      window.setTimeout(() => selectNovaPicPilotModel(), 150);
    }, 250);
  }

  function bootstrapNovaModelState(attempt = 0) {
    document.documentElement.dataset.picpilotNovaBootstrapping = 'true';
    const selectedModel = [...document.querySelectorAll('button')]
      .find(button => novaControlLabel(button) === 'PicPilot 生图'
        && !button.closest('[role="dialog"]'));
    if (selectedModel) {
      document.documentElement.dataset.picpilotNovaBootstrapping = 'false';
      window.dispatchEvent(new CustomEvent('picpilot:nova:model-ready'));
      return;
    }
    const settingsButton = [...document.querySelectorAll('button')]
      .find(button => novaControlLabel(button) === '设置'
        && !button.closest('[role="dialog"]'));
    if (settingsButton) {
      settingsButton.click();
      window.setTimeout(() => saveNovaSettings(), 100);
      return;
    }

    const warning = [...document.querySelectorAll('p')]
      .find(element => (element.textContent || '').includes('API 密钥未配置'));
    if (!warning) {
      if (attempt < 30) window.setTimeout(() => bootstrapNovaModelState(attempt + 1), 100);
      return;
    }

    let warningPanel = warning.parentElement;
    let configButton = null;
    for (let depth = 0; warningPanel && depth < 5 && !configButton; depth += 1) {
      configButton = findNovaButton(warningPanel, '配置');
      warningPanel = warningPanel.parentElement;
    }
    if (!configButton) return;
    configButton.click();

    window.setTimeout(() => {
      const gate = [...document.querySelectorAll('[role="dialog"]')]
        .find(dialog => (dialog.textContent || '').includes('请先配置 API 密钥'));
      const continueButton = findNovaButton(gate, '配置');
      if (!continueButton) return;
      continueButton.click();

      window.setTimeout(() => {
        const settings = [...document.querySelectorAll('[role="dialog"]')]
          .find(dialog => (dialog.textContent || '').includes('模型级独立配置'));
        if (!settings) return;
        saveNovaSettings();
      }, 120);
    }, 120);
  }

  function configureNovaAfterHydration() {
    const apply = () => window.setTimeout(() => {
      configureNova();
      window.dispatchEvent(new CustomEvent('picpilot:nova:configured'));
      const bootstrapVersion = 'picpilot-nova-registry-v2';
      if (sessionStorage.getItem('picpilot:nova:bootstrap-version') !== bootstrapVersion) {
        sessionStorage.setItem('picpilot:nova:bootstrap-version', bootstrapVersion);
        window.location.reload();
        return;
      }
      bootstrapNovaModelState();
    }, 400);
    if (document.readyState === 'complete') apply();
    else window.addEventListener('load', apply, { once: true });
  }

  function jsonResponse(value, status = 200) {
    return new Response(JSON.stringify(value), {
      status,
      headers: { 'Content-Type': 'application/json' }
    });
  }

  async function requestJsonBody(input, init) {
    const sourceBody = init.body ?? (input instanceof Request ? await input.clone().text() : '');
    if (sourceBody && typeof sourceBody === 'object') return sourceBody;
    return JSON.parse(String(sourceBody || '{}'));
  }

  function novaTextStreamResponse(value) {
    const response = value && typeof value === 'object' ? value : {};
    const choice = response.choices?.[0] || {};
    const message = choice.message || {};
    const delta = {};
    if (message.content != null) delta.content = message.content;
    if (message.reasoning_content != null) delta.reasoning_content = message.reasoning_content;
    if (message.reasoning != null) delta.reasoning = message.reasoning;
    if (Array.isArray(message.tool_calls)) delta.tool_calls = message.tool_calls;
    const identifier = response.id || `picpilot-text-${id()}`;
    const model = response.model || 'PicPilot 文案';
    const first = {
      id: identifier,
      object: 'chat.completion.chunk',
      model,
      choices: [{ index: 0, delta, finish_reason: null }]
    };
    const last = {
      id: identifier,
      object: 'chat.completion.chunk',
      model,
      choices: [{
        index: 0,
        delta: {},
        finish_reason: choice.finish_reason || (Array.isArray(message.tool_calls) ? 'tool_calls' : 'stop')
      }]
    };
    return new Response(`data: ${JSON.stringify(first)}\n\ndata: ${JSON.stringify(last)}\n\ndata: [DONE]\n\n`, {
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream; charset=utf-8',
        'Cache-Control': 'no-cache'
      }
    });
  }

  function installNovaFetchBridge() {
    const nativeFetch = window.fetch.bind(window);
    window.fetch = async (input, init = {}) => {
      const rawUrl = input instanceof Request ? input.url : String(input);
      const url = new URL(rawUrl, window.location.href);
      const method = String(init.method || (input instanceof Request ? input.method : 'GET')).toUpperCase();

      if (url.pathname === '/api/nova/proxy/text' && method === 'POST') {
        try {
          const body = await requestJsonBody(input, init);
          const result = await request('text.generate', {
            protocol: body.protocol || 'openai-chat-completions',
            model: body.model || 'gpt-5.5',
            requestBody: body.requestBody && typeof body.requestBody === 'object' ? body.requestBody : {},
            stream: body.stream !== false
          });
          const response = result.response && typeof result.response === 'object'
            ? result.response
            : { choices: [{ message: { role: 'assistant', content: String(result.text || '') }, finish_reason: 'stop' }] };
          return body.stream === false ? jsonResponse(response) : novaTextStreamResponse(response);
        } catch (error) {
          return jsonResponse({
            error: { message: error instanceof Error ? error.message : String(error) },
            code: 'PICPILOT_TEXT_BRIDGE_FAILED'
          }, 400);
        }
      }
      if (url.pathname === '/api/nova/proxy/models') {
        return jsonResponse({ data: [{ id: 'gpt-image-2' }, { id: 'gpt-5.5' }] });
      }
      if (url.pathname === '/api/nova/queue-status') {
        return jsonResponse({
          concurrencyLimit: 1,
          configuredConcurrency: 1,
          processingCount: 0,
          queuedCount: 0,
          pendingCount: 0,
          maxQueueSize: 20,
          remainingQueueSlots: 20,
          displayConcurrency: 0,
          displayQueued: 0,
          acceptingNewTasks: true,
          serverMessage: 'PicPilot 统一任务队列'
        });
      }
      if (url.pathname === '/api/nova/tasks' && method === 'POST') {
        try {
          const body = await requestJsonBody(input, init);
          const result = await request('image.submit', {
            prompt: body.prompt || '',
            images: (body.images || []).map(image => image?.data
              ? `data:${image.mimeType || 'image/png'};base64,${image.data}`
              : '').filter(Boolean),
            params: {
              outputSize: body.outputSize,
              customSize: body.customSize,
              aspectRatio: body.aspectRatio,
              count: body.parallelCount || 1,
              quality: body.gptImageQuality,
              style: body.gptImageStyle,
              background: body.gptImageBackground
            },
            model: body.model || 'gpt-image-2'
          });
          return jsonResponse({ taskId: result.taskId });
        } catch (error) {
          return jsonResponse({ error: error instanceof Error ? error.message : String(error), code: 'PICPILOT_BRIDGE_FAILED' }, 400);
        }
      }
      const taskMatch = url.pathname.match(/^\/api\/nova\/tasks\/([^/]+)(?:\/(ack))?$/);
      if (taskMatch) {
        if (taskMatch[2] === 'ack') return jsonResponse({ acknowledged: true });
        try {
          const result = await request('image.status', { taskId: decodeURIComponent(taskMatch[1]) });
          return jsonResponse(result.task);
        } catch (error) {
          return jsonResponse({ error: error instanceof Error ? error.message : String(error), code: 'PICPILOT_STATUS_FAILED' }, 400);
        }
      }
      return nativeFetch(input, init);
    };
  }

  runtimeConfig = readRuntimeConfig();
  if (app === 'canvas') {
    configureInfinite(runtimeConfig);
  } else {
    installNovaFetchBridge();
    configureNovaAfterHydration();
  }

  window.addEventListener('message', event => {
    if (event.source !== window.parent) return;
    const allowedOrigin = parentOrigin();
    if (allowedOrigin !== '*' && event.origin !== allowedOrigin) return;
    const message = event.data;
    if (!message || message.protocol !== protocol) return;

    if (message.type === 'picpilot:canvas:response') {
      const waiter = pending.get(message.requestId);
      if (!waiter) return;
      window.clearTimeout(waiter.timeout);
      pending.delete(message.requestId);
      if (message.ok) {
        waiter.resolve(message.data || {});
      } else if (message.error === cancelledCode) {
        waiter.reject(new DOMException('Aborted', 'AbortError'));
      } else {
        waiter.reject(new Error(message.error || 'PicPilot 任务处理失败'));
      }
      return;
    }

    if (message.type === 'picpilot:canvas:config') {
      runtimeConfig = message.config || fallbackRuntimeConfig();
      if (app !== 'canvas') return;
      const serialized = JSON.stringify(runtimeConfig);
      const previous = sessionStorage.getItem('picpilot:canvas:runtime-config') || '';
      sessionStorage.setItem('picpilot:canvas:runtime-config', serialized);
      configureInfinite(runtimeConfig);
      if (serialized !== previous) {
        window.dispatchEvent(new CustomEvent('picpilot:canvas:runtime-config-updated'));
      }
    }
  });

  function announceReady() {
    if (window.parent === window) return;
    window.parent.postMessage({
      protocol,
      type: 'picpilot:canvas:ready',
      app
    }, parentOrigin());
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', announceReady, { once: true });
  } else {
    announceReady();
  }
})();
