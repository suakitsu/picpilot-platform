FROM node:22-slim AS frontend-builder

WORKDIR /app

COPY third_party/nova-image-studio/package.json ./package.json
COPY third_party/nova-image-studio/frontend/package.json third_party/nova-image-studio/frontend/package-lock.json ./frontend/
RUN cd frontend && npm ci

COPY third_party/nova-image-studio/frontend/ ./frontend/
COPY canvas-lab/picpilot-canvas-bridge.js canvas-lab/picpilot-canvas-brand.js ./frontend/public/

ENV NEXT_PUBLIC_PICPILOT_EMBEDDED=true
RUN cd frontend && npm run build

FROM tianjiangqiji/nova-image-studio@sha256:858a4ccd26a1d49175a43710741cec635d6b2cfdd5a885942f8b59f2f7516165

COPY --from=frontend-builder /app/frontend/out/ /app/frontend/out/
