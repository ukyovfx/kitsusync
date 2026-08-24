FROM golang:1.21-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG APP_VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_TIMESTAMP=unknown
ARG SCHEMA_VERSION=1
ARG WORKTREE_DIRTY=false
ARG BUILD_SOURCE_ID=unknown
ARG IMAGE_REVISION=unknown
RUN CGO_ENABLED=1 go build -ldflags "-X main.BuildVersion=${APP_VERSION} -X main.BuildCommit=${COMMIT_SHA} -X main.BuildTimestamp=${BUILD_TIMESTAMP} -X main.BuildSchema=${SCHEMA_VERSION} -X main.BuildWorktreeDirty=${WORKTREE_DIRTY} -X main.BuildSourceID=${BUILD_SOURCE_ID} -X main.BuildImageRevision=${IMAGE_REVISION}" -o kitsu-discord ./src

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates curl tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/kitsu-discord .
COPY --from=builder /app/tpl ./tpl
COPY --from=builder /app/docs.html /app/site.jsx ./
ARG COMMIT_SHA=unknown
ARG WORKTREE_DIRTY=false
ARG BUILD_SOURCE_ID=unknown
LABEL org.opencontainers.image.revision="${COMMIT_SHA}" \
      org.opencontainers.image.source-id="${BUILD_SOURCE_ID}" \
      org.opencontainers.image.dirty="${WORKTREE_DIRTY}"
CMD ["./kitsu-discord"]
