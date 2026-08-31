# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /src
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bot ./cmd/bot

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata ffmpeg python3 py3-pip \
    && pip3 install --break-system-packages --no-cache-dir yt-dlp \
    && rm -rf /root/.cache
WORKDIR /app

COPY --from=builder /bot /app/bot

USER nobody
ENTRYPOINT ["/app/bot"]
