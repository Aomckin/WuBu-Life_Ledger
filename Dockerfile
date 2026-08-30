# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/stockroom ./cmd/stockroom

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata ffmpeg poppler-utils \
    && addgroup -S -g 10001 stockroom \
    && adduser -S -D -H -u 10001 -G stockroom stockroom \
    && mkdir -p /data \
    && chown stockroom:stockroom /data

WORKDIR /app
COPY --from=build /out/stockroom /app/stockroom

USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD ["/app/stockroom", "healthcheck", "http://127.0.0.1:8080/healthz"]

ENTRYPOINT ["/app/stockroom"]
