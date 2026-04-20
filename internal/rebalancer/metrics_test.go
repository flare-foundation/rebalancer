package rebalancer

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeiToNativeTokenFloat(t *testing.T) {
	t.Parallel()

	mustBig := func(s string) *big.Int {
		x, ok := new(big.Int).SetString(s, 10)
		require.True(t, ok, "parse %q", s)
		return x
	}

	tests := []struct {
		name string
		wei  *big.Int
		want float64
	}{
		{"nil", nil, 0},
		{"zero", big.NewInt(0), 0},
		{"oneWei", big.NewInt(1), 1e-18},
		{"fortyTwoWei", big.NewInt(42), 42e-18},
		{"arbitrarySubGwei", big.NewInt(938_271), 9.38271e-13},
		{"oneGwei", big.NewInt(1_000_000_000), 1e-9},
		{"arbitraryGwei", big.NewInt(6_419_283_705), 6.419283705e-9},
		{"pointOneFLR", new(big.Int).SetUint64(100_000_000_000_000_000), 0.1},
		{"halfFLR", new(big.Int).SetUint64(500_000_000_000_000_000), 0.5},
		{"arbitrarySubFLR", mustBig("384729105638472910"), 0.384729105638472910},
		{"oneFLR", new(big.Int).SetUint64(1_000_000_000_000_000_000), 1.0},
		{"arbitraryFLR", mustBig("2719384650192837465"), 2.719384650192837465},
		{"arbitraryTensFLR", mustBig("47583920164738291056"), 47.583920164738291056},
		{"twoHundredFLR", mustBig("200000000000000000000"), 200.0},
		{"arbitraryHundredsFLR", mustBig("836291047582930165472"), 836.291047582930165472},
		{"arbitraryThousandsFLR", mustBig("5174839201647382910564"), 5174.839201647382910564},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.InDelta(t, tc.want, weiToNativeTokenFloat(tc.wei), 1e-9)
		})
	}
}
