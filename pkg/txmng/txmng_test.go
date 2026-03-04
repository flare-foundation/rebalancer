package txmng

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// mockChainClient implements ChainClient for testing.
type mockChainClient struct {
	networkID         *big.Int
	suggestGasPrice   *big.Int
	suggestGasTipCap  *big.Int
	nonce             uint64
	estimateGasErr    error
	estimateGasResult uint64
	sendTxErr         error
	receiptByHash     map[common.Hash]*types.Receipt
	receiptErr        error

	mu           sync.Mutex
	sendTxCalls  int
	sendTxHashes []common.Hash
	receiptCalls int
}

func newMockChainClient() *mockChainClient {
	return &mockChainClient{
		networkID:         big.NewInt(1),
		suggestGasPrice:   big.NewInt(2e9), // 2 Gwei (max fee)
		suggestGasTipCap:  big.NewInt(1e9), // 1 Gwei (priority fee)
		nonce:             0,
		estimateGasResult: 21000,
		receiptByHash:     make(map[common.Hash]*types.Receipt),
	}
}

func (m *mockChainClient) NetworkID(ctx context.Context) (*big.Int, error) {
	return m.networkID, nil
}

func (m *mockChainClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return m.suggestGasPrice, nil
}

func (m *mockChainClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return m.suggestGasTipCap, nil
}

func (m *mockChainClient) NonceAt(ctx context.Context, addr common.Address, blockNumber *big.Int) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nonce, nil
}

func (m *mockChainClient) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	if m.estimateGasErr != nil {
		return 0, m.estimateGasErr
	}
	return m.estimateGasResult, nil
}

func (m *mockChainClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	m.mu.Lock()
	m.sendTxCalls++
	m.sendTxHashes = append(m.sendTxHashes, tx.Hash())
	m.mu.Unlock()

	if m.sendTxErr != nil {
		return m.sendTxErr
	}
	return nil
}

func (m *mockChainClient) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	m.mu.Lock()
	m.receiptCalls++
	m.mu.Unlock()

	if m.receiptErr != nil {
		return nil, m.receiptErr
	}
	if receipt, ok := m.receiptByHash[hash]; ok {
		return receipt, nil
	}
	return nil, ethereum.NotFound
}

func (m *mockChainClient) CodeAt(ctx context.Context, addr common.Address, blockNumber *big.Int) ([]byte, error) {
	return nil, nil
}

func (m *mockChainClient) setReceipt(hash common.Hash, status uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receiptByHash[hash] = &types.Receipt{
		Status:      status,
		TxHash:      hash,
		BlockNumber: big.NewInt(1),
	}
}

func (m *mockChainClient) getSendTxCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendTxCalls
}

func (m *mockChainClient) getLastSentHash() common.Hash {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sendTxHashes) > 0 {
		return m.sendTxHashes[len(m.sendTxHashes)-1]
	}
	return common.Hash{}
}

// testPrivateKey generates a test private key.
func testPrivateKey() *ecdsa.PrivateKey {
	key, _ := crypto.GenerateKey()
	return key
}

// TestNew tests the New constructor.
func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		privateKey *ecdsa.PrivateKey
		client     ChainClient
		cfg        Config
		expectErr  bool
		expectAddr common.Address
	}{
		{
			name:       "valid inputs with defaults",
			privateKey: testPrivateKey(),
			client:     newMockChainClient(),
			cfg:        Config{},
			expectErr:  false,
		},
		{
			name:       "custom timeout and retries",
			privateKey: testPrivateKey(),
			client:     newMockChainClient(),
			cfg: Config{
				TxTimeout:  20 * time.Second,
				MaxRetries: 5,
			},
			expectErr: false,
		},
		{
			name:       "nil private key",
			privateKey: nil,
			client:     newMockChainClient(),
			cfg:        Config{},
			expectErr:  true,
		},
		{
			name:       "nil client",
			privateKey: testPrivateKey(),
			client:     nil,
			cfg:        Config{},
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := New(tt.privateKey, tt.client, tt.cfg, nil)
			if tt.expectErr {
				require.Error(t, err)
				require.Nil(t, mgr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, mgr)
				require.Equal(t, crypto.PubkeyToAddress(tt.privateKey.PublicKey), mgr.Address())
			}
		})
	}
}

// TestAddress tests the Address method.
func TestAddress(t *testing.T) {
	pk := testPrivateKey()
	expectedAddr := crypto.PubkeyToAddress(pk.PublicKey)

	mgr, err := New(pk, newMockChainClient(), Config{}, nil)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, mgr.Address())
}

// TestSendHappyPath tests a successful send with immediate receipt.
func TestSendHappyPath(t *testing.T) {
	pk := testPrivateKey()
	mock := newMockChainClient()
	mgr, err := New(pk, mock, Config{TxTimeout: 5 * time.Second}, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Run in a goroutine
	go func() {
		runErr := mgr.Run(ctx)
		require.Equal(t, context.Canceled, runErr)
	}()

	// Allow Run to start
	time.Sleep(50 * time.Millisecond)

	// Set up mock to return successful receipt on second call (first call returns NotFound)
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	amount := big.NewInt(1000)

	// We'll inject the receipt after the tx is sent
	go func() {
		time.Sleep(100 * time.Millisecond)
		lastHash := mock.getLastSentHash()
		if lastHash != (common.Hash{}) {
			mock.setReceipt(lastHash, types.ReceiptStatusSuccessful)
		}
	}()

	err = mgr.Send(context.Background(), to, amount)
	require.NoError(t, err)

	cancel()
}

// TestSendEstimateGasError tests that EstimateGas error causes tx to be dropped.
func TestSendEstimateGasError(t *testing.T) {
	pk := testPrivateKey()
	mock := newMockChainClient()
	mock.estimateGasErr = fmt.Errorf("execution reverted")

	mgr, err := New(pk, mock, Config{TxTimeout: 5 * time.Second}, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		runErr := mgr.Run(ctx)
		require.Equal(t, context.Canceled, runErr)
	}()

	time.Sleep(50 * time.Millisecond)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	err = mgr.Send(context.Background(), to, big.NewInt(1000))
	require.Error(t, err)
	require.Contains(t, err.Error(), "dry run failed")

	cancel()
}

// TestSendAlreadyKnown tests that "already known" is treated as success.
func TestSendAlreadyKnown(t *testing.T) {
	pk := testPrivateKey()
	mock := newMockChainClient()
	mock.sendTxErr = fmt.Errorf("already known")

	mgr, err := New(pk, mock, Config{TxTimeout: 5 * time.Second}, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		runErr := mgr.Run(ctx)
		require.Equal(t, context.Canceled, runErr)
	}()

	time.Sleep(50 * time.Millisecond)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	err = mgr.Send(context.Background(), to, big.NewInt(1000))
	require.NoError(t, err)

	cancel()
}

// TestSendContextCancelled tests that cancelled context is handled.
func TestSendContextCancelled(t *testing.T) {
	pk := testPrivateKey()
	mock := newMockChainClient()

	mgr, err := New(pk, mock, Config{TxTimeout: 5 * time.Second}, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before sending

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	err = mgr.Send(ctx, to, big.NewInt(1000))
	require.Error(t, err)
	require.Contains(t, err.Error(), "context")
}

// TestMultipleSends tests that multiple sends are processed sequentially.
func TestMultipleSends(t *testing.T) {
	pk := testPrivateKey()
	mock := newMockChainClient()

	mgr, err := New(pk, mock, Config{TxTimeout: 2 * time.Second, MaxRetries: 1}, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Run
	runDone := make(chan error, 1)
	go func() {
		runDone <- mgr.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Set up a goroutine to inject receipts as they're sent
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			lastHash := mock.getLastSentHash()
			if lastHash != (common.Hash{}) {
				mock.setReceipt(lastHash, types.ReceiptStatusSuccessful)
			}
		}
	}()

	// Send multiple transactions with enough time between them
	for i := range 2 {
		to := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		err := mgr.Send(context.Background(), to, big.NewInt(int64(i+1)))
		require.NoError(t, err)
	}

	// Verify all were sent
	require.Greater(t, mock.getSendTxCalls(), 0)

	cancel()
	<-runDone
}

// TestBuildGasParams tests gas param computation.
func TestBuildGasParams(t *testing.T) {
	mock := newMockChainClient()
	mock.suggestGasTipCap = big.NewInt(1e9) // 1 Gwei
	mock.suggestGasPrice = big.NewInt(2e9)  // 2 Gwei

	tipCap, feeCap, err := buildGasParams(context.Background(), mock)
	require.NoError(t, err)

	// TipCap = SuggestGasTipCap = 1e9
	require.Equal(t, big.NewInt(1e9), tipCap)

	// FeeCap = SuggestGasPrice = 2e9
	require.Equal(t, big.NewInt(2e9), feeCap)
}

// TestBumpGasParams tests gas bumping.
func TestBumpGasParams(t *testing.T) {
	mock := newMockChainClient()
	mock.suggestGasTipCap = big.NewInt(1e9) // 1 Gwei
	mock.suggestGasPrice = big.NewInt(2e9)  // 2 Gwei

	prevTip := big.NewInt(1e9) // 1 Gwei
	prevFee := big.NewInt(2e9) // 2 Gwei

	newTip, newFee, err := bumpGasParams(context.Background(), mock, prevTip, prevFee)
	require.NoError(t, err)

	// newTip should be more than 10% higher: 1e9 * 1.1 + 1 = 1.1e9 + 1
	// max(1e9, 1.1e9 + 1) = 1.1e9 + 1
	require.True(t, newTip.Cmp(big.NewInt(1100000001)) >= 0)

	// newFee should be more than 10% higher: 2e9 * 1.1 + 1 = 2.2e9 + 1
	// max(2e9, 2.2e9 + 1) = 2.2e9 + 1
	require.True(t, newFee.Cmp(big.NewInt(2200000001)) >= 0)
}

// TestEstimateGasLimit tests gas limit estimation.
func TestEstimateGasLimit(t *testing.T) {
	mock := newMockChainClient()
	mock.estimateGasResult = 21000

	limit, err := estimateGasLimit(
		context.Background(),
		mock,
		common.HexToAddress("0x1111"),
		common.HexToAddress("0x2222"),
		big.NewInt(1000),
		nil,
	)
	require.NoError(t, err)

	// Expected: 21000 * 15 / 10 = 31500
	require.Equal(t, uint64(31500), limit)
}

// TestEstimateGasLimitWithError tests that EstimateGas error is propagated.
func TestEstimateGasLimitWithError(t *testing.T) {
	mock := newMockChainClient()
	mock.estimateGasErr = fmt.Errorf("execution reverted")

	limit, err := estimateGasLimit(
		context.Background(),
		mock,
		common.HexToAddress("0x1111"),
		common.HexToAddress("0x2222"),
		big.NewInt(1000),
		nil,
	)
	require.Error(t, err)
	require.Equal(t, uint64(0), limit)
	require.Contains(t, err.Error(), "EstimateGas failed")
}

// TestIsAlreadyKnown tests error classification.
func TestIsAlreadyKnown(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"contains already known", fmt.Errorf("already known"), true},
		{"contains in message", fmt.Errorf("tx is already known"), true},
		{"does not contain", fmt.Errorf("some other error"), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.err != nil {
				err = tt.err
			}
			result := isAlreadyKnown(err)
			require.Equal(t, tt.expect, result)
		})
	}
}

// TestIsNonceTooLow tests error classification.
func TestIsNonceTooLow(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"contains nonce too low", fmt.Errorf("nonce too low"), true},
		{"contains in message", fmt.Errorf("tx has nonce too low"), true},
		{"does not contain", fmt.Errorf("some other error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNonceTooLow(tt.err)
			require.Equal(t, tt.expect, result)
		})
	}
}

// TestIsTimeout tests error classification.
func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"other error", fmt.Errorf("some error"), false},
		{"wrapped timeout", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTimeout(tt.err)
			require.Equal(t, tt.expect, result)
		})
	}
}
