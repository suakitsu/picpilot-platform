# syntax=docker/dockerfile:1
ARG NODE_VERSION=24
FROM node:${NODE_VERSION}-bookworm-slim AS nova
WORKDIR /src/third_party/nova-image-studio/frontend
COPY canvas-lab/bridge/ /src/canvas-lab/bridge/
COPY third_party/nova-image-studio/frontend/package.json third_party/nova-image-studio/frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY third_party/nova-image-studio/frontend/ ./
RUN npm run build

FROM node:${NODE_VERSION}-bookworm-slim AS infinite
WORKDIR /src/third_party/infinite-canvas/web
COPY canvas-lab/bridge/ /src/canvas-lab/bridge/
COPY third_party/infinite-canvas/VERSION third_party/infinite-canvas/CHANGELOG.md /src/third_party/infinite-canvas/
COPY third_party/infinite-canvas/web/package.json third_party/infinite-canvas/web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY third_party/infinite-canvas/web/ ./
RUN npm run build

FROM caddy:2.10.2-alpine
COPY --from=nova /src/third_party/nova-image-studio/frontend/out/ /srv/canvases/nova/
COPY --from=infinite /src/third_party/infinite-canvas/web/dist/ /srv/canvases/infinite/
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY deploy/Caddyfile.https /etc/caddy/Caddyfile.https
COPY deploy/routes.caddy /etc/caddy/routes.caddy
EXPOSE 8080 8081 80 443 443/udp
