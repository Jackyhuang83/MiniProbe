# syntax=docker/dockerfile:1
# Multi-arch MiniProbe Server; Agent assets are bundled for existing node installs.
FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS=linux
ARG TARGETARCH
RUN set -eux; \
    test "$TARGETOS" = linux; \
    case "$TARGETARCH" in amd64|arm64) ;; *) exit 1;; esac; \
    mkdir -p /out/downloads; \
    CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" go build -trimpath -ldflags="-s -w" -o /out/miniprobe-server ./cmd/miniprobe-server; \
    for arch in amd64 arm64; do \
      CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "/out/downloads/miniprobe-agent-linux-$arch" ./cmd/miniprobe-agent; \
    done; \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o /out/downloads/miniprobe-agent-linux-armv7 ./cmd/miniprobe-agent

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 miniprobe \
    && adduser -S -D -H -u 10001 -G miniprobe miniprobe \
    && mkdir -p /opt/miniprobe/downloads /var/lib/miniprobe /run/miniprobe \
    && chown -R 10001:10001 /var/lib/miniprobe /run/miniprobe \
    && chmod 0700 /var/lib/miniprobe /run/miniprobe
COPY --from=builder --chmod=0755 /out/miniprobe-server /usr/local/bin/miniprobe-server
COPY --from=builder --chmod=0755 /out/downloads/ /opt/miniprobe/downloads/
USER 10001:10001
ENV MINIPROBE_DEPLOYMENT=docker
EXPOSE 28888
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:28888/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/miniprobe-server"]
CMD ["--listen", ":28888", "--data-dir", "/var/lib/miniprobe", "--downloads", "/opt/miniprobe/downloads", "--admin-socket", "/run/miniprobe/admin.sock"]
