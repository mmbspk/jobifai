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
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /jobifai ./cmd/server

# ── Stage 3: runtime ──────────────────────────────────────────────────────
# Debian Bookworm slim + Chromium for go-rod browser automation.
FROM debian:bookworm-slim

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
    && rm -rf /var/lib/apt/lists/*

ENV ROD_BROWSER_BIN=/usr/bin/chromium

WORKDIR /app
COPY --from=builder /jobifai ./jobifai
COPY resume_style/ ./resume_style/

# Data and uploads live on a mounted volume
VOLUME ["/app/data", "/app/job_applications", "/app/uploads"]

EXPOSE 8080

ENTRYPOINT ["./jobifai"]
CMD ["-addr", ":8080", "-db", "/app/data/jobifai.db"]
