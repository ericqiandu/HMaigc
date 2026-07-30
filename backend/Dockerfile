# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS backend-build

ARG GOPROXY=https://proxy.golang.org,direct
ARG APP_VERSION=dev
ARG APP_COMMIT=unknown
ENV GOPROXY=$GOPROXY

RUN apk add --no-cache build-base

WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend ./
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=1 GOOS=linux go build -trimpath \
        -ldflags="-s -w -X infinite-canvas/backend/internal/buildinfo.Version=${APP_VERSION} -X infinite-canvas/backend/internal/buildinfo.Commit=${APP_COMMIT}" \
        -o /out/hmaigc-backend ./cmd/server \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/hmaigc-migrate-sqlite-postgres ./cmd/migrate-sqlite-postgres

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S app \
    && adduser -S app -G app

WORKDIR /app
ENV GIN_MODE=release
ENV CANVAS_BACKEND_ADDR=:8080
ENV CANVAS_BACKEND_DATA_DIR=/data

RUN mkdir -p /data && chown -R app:app /data
COPY --from=backend-build /out/hmaigc-backend /usr/local/bin/hmaigc-backend
COPY --from=backend-build /out/hmaigc-migrate-sqlite-postgres /usr/local/bin/hmaigc-migrate-sqlite-postgres

USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1
CMD ["hmaigc-backend"]
