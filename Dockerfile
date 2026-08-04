FROM golang:1.21-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG APP_VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_TIMESTAMP=unknown
ARG SCHEMA_VERSION=1
RUN CGO_ENABLED=1 go build -ldflags "-X main.BuildVersion=${APP_VERSION} -X main.BuildCommit=${COMMIT_SHA} -X main.BuildTimestamp=${BUILD_TIMESTAMP} -X main.BuildSchema=${SCHEMA_VERSION}" -o kitsu-discord ./src

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates curl tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/kitsu-discord .
CMD ["./kitsu-discord"]
