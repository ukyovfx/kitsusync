FROM golang:1.26-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o kitsu-discord ./src

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates curl tzdata && rm -rf /var/lib/apt/lists/*
RUN groupadd --gid 10001 kitsusync \
    && useradd --uid 10001 --gid 10001 --home-dir /app --no-create-home --shell /usr/sbin/nologin kitsusync
WORKDIR /app
RUN mkdir -p /app/data /app/logs /app/dump \
    && chown -R 10001:10001 /app/data /app/logs /app/dump
COPY --from=builder --chown=10001:10001 /app/kitsu-discord .
USER 10001:10001
CMD ["./kitsu-discord"]
