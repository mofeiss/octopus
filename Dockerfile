# Stage 1: Build frontend
FROM node:22-alpine AS frontend

WORKDIR /build/web

RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm config set registry https://registry.npmmirror.com && \
    pnpm config set fetch-retries 10 && \
    pnpm config set fetch-retry-mintimeout 20000 && \
    pnpm config set fetch-retry-maxtimeout 180000 && \
    pnpm config set fetch-timeout 900000 && \
    pnpm install --frozen-lockfile

COPY web/ ./

ARG GIT_VERSION=dev
RUN NEXT_PUBLIC_APP_VERSION="${GIT_VERSION}" pnpm run build

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS backend

RUN apk add --no-cache git python3

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy frontend output to static/out for Go embed
COPY --from=frontend /build/web/out ./static/out

# Generate price presets
RUN python3 scripts/updatePrice.py

# Build metadata
ARG GIT_VERSION=dev
RUN COMMIT_ID=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown') && \
    BUILD_TIME=$(date +'%F %T %z') && \
    CGO_ENABLED=0 go build -o octopus \
      -ldflags="-X 'github.com/bestruirui/octopus/internal/conf.Version=${GIT_VERSION}' \
                -X 'github.com/bestruirui/octopus/internal/conf.BuildTime=${BUILD_TIME}' \
                -X 'github.com/bestruirui/octopus/internal/conf.Author=bestrui' \
                -X 'github.com/bestruirui/octopus/internal/conf.Commit=${COMMIT_ID}' \
                -s -w" \
      -tags=jsoniter .

# Stage 3: Runtime
FROM alpine

ENV TZ=Asia/Shanghai

RUN apk add --no-cache alpine-conf ca-certificates su-exec && \
    /usr/sbin/setup-timezone -z Asia/Shanghai && \
    apk del alpine-conf && \
    rm -rf /var/cache/apk/* && \
    mkdir -p /app

COPY --from=backend /build/octopus /app/octopus
COPY scripts/dockerfiles/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh /app/octopus

EXPOSE 8080

CMD ["/entrypoint.sh"]
