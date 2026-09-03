FROM golang:1.21-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG APP_VERSION=0.4.4
ARG COMMIT_SHA=unknown
ARG BUILD_TIMESTAMP=unknown
ARG SCHEMA_VERSION=1
ARG WORKTREE_DIRTY=false
ARG BUILD_SOURCE_ID=unknown
ARG IMAGE_REVISION=unknown
RUN CGO_ENABLED=1 go build -ldflags "-X main.BuildVersion=${APP_VERSION} -X main.BuildCommit=${COMMIT_SHA} -X main.BuildTimestamp=${BUILD_TIMESTAMP} -X main.BuildSchema=${SCHEMA_VERSION} -X main.BuildWorktreeDirty=${WORKTREE_DIRTY} -X main.BuildSourceID=${BUILD_SOURCE_ID} -X main.BuildImageRevision=${IMAGE_REVISION}" -o kitsu-discord ./src

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates curl tzdata && rm -rf /var/lib/apt/lists/*
RUN groupadd --gid 10001 kitsusync \
    && useradd --uid 10001 --gid 10001 --home-dir /app --no-create-home --shell /usr/sbin/nologin kitsusync
WORKDIR /app
RUN mkdir -p /app/data /app/logs /app/dump \
    && chown -R 10001:10001 /app/data /app/logs /app/dump
COPY --from=builder --chown=10001:10001 /app/kitsu-discord .
COPY --from=builder --chown=10001:10001 /app/tpl ./tpl
ARG COMMIT_SHA=unknown
ARG WORKTREE_DIRTY=false
ARG BUILD_SOURCE_ID=unknown
LABEL org.opencontainers.image.revision="${COMMIT_SHA}" \
      org.opencontainers.image.source-id="${BUILD_SOURCE_ID}" \
      org.opencontainers.image.dirty="${WORKTREE_DIRTY}"
USER 10001:10001
CMD ["./kitsu-discord"]
