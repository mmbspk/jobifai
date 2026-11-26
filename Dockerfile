# ── Stage 1: build frontend ────────────────────────────────────────────────
FROM node:22-slim AS frontend

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ── Stage 2: build Go server ───────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /web/dist ./web/dist
ARG BUILD_VERSION=dev
ARG BUILD_GIT_SHA=unknown
ARG BUILD_TIMESTAMP
ARG BUILD_SOURCE_STATE=unknown
RUN JOBIFAI_BUILD_VERSION="$BUILD_VERSION" \
    JOBIFAI_BUILD_GIT_SHA="$BUILD_GIT_SHA" \
    JOBIFAI_BUILD_TIME="$BUILD_TIMESTAMP" \
    JOBIFAI_SOURCE_STATE="$BUILD_SOURCE_STATE" \
    CGO_ENABLED=0 GOOS=linux bash scripts/build.sh go build -buildvcs=false -trimpath -o /jobifai ./cmd/server

# ── Stage 3: runtime ──────────────────────────────────────────────────────
# Debian Bookworm slim + Chromium for go-rod browser automation.
FROM debian:bookworm-slim
ARG BUILD_VERSION=dev
ARG BUILD_GIT_SHA=unknown
ARG BUILD_TIMESTAMP
LABEL org.opencontainers.image.version="$BUILD_VERSION" \
      org.opencontainers.image.revision="$BUILD_GIT_SHA" \
      org.opencontainers.image.created="$BUILD_TIMESTAMP"

RUN apt-get update && apt-get install -y --no-install-recommends \
        chromium \
        chromium-driver \
        ca-certificates \
        fonts-liberation \
        libnss3 \
        libatk-bridge2.0-0 \
        libgtk-3-0 \
        libx11-xcb1 \
        libxcomposite1 \
        libxdamage1 \
        libxrandr2 \
        libgbm1 \
        libasound2 \
        xvfb \
        x11vnc \
        novnc \
    && rm -rf /var/lib/apt/lists/*

ENV ROD_BROWSER_BIN=/usr/bin/chromium
ENV DISPLAY=:99

WORKDIR /app
COPY --from=builder /jobifai ./jobifai
COPY --from=builder /src/web/dist ./web/dist
COPY --from=builder /src/resume_markets ./resume_markets
COPY docker-entrypoint.sh ./docker-entrypoint.sh
RUN chmod +x ./docker-entrypoint.sh \
    && mkdir -p resume_style job_applications uploads

EXPOSE 8081

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["-addr", ":8081", "-db", "/app/data/jobifai.db"]
