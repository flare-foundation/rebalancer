package rebalancer

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeiToNativeTokenFloat_200FLR(t *testing.T) {
	t.Parallel()
	w, ok := new(big.Int).SetString("200000000000000000000", 10)
	require.True(t, ok)
	require.InDelta(t, 200.0, weiToNativeTokenFloat(w), 1e-9)
}

func TestWeiToNativeTokenFloat_largeWeiNotExactAsFloat64Raw(t *testing.T) {
	t.Parallel()
	w, ok := new(big.Int).SetString("200000000000000000000", 10)
	require.True(t, ok)
	raw, _ := new(big.Float).SetInt(w).Float64()
	require.NotEqual(t, 200.0, raw, "raw wei in float64 is not exact token units")
}
