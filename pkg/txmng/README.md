# Transaction Manager (`txmng`)

A sequential transaction manager for Ethereum-compatible blockchains that handles type 2 (EIP-1559) transactions with automatic retries, gas bumping, and nonce management.

## Overview

`pkg/txmng` manages sending transactions for a single private key. It:
- Builds and signs EIP-1559 DynamicFeeTx transactions
- Estimates gas and applies safety multipliers
- Manages nonce sequentially (one transaction at a time)
- Polls for transaction receipts
- Automatically retries with gas bumping if transactions timeout
- Handles common errors (already known, nonce too low, reverts)

## Installation

```bash
import "github.com/flare-foundation/flare-system-client/pkg/txmng"
```

## Usage

### Creating a Manager

```go
import (
    "crypto/ecdsa"
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/flare-foundation/flare-system-client/pkg/txmng"
)

privateKey := ... // *ecdsa.PrivateKey
client, _ := ethclient.Dial("http://localhost:8545")

cfg := txmng.Config{
    TxTimeout:  10 * time.Second,  // default
    MaxRetries: 3,                 // default
}

mgr, err := txmng.New(privateKey, client, cfg, logger)
// logger can be nil (uses NoOpLogger)
```

### Sending Transactions

```go
// Send a value transfer
err := mgr.Send(ctx, toAddress, amount)

// Send with input data
err := mgr.SendWithInput(ctx, toAddress, amount, inputData)
```

Both methods block until the transaction is confirmed or dropped.

### Running the Manager

The manager operates as a queue processor. Start it in a goroutine:

```go
go func() {
    if err := mgr.Run(ctx); err != nil {
        log.Errorf("manager stopped: %v", err)
    }
}()

// Now Send/SendWithInput will process transactions
```

## Interface: ChainClient

The manager requires a `ChainClient` interface compatible with `ethclient.Client` (v1.16.8+):

```go
type ChainClient interface {
    NetworkID(context.Context) (*big.Int, error)
    SuggestGasPrice(context.Context) (*big.Int, error)
    SuggestGasTipCap(context.Context) (*big.Int, error)
    NonceAt(context.Context, common.Address, *big.Int) (uint64, error)
    EstimateGas(context.Context, ethereum.CallMsg) (uint64, error)
    SendTransaction(context.Context, *types.Transaction) error
    TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error)
    CodeAt(context.Context, common.Address, *big.Int) ([]byte, error)
}
```

**Method Details:**
- `SuggestGasTipCap`: Returns the suggested max priority fee per gas (used for EIP-1559 tip cap)
- `SuggestGasPrice`: Returns the max fee per gas (on Flare/Avalanche, includes base fee)

## Gas Calculation

### Initial Transaction
- **TipCap** = SuggestGasTipCap (directly from node)
- **FeeCap** = SuggestGasPrice (directly from node)
- **GasLimit** = EstimatedGas × 1.5

### Replacement Transaction (on retry)
- Both TipCap and FeeCap are bumped by: `max(fresh values, (previous × 1.1) + 1 wei)`
- The +1 wei ensures strictly > 10% higher (required by nodes for replacement transactions)
- Fresh node values are used if they're higher than the calculated bump

## Retry Logic

Retries occur when:
- Transaction times out (no receipt within `TxTimeout`)
- Send error is an RPC error

Special cases:
- **"nonce too low" on first attempt**: Nonce is re-fetched and transaction is rebuilt (doesn't consume a retry slot)
- **"nonce too low" on subsequent attempts**: Transaction is treated as successful (prior attempt was confirmed)
- **"already known"**: Transaction is treated as successful
- **EstimateGas error**: Transaction is dropped (revert in dry-run)

## Configuration

```go
type Config struct {
    TxTimeout  time.Duration  // default: 10 seconds
    MaxRetries int            // default: 3
}
```

Both fields support TOML serialization.

## Logger Interface

An optional `Logger` can be passed to receive info/debug/warn/error messages:

```go
type Logger interface {
    Infof(format string, args ...any)
    Info(args ...any)
    Debugf(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
}
```

If no logger is provided, a `NoOpLogger` is used (no logging).

## Sender Interface Compatibility

The `Manager` implements the `Sender` interface from `pkg/rebalancer`:

```go
type Sender interface {
    Send(context.Context, common.Address, *big.Int) error
    Address() common.Address
}
```

This allows using `Manager` as the sender for `pkg/rebalancer`.

## Example

```go
package main

import (
    "context"
    "crypto/ecdsa"
    "log"
    "math/big"
    "time"

    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/crypto"
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/flare-foundation/flare-system-client/pkg/txmng"
)

func main() {
    // Setup
    client, _ := ethclient.Dial("http://localhost:8545")
    privateKey, _ := crypto.HexToECDSA("your-private-key")

    cfg := txmng.Config{
        TxTimeout:  10 * time.Second,
        MaxRetries: 3,
    }

    mgr, _ := txmng.New(privateKey, client, cfg, nil)

    // Start manager
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go mgr.Run(ctx)

    // Send transaction
    to := common.HexToAddress("0x...")
    amount := big.NewInt(1e18) // 1 token

    if err := mgr.Send(context.Background(), to, amount); err != nil {
        log.Printf("send failed: %v", err)
    }
}
```

## Sequential Processing

Transactions are processed one at a time. When you call `Send` or `SendWithInput`:

1. The transaction is enqueued
2. The caller blocks waiting for a result
3. The `Run` loop dequeues and processes the transaction
4. Once confirmed or dropped, the result is returned to the caller
5. The next queued transaction is dequeued

This ensures nonce ordering is maintained and simplifies state management.
