package txmng

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// buildGasParams queries SuggestGasTipCap and SuggestGasPrice to construct
// gas parameters for EIP-1559 transactions.
//
// gasTipCap is obtained directly from SuggestGasTipCap (max priority fee per gas).
// gasFeeCap is obtained directly from SuggestGasPrice (max fee per gas on Flare/Avalanche,
// includes base fee).
func buildGasParams(
	ctx context.Context,
	client ChainClient,
) (gasTipCap *big.Int, gasFeeCap *big.Int, err error) {
	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("SuggestGasTipCap failed: %w", err)
	}

	feeCap, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("SuggestGasPrice failed: %w", err)
	}

	return tipCap, feeCap, nil
}

// bumpGasParams returns new tip and fee caps that are more than 10% higher than
// the provided previous values. It takes the maximum of the bumped previous
// values and freshly computed values to ensure both the replacement rule
// (>10% bump) and freshness are satisfied.
func bumpGasParams(
	ctx context.Context,
	client ChainClient,
	prevTipCap *big.Int,
	prevFeeCap *big.Int,
) (gasTipCap *big.Int, gasFeeCap *big.Int, err error) {
	// Compute fresh values
	freshTip, freshFee, err := buildGasParams(ctx, client)
	if err != nil {
		return nil, nil, fmt.Errorf("buildGasParams failed: %w", err)
	}

	// Compute bumped-previous: multiply by 110/100 (10% increase) and add 1 to ensure > 10%
	bumpedTip := new(big.Int).Mul(prevTipCap, big.NewInt(int64(RetryGasBumpNumerator)))
	bumpedTip.Div(bumpedTip, big.NewInt(int64(RetryGasBumpDivisor)))
	bumpedTip.Add(bumpedTip, big.NewInt(1))

	bumpedFee := new(big.Int).Mul(prevFeeCap, big.NewInt(int64(RetryGasBumpNumerator)))
	bumpedFee.Div(bumpedFee, big.NewInt(int64(RetryGasBumpDivisor)))
	bumpedFee.Add(bumpedFee, big.NewInt(1))

	// Take maximum of fresh and bumped
	if freshTip.Cmp(bumpedTip) < 0 {
		gasTipCap = bumpedTip
	} else {
		gasTipCap = freshTip
	}

	if freshFee.Cmp(bumpedFee) < 0 {
		gasFeeCap = bumpedFee
	} else {
		gasFeeCap = freshFee
	}

	return gasTipCap, gasFeeCap, nil
}

// estimateGasLimit calls EstimateGas and multiplies the result by 1.5x
// (GasLimitNumerator/GasLimitDivisor) to get an actual gas limit.
// Returns an error if the dry-run reverts (EstimateGas returns an error
// containing revert information).
func estimateGasLimit(
	ctx context.Context,
	client ChainClient,
	from common.Address,
	to common.Address,
	amount *big.Int,
	input []byte,
) (uint64, error) {
	msg := ethereum.CallMsg{
		From:  from,
		To:    &to,
		Value: amount,
		Data:  input,
	}
	estimated, err := client.EstimateGas(ctx, msg)
	if err != nil {
		return 0, fmt.Errorf("EstimateGas failed (tx will revert): %w", err)
	}

	// Multiply by 1.5x: (estimated * 15) / 10
	limit := estimated * uint64(GasLimitNumerator) / uint64(GasLimitDivisor)
	return limit, nil
}
