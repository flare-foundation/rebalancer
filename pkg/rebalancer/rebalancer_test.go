package rebalancer

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// mockSender is a mock implementation of the Sender interface for testing.
type mockSender struct {
	address common.Address
	sends   map[string]int64 // address -> total amount sent
	err     error
}

func (m *mockSender) Send(ctx context.Context, to common.Address, amount *big.Int) error {
	if m.err != nil {
		return m.err
	}
	key := to.Hex()
	m.sends[key] += amount.Int64()
	return nil
}

func (m *mockSender) Address() common.Address {
	return m.address
}

func newMockSender() *mockSender {
	return &mockSender{
		address: common.HexToAddress("0xSender"),
		sends:   make(map[string]int64),
	}
}

// mockBalanceChecker is a mock implementation of the BalanceChecker interface for testing.
type mockBalanceChecker struct {
	balances map[common.Address]*big.Int
	err      error
}

func (m *mockBalanceChecker) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	balance, ok := m.balances[account]
	if !ok {
		return big.NewInt(0), nil
	}
	return new(big.Int).Set(balance), nil
}

func newMockBalanceChecker() *mockBalanceChecker {
	return &mockBalanceChecker{
		balances: make(map[common.Address]*big.Int),
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		sender    Sender
		cfg       Config
		expectErr bool
	}{
		{
			name:   "valid config",
			sender: newMockSender(),
			cfg: Config{
				CheckInterval: 5 * time.Minute,
			},
			expectErr: false,
		},
		{
			name:      "no sender",
			sender:    nil,
			expectErr: true,
		},
		{
			name:   "default check interval",
			sender: newMockSender(),
			cfg: Config{
				CheckInterval: 0, // Should use default
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(tt.sender, nil, tt.cfg, nil)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, r)
				require.NotZero(t, r.checkInterval)
			}
		})
	}
}

func TestAddAddress(t *testing.T) {
	cfg := Config{
		CheckInterval: 1 * time.Millisecond,
	}
	r, err := New(newMockSender(), nil, cfg, nil)
	require.NoError(t, err)

	tests := []struct {
		name      string
		ta        *TrackedAddress
		expectErr bool
	}{
		{
			name: "valid address",
			ta: &TrackedAddress{
				Address:    common.HexToAddress("0x1"),
				MinBalance: DefaultMinBalanceWei(),
				TopUpValue: DefaultTopUpValueWei(),
			},
			expectErr: false,
		},
		{
			name:      "nil tracked address",
			ta:        nil,
			expectErr: true,
		},
		{
			name: "nil min balance",
			ta: &TrackedAddress{
				Address:    common.HexToAddress("0x2"),
				MinBalance: nil,
				TopUpValue: DefaultTopUpValueWei(),
			},
			expectErr: true,
		},
		{
			name: "nil top up value",
			ta: &TrackedAddress{
				Address:    common.HexToAddress("0x3"),
				MinBalance: DefaultMinBalanceWei(),
				TopUpValue: nil,
			},
			expectErr: true,
		},
		{
			name: "zero min balance",
			ta: &TrackedAddress{
				Address:    common.HexToAddress("0x4"),
				MinBalance: big.NewInt(0),
				TopUpValue: DefaultTopUpValueWei(),
			},
			expectErr: true,
		},
		{
			name: "top up less than min",
			ta: &TrackedAddress{
				Address:    common.HexToAddress("0x5"),
				MinBalance: DefaultTopUpValueWei(),
				TopUpValue: DefaultMinBalanceWei(),
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.AddAddress(tt.ta)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRemoveAddress(t *testing.T) {
	cfg := Config{
		CheckInterval: 1 * time.Millisecond,
	}
	r, err := New(newMockSender(), nil, cfg, nil)
	require.NoError(t, err)

	addr := common.HexToAddress("0x1")
	ta := &TrackedAddress{
		Address:    addr,
		MinBalance: DefaultMinBalanceWei(),
		TopUpValue: DefaultTopUpValueWei(),
	}
	require.NoError(t, r.AddAddress(ta))

	// Remove existing address
	require.NoError(t, r.RemoveAddress(addr))

	// Try to remove non-existent address
	require.Error(t, r.RemoveAddress(addr))
}

func TestGetTrackedAddresses(t *testing.T) {
	cfg := Config{
		CheckInterval: 1 * time.Millisecond,
	}
	r, err := New(newMockSender(), nil, cfg, nil)
	require.NoError(t, err)

	addr1 := common.HexToAddress("0x1")
	addr2 := common.HexToAddress("0x2")

	ta1 := &TrackedAddress{
		Address:    addr1,
		MinBalance: DefaultMinBalanceWei(),
		TopUpValue: DefaultTopUpValueWei(),
	}
	ta2 := &TrackedAddress{
		Address:    addr2,
		MinBalance: DefaultMinBalanceWei(),
		TopUpValue: DefaultTopUpValueWei(),
	}

	require.NoError(t, r.AddAddress(ta1))
	require.NoError(t, r.AddAddress(ta2))

	tracked := r.GetTrackedAddresses()
	require.Len(t, tracked, 2)
	require.Contains(t, tracked, addr1)
	require.Contains(t, tracked, addr2)
}

func TestGetMetrics(t *testing.T) {
	cfg := Config{
		CheckInterval: 1 * time.Millisecond,
	}
	r, err := New(newMockSender(), nil, cfg, nil)
	require.NoError(t, err)

	metrics := r.GetMetrics()

	require.Equal(t, uint64(0), metrics.TotalChecks)
	require.Equal(t, uint64(0), metrics.TotalFundings)
	require.Equal(t, 0, metrics.TotalAmountSent.Cmp(big.NewInt(0)))
}

func TestStop(t *testing.T) {
	cfg := Config{
		CheckInterval: 100 * time.Millisecond,
	}
	r, err := New(newMockSender(), newMockBalanceChecker(), cfg, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	// Run in a goroutine
	done := make(chan error)
	go func() {
		done <- r.Run(ctx)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop the rebalancer
	stopCtx, stopCancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer stopCancel()
	require.NoError(t, r.Stop(stopCtx))

	// Verify it actually stopped
	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(1 * time.Second):
		require.Fail(t, "rebalancer did not stop in time")
	}
}

func TestContextCancellation(t *testing.T) {
	cfg := Config{
		CheckInterval: 100 * time.Millisecond,
	}
	r, err := New(newMockSender(), newMockBalanceChecker(), cfg, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error)
	go func() {
		done <- r.Run(ctx)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	select {
	case err := <-done:
		require.Equal(t, context.Canceled, err)
	case <-time.After(1 * time.Second):
		require.Fail(t, "rebalancer did not exit on context cancellation")
	}
}

func TestFLRToWei(t *testing.T) {
	tests := []struct {
		flr      int64
		expected *big.Int
	}{
		{1, new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18))},
		{20, new(big.Int).Mul(big.NewInt(20), big.NewInt(1e18))},
		{200, new(big.Int).Mul(big.NewInt(200), big.NewInt(1e18))},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%dFLR", tt.flr), func(t *testing.T) {
			result := FLRToWei(tt.flr)
			require.Equal(t, 0, result.Cmp(tt.expected))
		})
	}
}

func TestDefaultBalances(t *testing.T) {
	minBal := DefaultMinBalanceWei()
	topUpVal := DefaultTopUpValueWei()
	warnBal := DefaultWarningBalanceWei()

	require.Equal(t, 0, minBal.Cmp(FLRToWei(DefaultMinBalance)))
	require.Equal(t, 0, topUpVal.Cmp(FLRToWei(DefaultTopUpValue)))
	require.Equal(t, 0, warnBal.Cmp(FLRToWei(DefaultWarningBalance)))
	require.GreaterOrEqual(t, topUpVal.Cmp(minBal), 0)
}
