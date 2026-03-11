package rebalancer

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAmountInWindow(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)

	ta := &TrackedAddress{
		FundingHistory: []FundingRecord{
			{Amount: big.NewInt(100), Time: now.Add(-25 * time.Hour)}, // outside 24h
			{Amount: big.NewInt(200), Time: now.Add(-23 * time.Hour)}, // inside 24h
			{Amount: big.NewInt(300), Time: now.Add(-1 * time.Hour)},  // inside 24h
		},
	}

	daily := ta.AmountInWindow(24*time.Hour, now)
	require.Equal(t, 0, daily.Cmp(big.NewInt(500)))

	weekly := ta.AmountInWindow(7*24*time.Hour, now)
	require.Equal(t, 0, weekly.Cmp(big.NewInt(600)))

	// Empty history
	empty := &TrackedAddress{}
	require.Equal(t, 0, empty.AmountInWindow(24*time.Hour, now).Cmp(big.NewInt(0)))
}

func TestPruneFundingHistory(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)

	ta := &TrackedAddress{
		FundingHistory: []FundingRecord{
			{Amount: big.NewInt(100), Time: now.Add(-8 * 24 * time.Hour)}, // older than 7d
			{Amount: big.NewInt(200), Time: now.Add(-6 * 24 * time.Hour)}, // within 7d
			{Amount: big.NewInt(300), Time: now.Add(-1 * time.Hour)},      // within 7d
		},
	}

	ta.PruneFundingHistory(now)
	require.Len(t, ta.FundingHistory, 2)
	require.Equal(t, 0, ta.FundingHistory[0].Amount.Cmp(big.NewInt(200)))
	require.Equal(t, 0, ta.FundingHistory[1].Amount.Cmp(big.NewInt(300)))
}
