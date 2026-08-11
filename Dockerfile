# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache ca-certificates git

# go.sum may be missing on older clones; download resolves deps from go.mod alone
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o /app/server \
    ./cmd/server

FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache ca-certificates wget tzdata \
    && addgroup -S app \
    && adduser -S app -G app

COPY --from=builder /app/server ./server

RUN mkdir -p uploads && chown -R app:app /app

USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
  CMD wget --quiet --tries=1 -O /dev/null http://127.0.0.1:8080/health || exit 1

CMD ["./server"]
