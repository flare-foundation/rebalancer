<div align="center">
  <a href="https://flare.network/" target="blank">
    <img src="https://content.flare.network/Flare-2.svg" width="300" alt="Flare Logo" />
  </a>
  <br />
  <a href="CONTRIBUTING.md">Contributing</a>
  ·
  <a href="SECURITY.md">Security</a>
  ·
  <a href="CHANGELOG.md">Changelog</a>
</div>

# Rebalancer

Automated balance rebalancing service for Ethereum accounts on the Flare Network.
Monitors account balances and automatically tops them up when they fall below configured thresholds.
Supports configurable daily and weekly spending limits per address to prevent excessive top-ups.

## Table of Contents

- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [Configuration](#configuration)
- [Docker Deployment](#docker-deployment)
- [Building from Source](#building-from-source)

## Quick Start

The fastest way to get started is using Docker Compose:

```bash
# 1. Copy the example config and customize it
cp rebalancer.toml.example rebalancer.toml

# 2. Edit the config with your addresses and settings
# vim rebalancer.toml

# 3. Set required environment variables
export ETH_RPC_URL="https://your-ethereum-rpc.com"
export REBALANCER_PRIVATE_KEY="your-private-key-without-0x"

# 4. Run with Docker Compose
docker-compose up -d
```

## Prerequisites

- **Docker** (version 20.10+) and **Docker Compose** (version 1.29+), OR
- **Go** (version 1.25.5+) for building from source
- Valid Ethereum RPC endpoint
- Private key for the account that will perform top-ups

## Configuration

### Configuration File

Configuration is defined in `rebalancer.toml`.
Start with the template:

```bash
cp rebalancer.toml.example rebalancer.toml
```

### Environment Variables

The following environment variables **must** be set (no TOML equivalent):

- `ETH_RPC_URL`: Node's RPC endpoint URL
- `REBALANCER_PRIVATE_KEY`: Private key (hex format, without 0x prefix)

Optional overrides (can override TOML values):

- `REBALANCER_CHECK_INTERVAL`: e.g., "5m", "30s"
- `REBALANCER_WARNING_BALANCE_FLR`: e.g., "1000"
- `REBALANCER_TX_TIMEOUT`: e.g., "10s"
- `REBALANCER_MAX_RETRIES`: e.g., "3"
- `REBALANCER_METRICS_ADDR`: e.g., ":8080", ":9090"

### Logger Configuration

Logger settings are configured in the `[logger]` section of `rebalancer.toml`:

```toml
[logger]
level = "info"      # Log level: debug, info, warn, error
format = "text"     # Log format: text or json
```

For advanced logger options, refer to the [go-flare-common logger documentation](https://github.com/flare-foundation/go-flare-common/pkg/logger).

### Rate Limiting

Each tracked address can have optional daily and weekly spending limits (in wei).
When a limit would be exceeded, the top-up is skipped, a warning is logged, and a Prometheus counter is incremented.

```toml
[[addresses]]
address = "0x1234..."
min_balance_wei = "10000000000000000000"
top_up_value_wei = "100000000000000000000"
daily_limit_wei = "500000000000000000000"    # 500 FLR per 24h rolling window
weekly_limit_wei = "2000000000000000000000"   # 2000 FLR per 7d rolling window
```

- Limits use a rolling time window (last 24 hours / last 7 days)
- If not set or set to 0, no limit is applied
- When a limit is reached, the Prometheus metric `rebalancer_topup_limit_reached_total` is incremented (with `address` and `limit_type` labels)

## Docker Deployment

### Using Docker Compose (Recommended)

1. **Prepare configuration:**

```bash
cp rebalancer.toml.example rebalancer.toml
# Edit rebalancer.toml with your addresses and settings
```

2. **Set environment variables:**

Create a `.env` file in the project root:

```env
ETH_RPC_URL=https://your-ethereum-rpc.com
REBALANCER_PRIVATE_KEY=your_private_key_without_0x_prefix
REBALANCER_CHECK_INTERVAL=5m
REBALANCER_WARNING_BALANCE_FLR=1000
```

3. **Start the service:**

```bash
docker-compose up -d
```

4. **Monitor logs:**

```bash
docker-compose logs -f rebalancer
```

5. **Stop the service:**

```bash
docker-compose down
```

### Using Docker CLI

```bash
# Build the image
docker build -t rebalancer:latest .

# Run the container
docker run -d \
  --name rebalancer \
  -e ETH_RPC_URL="https://your-ethereum-rpc.com" \
  -e REBALANCER_PRIVATE_KEY="your_private_key" \
  -p 8080:8080 \
  -v $(pwd)/rebalancer.toml:/app/rebalancer.toml:ro \
  rebalancer:latest
```

### Health Checks

The Docker container includes a health check that runs every 30 seconds.
It verifies that the config file exists:

```bash
docker ps
# Look for "healthy" status under STATUS column
```

### Metrics

The service exposes Prometheus metrics at `/metrics` on port `8080` by default.

```bash
curl http://localhost:8080/metrics
```

| Metric | Type | Description |
|---|---|---|
| `rebalancer_sender_balance_native` | Gauge | Current balance of the rebalancer's signing address in **native token units** (not wei) |
| `rebalancer_topup_limit_reached_total` | Counter | Top-ups skipped due to rate limits, labeled by `address` and `limit_type` (`daily` / `weekly`) |
| `rebalancer_checks` | Gauge | Cumulative number of balance check cycles completed |
| `rebalancer_fundings` | Gauge | Cumulative number of successful top-up transactions sent |
| `rebalancer_successful_topups_total` | Counter | Increments once per successful top-up (for `increase`/`rate` in Prometheus) |
| `rebalancer_topup_amount_native_total` | Counter | Cumulative top-ups in **native token units** (not wei); safe for Prometheus float64 |
| `rebalancer_amount_sent_native` | Gauge | Cumulative amount sent in **native token units** across all top-up transactions |
| `rebalancer_last_check_timestamp_seconds` | Gauge | Unix timestamp of the most recent balance check cycle |
| `rebalancer_last_funding_timestamp_seconds` | Gauge | Unix seconds for the most recent successful top-up; **0 until the first top-up** in this process (do not use `time() - metric` in alerts when the value is 0) |

To change the listen address, set `metrics_addr` in `rebalancer.toml` or use the `REBALANCER_METRICS_ADDR` environment variable.

**Prometheus scrape config:**

```yaml
scrape_configs:
  - job_name: rebalancer
    static_configs:
      - targets: ['localhost:8080']
```

### Logs

View container logs:

```bash
# Using docker-compose
docker-compose logs rebalancer

# Using docker cli
docker logs rebalancer

# Follow logs in real-time
docker logs -f rebalancer
```

## Building from Source

### Requirements

- Go 1.25.5 or higher
- Unix-like OS (Linux, macOS, or WSL)

### Build

```bash
go build -o rebalancer ./cmd/rebalancer
```

### Run

```bash
export ETH_RPC_URL="https://your-ethereum-rpc.com"
export REBALANCER_PRIVATE_KEY="your_private_key"

./rebalancer -config rebalancer.toml
```
