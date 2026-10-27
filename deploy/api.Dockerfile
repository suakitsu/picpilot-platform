# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.3
FROM golang:${GO_VERSION}-bookworm AS dependencies
WORKDIR /src
ENV GOMAXPROCS=2 GOTOOLCHAIN=local
COPY go.mod go.sum ./
COPY core/go.mod core/go.sum ./core/
RUN go mod download && cd core && go mod download

FROM dependencies AS source
COPY cmd/ ./cmd/
COPY core/ ./core/
COPY docs/ ./docs/

FROM source AS test
WORKDIR /src/core
ENV CGO_ENABLED=1
CMD ["go", "test", "-p=2", "-race", "-count=1", "./..."]

FROM source AS native-test
RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates libheif1 libvips42 libvips-tools libimage-exiftool-perl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/core
ENV CGO_ENABLED=1 PICPILOT_TEST_HEIF=1 PICPILOT_HEIF_VIPS_PATH=/usr/bin/vips PICPILOT_HEIF_VIPSHEADER_PATH=/usr/bin/vipsheader
CMD ["go", "test", "-p=2", "-count=1", "./storage", "-run", "TestHEIFNativeSyntheticNormalization"]

FROM source AS builder
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/picpilot ./cmd/picpilot
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates libheif1 libvips42 libvips-tools \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 65532 picpilot \
    && useradd --system --uid 65532 --gid 65532 --home-dir /nonexistent --shell /usr/sbin/nologin picpilot
COPY --from=builder /out/picpilot /picpilot
COPY --from=builder --chown=picpilot:picpilot /out/data /data
ENV PICPILOT_LISTEN=0.0.0.0:8080 PICPILOT_DATA_DIR=/data GOMAXPROCS=2 PICPILOT_HEIF_VIPS_PATH=/usr/bin/vips PICPILOT_HEIF_VIPSHEADER_PATH=/usr/bin/vipsheader
USER picpilot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 CMD ["/picpilot", "--healthcheck"]
ENTRYPOINT ["/picpilot"]
