# PicPilot Canvas Static Build Contract

Both products are static browser frontends. All business APIs belong to Go and
are accessed through the authenticated PicPilot parent bridge. No Node/Python
business runtime, provider credentials, or backend URL is embedded in either build.

## Build Context

Use Node 24. Preserve repository-relative paths and include `canvas-lab/bridge/**`
in both build stages. Build one application at a time. Optional resource limits:
`NODE_OPTIONS=--max-old-space-size=2048`, `RAYON_NUM_THREADS=2`,
`UV_THREADPOOL_SIZE=2`, `NEXT_TELEMETRY_DISABLED=1`.

| Product | Working directory | Command | Output | Browser base |
| --- | --- | --- | --- | --- |
| Nova | third_party/nova-image-studio/frontend | npm ci && npm run build | out/ | /canvas/nova/ |
| Infinite | third_party/infinite-canvas/web | npm ci && npm run build | dist/ | /canvas/infinite/ |

No environment flag is required. Nova fixes embedded mode in next.config.ts;
`NEXT_PUBLIC_PICPILOT_EMBEDDED=true` is redundant but harmless. Infinite does not
need a `VITE_*` variable. Do not configure provider URLs, keys or localhost fallbacks.
Infinite additionally reads `../VERSION` and `../CHANGELOG.md` at build time.

## Static Gateway

Copy Nova's out/ contents to /srv/canvases/nova and Infinite's dist/ contents to
/srv/canvases/infinite. Strip each public prefix when resolving files. Redirect
the bare prefix to its trailing-slash form. Nova uses directory index.html files;
Infinite requires an SPA fallback to its own index.html for page navigation.
Missing JS/CSS/image resources must return 404, not the SPA index.

The parent page, both canvases and /api share exactly one origin. Standalone
canvas loading deliberately fails closed without the parent handshake. CSP must
not contain unsafe-eval. Next inline bootstrap/style support is needed; HTML
previews apply a stricter independent CSP and an opaque-origin sandbox.
Neither embedded product registers a service worker.

This file documents packaging, not release readiness. Check the verification
report before treating the current work-in-progress sources as validated.
