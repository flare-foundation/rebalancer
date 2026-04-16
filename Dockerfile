FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -o rebalancer ./cmd/rebalancer

FROM alpine:3.22.3

WORKDIR /app

RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 rebalancer \
    && adduser -D -u 10001 -G rebalancer rebalancer \
    && chown rebalancer:rebalancer /app

COPY --from=builder --chown=rebalancer:rebalancer /build/rebalancer .

HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/metrics || exit 1

USER 10001:10001

ENTRYPOINT ["./rebalancer"]
