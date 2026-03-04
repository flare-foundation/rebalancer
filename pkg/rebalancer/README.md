# Rebalancer Package

The rebalancer package provides automatic funding of tracked Ethereum addresses to maintain minimum balance thresholds.

## Overview

The Rebalancer is a process that:
- Tracks a configurable list of Ethereum addresses
- Periodically checks their balances
- Automatically funds addresses when their balance falls below a minimum threshold
- Tops up balances to a configured value
- Logs all transactions for audit purposes

## Usage

### Basic Setup

```go
import (
    "context"
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/flare-foundation/flare-system-client/pkg/rebalancer"
)

// Create a balance checker (e.g., using go-ethereum ethclient)
ethClient, err := ethclient.Dial("http://localhost:9650/ext/C/rpc")
if err != nil {
    panic(err)
}

// Implement the Sender interface to handle funding transactions
type MyFunder struct {
    privateKey *ecdsa.PrivateKey
    ethClient  *ethclient.Client
}

func (f *MyFunder) Send(ctx context.Context, to common.Address, amount *big.Int) error {
    // Send transaction to fund address
    // Use ethclient to send the transaction
    return nil
}

// Create a new rebalancer
rb, err := rebalancer.New(rebalancer.Config{
    BalanceChecker: ethClient,  // ethclient.Client implements BalanceChecker
    Sender:         &MyFunder{},
    CheckInterval:  5 * time.Minute,
})
if err != nil {
    panic(err)
}

// Add addresses to track
rb.AddAddress(
    common.HexToAddress("0x123..."),
    rebalancer.DefaultMinBalanceWei(),
    rebalancer.DefaultTopUpValueWei(),
)

// Start the rebalancer in a goroutine
go rb.Run(context.Background())

// Stop gracefully
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
rb.Stop(ctx)
```

## Configuration

### Default Values

- **CheckInterval**: 5 minutes - How often to check balances
- **MinBalance**: 20 FLR - Threshold below which an address is topped up
- **TopUpValue**: 200 FLR - Target balance after top-up

### Custom Balances

```go
minBalance := rebalancer.FLRToWei(50)   // 50 FLR
topUpValue := rebalancer.FLRToWei(500)  // 500 FLR

rb.AddAddress(addr, minBalance, topUpValue)
```

## Interfaces

### BalanceChecker

The `BalanceChecker` interface is used to query address balances on-chain:

```go
type BalanceChecker interface {
    BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}
```

This is compatible with `*ethclient.Client` from go-ethereum, so you can pass it directly. You can also implement your own for testing or custom behavior.

### Sender

The `Sender` interface must be implemented to handle funding transactions:

```go
type Sender interface {
    Send(ctx context.Context, to common.Address, amount *big.Int) error
}
```

The `Send` method is responsible for:
- Creating and signing the transaction
- Handling gas configuration
- Retrying on failure
- Managing nonces

## API

### Adding and Removing Addresses

```go
// Add a tracked address
err := rb.AddAddress(addr, minBal, topUpVal)

// Remove a tracked address
err := rb.RemoveAddress(addr)

// Get all tracked addresses
tracked := rb.GetTrackedAddresses()
```

### Monitoring

```go
// Get current metrics
metrics := rb.GetMetrics()
println("Total fundings:", metrics.TotalFundings)
println("Total amount sent:", metrics.TotalAmountSent.String())
```

### Lifecycle

```go
// Start the rebalancer
go rb.Run(ctx)

// Stop gracefully
stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
rb.Stop(stopCtx)
```

## How It Works

1. **Periodic Checks**: Every `CheckInterval`, the rebalancer:
   - Gets a list of all tracked addresses
   - Checks their current balance on-chain
   - Compares balance against the configured minimum

2. **Funding Decision**: For each address:
   - If balance < minimum: Send `topUpValue - currentBalance` to fund it
   - If balance >= minimum: Log and continue

3. **Metrics**: The rebalancer tracks:
   - Total balance checks performed
   - Total funding transactions sent
   - Total amount funded
   - Last check and fund timestamps

4. **Concurrency**:
   - Balance checks are done in parallel using goroutines
   - Tracked addresses map is protected by read/write locks
   - Safe for concurrent Add/Remove operations

## Integration with Flare System

The rebalancer tracks these addresses:
- Proposer address
- SigningPolicy address
- Submit address
- SubmitSignatures address

These addresses are funded to ensure reliable operation of the system. Each address can have different minimum and top-up values based on its usage patterns.

## Error Handling

The rebalancer:
- Logs all errors but continues operation
- Returns errors for Add/Remove operations if validation fails
- Continues checking other addresses if one fails
- Gracefully handles context cancellation
