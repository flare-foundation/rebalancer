package txmng

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Manager manages sending transactions for a single private key.
// It handles nonce management, gas estimation, signing, and retries.
type Manager struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
	chainID    *big.Int

	client ChainClient
	logger Logger

	txTimeout  time.Duration
	maxRetries int

	queue chan txRequest
}

// New creates a new Manager instance.
func New(
	privateKey *ecdsa.PrivateKey,
	client ChainClient,
	cfg Config,
	logger Logger,
) (*Manager, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("txmng.New: private key is required")
	}
	if client == nil {
		return nil, fmt.Errorf("txmng.New: client is required")
	}

	// Apply defaults
	if cfg.TxTimeout <= 0 {
		cfg.TxTimeout = DefaultTxTimeout
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("txmng.New: validating config: %w", err)
	}

	// Fetch chainID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	chainID, err := client.NetworkID(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("txmng.New: NetworkID failed: %w", err)
	}

	// Default logger if none provided
	if logger == nil {
		logger = &NoOpLogger{}
	}

	// Derive address
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	m := &Manager{
		privateKey: privateKey,
		address:    address,
		chainID:    chainID,
		client:     client,
		logger:     logger,
		txTimeout:  cfg.TxTimeout,
		maxRetries: cfg.MaxRetries,
		queue:      make(chan txRequest, DefaultQueueSize),
	}

	return m, nil
}

// Address returns the Ethereum address of the managed private key.
// It satisfies the Sender interface from pkg/rebalancer.
func (m *Manager) Address() common.Address {
	return m.address
}

// Send enqueues a value transfer transaction and blocks until it is confirmed or dropped.
func (m *Manager) Send(ctx context.Context, to common.Address, amount *big.Int) error {
	return m.SendWithInput(ctx, to, amount, nil)
}

// SendWithInput enqueues a transaction with optional input data and blocks until confirmed or dropped.
func (m *Manager) SendWithInput(ctx context.Context, to common.Address, amount *big.Int, input []byte) error {
	if amount == nil {
		amount = big.NewInt(0)
	}

	req := txRequest{
		to:     to,
		amount: new(big.Int).Set(amount),
		input:  input,
		result: make(chan error, 1), // buffered so Run never blocks writing
	}

	// Enqueue request
	select {
	case m.queue <- req:
	case <-ctx.Done():
		return fmt.Errorf("txmng.SendWithInput: enqueue failed: %w", ctx.Err())
	}

	// Wait for result
	select {
	case err := <-req.result:
		return err
	case <-ctx.Done():
		return fmt.Errorf("txmng.SendWithInput: waiting for result: %w", ctx.Err())
	}
}

// Run starts the transaction processing loop. It blocks until ctx is cancelled.
// Exactly one transaction is processed at a time (sequential mode).
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Infof("txmng: started for address %s", m.address.Hex())

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("txmng: context cancelled, stopping")
			return ctx.Err()

		case req := <-m.queue:
			m.processTransaction(ctx, req)
		}
	}
}

// processTransaction processes a single transaction request with retries.
func (m *Manager) processTransaction(runCtx context.Context, req txRequest) {
	defer func() {
		// Ensure result is always written, even if something panics
		if recover() != nil {
			req.result <- fmt.Errorf("txmng: processTransaction panicked")
		}
	}()

	// Step 1: Estimate gas (dry run)
	ctx, cancel := context.WithTimeout(runCtx, m.txTimeout)
	gasLimit, err := estimateGasLimit(ctx, m.client, m.address, req.to, req.amount, req.input)
	cancel()
	if err != nil {
		m.logger.Warnf("txmng: EstimateGas failed (dropping tx): %v", err)
		req.result <- fmt.Errorf("txmng: dry run failed: %w", err)
		return
	}

	// Step 2: Fetch nonce
	ctx, cancel = context.WithTimeout(runCtx, m.txTimeout)
	nonce, err := m.client.NonceAt(ctx, m.address, nil)
	cancel()
	if err != nil {
		req.result <- fmt.Errorf("txmng: NonceAt failed: %w", err)
		return
	}

	// Step 3: Build initial gas params
	ctx, cancel = context.WithTimeout(runCtx, m.txTimeout)
	tipCap, feeCap, err := buildGasParams(ctx, m.client)
	cancel()
	if err != nil {
		req.result <- fmt.Errorf("txmng: buildGasParams failed: %w", err)
		return
	}

	// Step 4: Sign initial tx
	signedTx, err := m.buildAndSignTx(nonce, req.to, req.amount, req.input, gasLimit, tipCap, feeCap)
	if err != nil {
		req.result <- fmt.Errorf("txmng: sign failed: %w", err)
		return
	}

	// Step 5: Retry loop
	for attempt := 0; attempt < m.maxRetries; attempt++ {
		m.logger.Debugf("txmng: attempt %d, nonce %d, hash %s", attempt, nonce, signedTx.Hash().Hex())

		// Send transaction
		ctx, cancel = context.WithTimeout(runCtx, m.txTimeout)
		sendErr := m.client.SendTransaction(ctx, signedTx)
		cancel()

		if sendErr != nil {
			if isAlreadyKnown(sendErr) {
				// Exact duplicate in mempool; treat as success.
				m.logger.Infof("txmng: tx already known, dropping: %s", signedTx.Hash().Hex())
				req.result <- nil
				return
			}

			if isNonceTooLow(sendErr) && attempt == 0 {
				// On first attempt, nonce was stale; re-fetch and rebuild.
				m.logger.Warnf("txmng: nonce too low on first attempt, re-fetching nonce")
				ctx, cancel = context.WithTimeout(runCtx, m.txTimeout)
				nonce, err = m.client.NonceAt(ctx, m.address, nil)
				cancel()
				if err != nil {
					req.result <- fmt.Errorf("txmng: re-fetch nonce failed: %w", err)
					return
				}
				signedTx, err = m.buildAndSignTx(nonce, req.to, req.amount, req.input, gasLimit, tipCap, feeCap)
				if err != nil {
					req.result <- fmt.Errorf("txmng: rebuild tx failed: %w", err)
					return
				}
				attempt-- // Don't consume retry slot
				continue
			}

			if isNonceTooLow(sendErr) {
				// On subsequent attempts, nonce too low means prior attempt succeeded.
				m.logger.Infof("txmng: nonce too low on retry %d, prior attempt succeeded", attempt)
				req.result <- nil
				return
			}

			// Other RPC or error: log and retry.
			m.logger.Warnf("txmng: SendTransaction error (attempt %d): %v", attempt, sendErr)
		} else {
			// Send succeeded; poll for receipt
			confirmed, dropped, pollErr := m.pollReceipt(runCtx, signedTx.Hash())
			if confirmed {
				m.logger.Infof("txmng: tx confirmed: %s", signedTx.Hash().Hex())
				req.result <- nil
				return
			}
			if dropped {
				m.logger.Warnf("txmng: tx reverted: %s", signedTx.Hash().Hex())
				req.result <- fmt.Errorf("txmng: tx reverted: %s", signedTx.Hash().Hex())
				return
			}
			// pollErr != nil means timeout or context cancel.
			if pollErr != nil && !isTimeout(pollErr) {
				// Genuine context cancellation: stop.
				req.result <- fmt.Errorf("txmng: poll interrupted: %w", pollErr)
				return
			}
			// Timeout: will retry with bumped gas.
			m.logger.Debugf("txmng: tx timed out, bumping gas for retry %d", attempt+1)
		}

		// Prepare replacement tx for next iteration
		if attempt+1 < m.maxRetries {
			ctx, cancel = context.WithTimeout(runCtx, m.txTimeout)
			tipCap, feeCap, err = bumpGasParams(ctx, m.client, tipCap, feeCap)
			cancel()
			if err != nil {
				m.logger.Warnf("txmng: bumpGasParams failed: %v", err)
				// Fallback: use pure math without fresh data
				tipCap = new(big.Int).Mul(tipCap, big.NewInt(int64(RetryGasBumpNumerator)))
				tipCap.Div(tipCap, big.NewInt(int64(RetryGasBumpDivisor)))
				feeCap = new(big.Int).Mul(feeCap, big.NewInt(int64(RetryGasBumpNumerator)))
				feeCap.Div(feeCap, big.NewInt(int64(RetryGasBumpDivisor)))
			}

			signedTx, err = m.buildAndSignTx(nonce, req.to, req.amount, req.input, gasLimit, tipCap, feeCap)
			if err != nil {
				req.result <- fmt.Errorf("txmng: rebuild tx for retry failed: %w", err)
				return
			}
		}
	}

	// Max retries exhausted
	m.logger.Errorf("txmng: max retries (%d) exhausted for nonce %d", m.maxRetries, nonce)
	req.result <- fmt.Errorf("txmng: max retries exhausted")
}

// buildAndSignTx builds and signs a DynamicFeeTx.
func (m *Manager) buildAndSignTx(
	nonce uint64,
	to common.Address,
	amount *big.Int,
	input []byte,
	gasLimit uint64,
	gasTipCap *big.Int,
	gasFeeCap *big.Int,
) (*types.Transaction, error) {
	txData := &types.DynamicFeeTx{
		ChainID:   m.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     amount,
		Data:      input,
	}

	tx := types.NewTx(txData)
	signed, err := types.SignTx(tx, types.NewCancunSigner(m.chainID), m.privateKey)
	if err != nil {
		return nil, fmt.Errorf("SignTx failed: %w", err)
	}

	return signed, nil
}

// pollReceipt polls TransactionReceipt every DefaultPollInterval until:
//   - Receipt received with success status -> returns confirmed=true
//   - Receipt received with failure status -> returns dropped=true
//   - txTimeout elapses without receipt -> returns pollErr (timeout error)
//   - runCtx is cancelled -> returns pollErr (context error)
func (m *Manager) pollReceipt(
	runCtx context.Context,
	txHash common.Hash,
) (confirmed bool, dropped bool, pollErr error) {
	ctx, cancel := context.WithTimeout(runCtx, m.txTimeout)
	defer cancel()

	ticker := time.NewTicker(DefaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()

		case <-ticker.C:
			receipt, err := m.client.TransactionReceipt(ctx, txHash)
			if err != nil {
				// Not mined yet (ethereum.NotFound) or RPC error; keep polling.
				continue
			}

			if receipt.Status == types.ReceiptStatusSuccessful {
				return true, false, nil
			}

			// Non-success status means reverted on-chain
			return false, true, nil
		}
	}
}

// isAlreadyKnown checks if an error indicates the transaction is already in mempool.
func isAlreadyKnown(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already known")
}

// isNonceTooLow checks if an error indicates nonce is too low.
func isNonceTooLow(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "nonce too low")
}

// isTimeout checks if an error is a context deadline exceeded.
func isTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
