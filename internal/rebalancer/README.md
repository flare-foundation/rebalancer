# internal/rebalancer

A ready-to-run rebalancer package that combines `pkg/rebalancer` and `pkg/txmng` with real dependencies.

## Overview

This package wires together:
- `pkg/txmng.Manager` — sequential private key transaction manager with EIP-1559 support
- `pkg/rebalancer.Rebalancer` — periodic balance checker and address funder
- `ethclient.Client` — Ethereum chain interactions
- `go-flare-common/pkg/logger` — logging
- Prometheus metrics — sender balance tracking

## Usage

```go
import "github.com/flare-foundation/flare-system-client/internal/rebalancer"

// Load configuration from TOML + ENV
cfg, err := rebalancer.Load("config.toml")
if err != nil {
    // handle error
}

// Create the rebalancer (wires ethclient, txmng, rebalancer packages)
rb, err := rebalancer.New(cfg)
if err != nil {
    // handle error
}

// Run (blocking; start in goroutine)
go func() {
    if err := rb.Run(ctx); err != nil {
        log.Fatalf("rebalancer failed: %v", err)
    }
}()

// The rebalancer now:
// - Monitors and funds tracked addresses
// - Manages transactions via txmng
// - Exposes sender balance via Prometheus
```

## Configuration

Configuration is loaded from TOML file, then overridden by environment variables.

### TOML Example

```toml
check_interval = "5m"
warning_balance_flr = 1000
tx_timeout = "10s"
max_retries = 3

[[addresses]]
address = "0x1234..."
min_balance_wei = 20000000000000000000
top_up_value_wei = 200000000000000000000

[[addresses]]
address = "0x5678..."
# Uses defaults (20 FLR min, 200 FLR top-up in wei)
```

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ETH_RPC_URL` | Yes | Node URL for chain access |
| `REBALANCER_PRIVATE_KEY` | Yes | Sender private key (hex string) |
| `REBALANCER_CHECK_INTERVAL` | No | Balance check interval (overrides TOML) |
| `REBALANCER_WARNING_BALANCE_FLR` | No | Balance threshold for warnings (FLR) |
| `REBALANCER_TX_TIMEOUT` | No | Transaction timeout (overrides TOML) |
| `REBALANCER_MAX_RETRIES` | No | Max tx retries on timeout (overrides TOML) |

### Configuration Defaults

- `CheckInterval`: 5 minutes
- `WarningBalanceFLR`: 1000 FLR (per `pkg/rebalancer`)
- `TxTimeout`: 10 seconds
- `MaxRetries`: 3

Address-specific defaults (if not specified in TOML):
- `MinBalance`: 20 FLR (in wei: 20000000000000000000)
- `TopUpValue`: 200 FLR (in wei: 200000000000000000000)

## Prometheus Metrics

The rebalancer exposes:

- `rebalancer_sender_balance_wei` — Current balance of the sender address in wei
  - Updated every `CheckInterval`

Example query: `rebalancer_sender_balance_wei / 1e18` (convert wei to FLR)

## Logging

All logs are emitted via `go-flare-common/pkg/logger`. Configure logging in your application's logger config.

## Design

- **Sequential transactions**: txmng ensures transactions are sent one at a time with proper nonce ordering
- **Gas bumping**: Automatic 10%+ gas increases on timeout/retry
- **Rebalancing**: Tracks multiple addresses, only funds those below configured minimums
- **Monitoring**: Sender balance exposed to Prometheus for low-fund alerts
