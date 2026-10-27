# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm npm ci --prefer-offline --no-audit
FROM node:22-alpine AS build
WORKDIR /app
ARG NEXT_PUBLIC_CANVAS_LAB_ENABLED=true
ARG NEXT_PUBLIC_CANVAS_NOVA_URL
ARG NEXT_PUBLIC_CANVAS_INFINITE_URL
ENV NEXT_PUBLIC_CANVAS_LAB_ENABLED=$NEXT_PUBLIC_CANVAS_LAB_ENABLED
ENV NEXT_PUBLIC_CANVAS_NOVA_URL=$NEXT_PUBLIC_CANVAS_NOVA_URL
ENV NEXT_PUBLIC_CANVAS_INFINITE_URL=$NEXT_PUBLIC_CANVAS_INFINITE_URL
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build
FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
EXPOSE 3000
CMD ["node","server.js"]
