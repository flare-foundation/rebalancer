# Multi-stage build for rebalancer
# Stage 1: Build the binary
FROM golang:1.25.5-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -o rebalancer ./cmd/rebalancer

# Stage 2: Runtime image
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS connections
RUN apk add --no-cache ca-certificates

# Copy binary from builder
COPY --from=builder /build/rebalancer .

# Copy example config file
COPY rebalancer.toml .

# Create a default config location
ENV CONFIG_PATH=/app/rebalancer.toml

# Expose metrics port (if using Prometheus)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD [ -f /app/rebalancer.toml ] || exit 1

# Run the rebalancer
ENTRYPOINT ["./rebalancer", "-config", "/app/rebalancer.toml"]
