# InfraWho container image.
#
# Targets:
#   (default)  runtime     — API + optional SPA for single-box lab/verify
#   api        — API-only binary stage alias (same runtime without forcing WEB_ROOT)
#
# Production (original design): run API-only behind an org reverse proxy for TLS;
# mount KEK from the host (e.g. /etc/infrawho/master.key:ro) and do NOT rely on
# INFRAWHO_WEB_ROOT — serve the SPA separately or via the proxy. See
# docker-compose.prod.example.yml and docs/docker-packaging.md.
#
# Lab / single-box verify: docker-compose.verify.yml builds this image with the
# Vite SPA copied to /app/web and sets INFRAWHO_WEB_ROOT=/app/web.

# --- frontend (Vite) ---------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# Production SPA build; API is same-origin when served by the Go process.
RUN npm run build

# --- backend -----------------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/infrawho ./cmd/infrawho

# --- runtime -----------------------------------------------------------------
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates curl
WORKDIR /app

COPY --from=build /out/infrawho /usr/local/bin/infrawho
# SPA assets for single-box verify; production compose leaves WEB_ROOT unset.
COPY --from=web /web/dist /app/web

ENV INFRAWHO_LISTEN_ADDR=":8080" \
    INFRAWHO_DB_URL="sqlite:///data/infrawho.db" \
    INFRAWHO_MASTER_KEY_FILE="/etc/infrawho/master.key"

EXPOSE 8080

# Liveness: process up. Readiness still needs DB + KEK (see /readyz).
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

# Default: run as root so lab bind-mounted master.key files (often host-owned
# mode 0600) remain readable. Do not add a fixed non-root USER here — that
# breaks typical lab key mounts. For production, run with a matching UID, e.g.
#   docker run --user "$(stat -c %u:%g /etc/infrawho/master.key)" …
# That UID must also write the SQLite data path (e.g. /data).
ENTRYPOINT ["/usr/local/bin/infrawho"]
CMD ["serve"]

# Explicit alias for docs / buildx target selection.
FROM runtime AS api
