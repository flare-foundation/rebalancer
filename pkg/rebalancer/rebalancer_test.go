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

// mockLimitReporter records limit-reached events for testing.
type mockLimitReporter struct {
	events []limitEvent
}

type limitEvent struct {
	address string
	kind    string
}

func (m *mockLimitReporter) ReportLimitReached(addr common.Address, limitType string) {
	m.events = append(m.events, limitEvent{address: addr.Hex(), kind: limitType})
}

func TestDailyLimitSkipsTopup(t *testing.T) {
	addr := common.HexToAddress("0x1")
	senderAddr := common.HexToAddress("0xSender")
	checker := newMockBalanceChecker()
	// Balance below min triggers funding: topup = 200 - 5 = 195
	checker.balances[addr] = big.NewInt(5)
	checker.balances[senderAddr] = big.NewInt(100000)

	sender := newMockSender()
	reporter := &mockLimitReporter{}

	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)

	r, err := New(sender, checker, Config{
		CheckInterval: 1 * time.Millisecond,
		InitialAddresses: []*TrackedAddress{
			{
				Address:    addr,
				MinBalance: big.NewInt(10),
				TopUpValue: big.NewInt(200),
				DailyLimit: big.NewInt(300), // limit: 300 per day
			},
		},
		LimitReporter: reporter,
	}, nil)
	require.NoError(t, err)
	r.nowFunc = func() time.Time { return now }

	// First check: sends 195, within limit
	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Len(t, sender.sends, 1)
	require.Empty(t, reporter.events)

	// Second check: would send another 195, total 390 > 300 limit
	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Equal(t, int64(195), sender.sends[addr.Hex()]) // no second send
	require.Len(t, reporter.events, 1)
	require.Equal(t, "daily", reporter.events[0].kind)
}

func TestWeeklyLimitSkipsTopup(t *testing.T) {
	addr := common.HexToAddress("0x1")
	senderAddr := common.HexToAddress("0xSender")
	checker := newMockBalanceChecker()
	checker.balances[addr] = big.NewInt(5)
	checker.balances[senderAddr] = big.NewInt(100000)

	sender := newMockSender()
	reporter := &mockLimitReporter{}

	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)

	r, err := New(sender, checker, Config{
		CheckInterval: 1 * time.Millisecond,
		InitialAddresses: []*TrackedAddress{
			{
				Address:     addr,
				MinBalance:  big.NewInt(10),
				TopUpValue:  big.NewInt(200),
				WeeklyLimit: big.NewInt(300),
			},
		},
		LimitReporter: reporter,
	}, nil)
	require.NoError(t, err)
	r.nowFunc = func() time.Time { return now }

	// First check: sends 195
	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Len(t, sender.sends, 1)

	// Second check: 195 + 195 = 390 > 300
	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Equal(t, int64(195), sender.sends[addr.Hex()])
	require.Len(t, reporter.events, 1)
	require.Equal(t, "weekly", reporter.events[0].kind)
}

func TestZeroLimitsAllowUnlimitedTopups(t *testing.T) {
	addr := common.HexToAddress("0x1")
	senderAddr := common.HexToAddress("0xSender")
	checker := newMockBalanceChecker()
	checker.balances[addr] = big.NewInt(5)
	checker.balances[senderAddr] = big.NewInt(100000)

	sender := newMockSender()

	r, err := New(sender, checker, Config{
		CheckInterval: 1 * time.Millisecond,
		InitialAddresses: []*TrackedAddress{
			{
				Address:    addr,
				MinBalance: big.NewInt(10),
				TopUpValue: big.NewInt(200),
				// No limits set (nil)
			},
		},
	}, nil)
	require.NoError(t, err)

	// Multiple topups should all succeed
	for range 5 {
		require.NoError(t, r.checkAndRebalance(context.Background()))
	}
	require.Equal(t, int64(195*5), sender.sends[addr.Hex()])
}

func TestDailyLimitResetsAfterWindow(t *testing.T) {
	addr := common.HexToAddress("0x1")
	senderAddr := common.HexToAddress("0xSender")
	checker := newMockBalanceChecker()
	checker.balances[addr] = big.NewInt(5)
	checker.balances[senderAddr] = big.NewInt(100000)

	sender := newMockSender()
	reporter := &mockLimitReporter{}

	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)

	r, err := New(sender, checker, Config{
		CheckInterval: 1 * time.Millisecond,
		InitialAddresses: []*TrackedAddress{
			{
				Address:    addr,
				MinBalance: big.NewInt(10),
				TopUpValue: big.NewInt(200),
				DailyLimit: big.NewInt(300),
			},
		},
		LimitReporter: reporter,
	}, nil)
	require.NoError(t, err)
	r.nowFunc = func() time.Time { return now }

	// First topup succeeds (195)
	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Equal(t, int64(195), sender.sends[addr.Hex()])

	// Second topup blocked (390 > 300)
	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Equal(t, int64(195), sender.sends[addr.Hex()])

	// Advance 25 hours — daily window resets
	now = now.Add(25 * time.Hour)
	r.nowFunc = func() time.Time { return now }

	require.NoError(t, r.checkAndRebalance(context.Background()))
	require.Equal(t, int64(195*2), sender.sends[addr.Hex()])
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
