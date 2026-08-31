# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim AS frontend
WORKDIR /build/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-bookworm AS go-build
ENV CGO_ENABLED=0
WORKDIR /build/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go build -trimpath -o /out/novaly-drama .
WORKDIR /build/doubao-web-api
COPY doubao-web-api/go.mod doubao-web-api/go.sum ./
RUN go mod download
COPY doubao-web-api/ ./
RUN go build -trimpath -o /out/doubao-web-api ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates chromium ffmpeg fonts-noto-cjk fonts-liberation \
    xvfb x11vnc novnc websockify openbox x11-utils dbus-x11 \
    bash tini curl lsof procps python3 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1000 --shell /bin/bash novaly
ENV DISPLAY=:99 CHROME_BIN=/usr/bin/chromium \
    NOVALY_LISTEN_HOST=0.0.0.0 NOVALY_LOG_PATH=/app/backend/data/novaly.log \
    GIN_MODE=release PORT=8085 \
    DOUBAO_WEB_API_URL=http://127.0.0.1:8086/api/v3 \
    LANG=C.UTF-8
WORKDIR /app
COPY --from=go-build /out/novaly-drama /app/bin/novaly-drama
COPY --from=go-build /out/doubao-web-api /app/doubao-web-api/bin/doubao-web-api
COPY --from=frontend /build/frontend/dist /app/frontend/dist
COPY frontend/director-desk /app/frontend/director-desk
COPY doubao-web-api/scripts/start-chrome.sh /app/doubao-web-api/scripts/start-chrome.sh
COPY docker/entrypoint.sh /app/docker/entrypoint.sh
RUN mkdir -p /app/backend/data /app/doubao-web-api/data /app/doubao-web-api/session \
    && chmod +x /app/docker/entrypoint.sh /app/doubao-web-api/scripts/start-chrome.sh \
    && chown -R novaly:novaly /app
USER novaly
EXPOSE 8085 8086 6080
HEALTHCHECK --interval=15s --timeout=5s --start-period=40s --retries=5 \
    CMD curl -fsS http://127.0.0.1:8085/api/health || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/app/docker/entrypoint.sh"]
