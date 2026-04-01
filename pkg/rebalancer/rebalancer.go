package rebalancer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Rebalancer manages funding of tracked addresses by periodically checking their balances.
type Rebalancer struct {
	balanceChecker BalanceChecker
	sender         Sender
	logger         Logger
	limitReporter  LimitReporter
	checkInterval  time.Duration
	warningBalance *big.Int
	nowFunc        func() time.Time

	mu        sync.RWMutex
	addresses map[common.Address]*TrackedAddress
	metrics   RebalancerMetrics
	stopChan  chan struct{}
	stoppedCh chan struct{}
}

// New creates a new Rebalancer instance.
func New(sender Sender, balanceChecker BalanceChecker, cfg Config, logger Logger) (*Rebalancer, error) {
	if sender == nil {
		return nil, errors.New("sender is required")
	}
	if logger == nil {
		logger = &NoOpLogger{}
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = DefaultCheckInterval
	}
	if cfg.WarningBalance == nil {
		cfg.WarningBalance = DefaultWarningBalanceWei()
	}

	r := &Rebalancer{
		balanceChecker: balanceChecker,
		sender:         sender,
		logger:         logger,
		limitReporter:  cfg.LimitReporter,
		checkInterval:  cfg.CheckInterval,
		warningBalance: cfg.WarningBalance,
		nowFunc:        time.Now,
		addresses:      make(map[common.Address]*TrackedAddress),
		metrics: RebalancerMetrics{
			TotalAmountSent: big.NewInt(0),
		},
		stopChan:  make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}

	// Add initial addresses if provided
	if cfg.InitialAddresses != nil {
		for _, ta := range cfg.InitialAddresses {
			if ta != nil {
				if err := r.addAddressInternal(ta); err != nil {
					return nil, fmt.Errorf("failed to add initial address: %w", err)
				}
			}
		}
	}

	return r, nil
}

// AddAddress adds a new address to be tracked for rebalancing.
func (r *Rebalancer) AddAddress(ta *TrackedAddress) error {
	if ta == nil {
		return errors.New("tracked address must not be nil")
	}
	if ta.MinBalance == nil || ta.TopUpValue == nil {
		return errors.New("min balance and top up value must not be nil")
	}
	if ta.MinBalance.Cmp(big.NewInt(0)) <= 0 || ta.TopUpValue.Cmp(big.NewInt(0)) <= 0 {
		return errors.New("min balance and top up value must be greater than 0")
	}
	if ta.TopUpValue.Cmp(ta.MinBalance) < 0 {
		return errors.New("top up value must be greater than or equal to min balance")
	}

	return r.addAddressInternal(ta)
}

// addAddressInternal adds an address without validation (assumes already validated).
func (r *Rebalancer) addAddressInternal(ta *TrackedAddress) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.addresses[ta.Address] = ta

	r.logger.Infof("added tracked address %s with min balance %s, top up value %s",
		ta.Address.Hex(), ta.MinBalance.String(), ta.TopUpValue.String())

	return nil
}

// RemoveAddress stops tracking an address for rebalancing.
func (r *Rebalancer) RemoveAddress(addr common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.addresses[addr]; !exists {
		return fmt.Errorf("address %s is not tracked", addr.Hex())
	}

	delete(r.addresses, addr)
	r.logger.Infof("removed tracked address %s", addr.Hex())

	return nil
}

// GetTrackedAddresses returns a copy of all currently tracked addresses.
func (r *Rebalancer) GetTrackedAddresses() map[common.Address]*TrackedAddress {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[common.Address]*TrackedAddress)
	maps.Copy(result, r.addresses)
	return result
}

// GetMetrics returns a copy of the current metrics.
func (r *Rebalancer) GetMetrics() RebalancerMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RebalancerMetrics{
		TotalChecks:     r.metrics.TotalChecks,
		TotalFundings:   r.metrics.TotalFundings,
		TotalAmountSent: new(big.Int).Set(r.metrics.TotalAmountSent),
		LastCheckTime:   r.metrics.LastCheckTime,
		LastFundTime:    r.metrics.LastFundTime,
	}
}

// Run starts the periodic balance checking loop. It blocks until the context is cancelled
// or Stop is called.
func (r *Rebalancer) Run(ctx context.Context) error {
	if r.balanceChecker == nil {
		return errors.New("balance checker is required to run rebalancer")
	}

	r.logger.Infof("rebalancer started with check interval %v", r.checkInterval)

	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("rebalancer context cancelled")
			close(r.stoppedCh)
			return ctx.Err()

		case <-r.stopChan:
			r.logger.Info("rebalancer stopped")
			close(r.stoppedCh)
			return nil

		case <-ticker.C:
			if err := r.checkAndRebalance(ctx); err != nil {
				r.logger.Errorf("error during rebalance check: %v", err)
			}
		}
	}
}

// Stop signals the rebalancer to stop and waits for it to finish.
func (r *Rebalancer) Stop(ctx context.Context) error {
	close(r.stopChan)

	select {
	case <-r.stoppedCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// checkAndRebalance checks all tracked addresses and funds them if needed.
func (r *Rebalancer) checkAndRebalance(ctx context.Context) error {
	r.mu.RLock()
	addresses := make([]*TrackedAddress, 0, len(r.addresses))
	for _, tracked := range r.addresses {
		addresses = append(addresses, tracked)
	}
	r.mu.RUnlock()

	if len(addresses) == 0 {
		return nil
	}

	r.logger.Debugf("checking balance of %d tracked addresses", len(addresses))

	results := r.checkBalances(ctx, addresses)

	// Check sender's balance
	senderAddr := r.sender.Address()
	senderBalance, err := r.balanceChecker.BalanceAt(ctx, senderAddr, nil)
	if err != nil {
		r.logger.Warnf("failed to check sender balance for %s: %v", senderAddr.Hex(), err)
	} else if senderBalance.Cmp(r.warningBalance) < 0 {
		r.logger.Warnf("sender balance for %s is low: %s (warning threshold: %s)",
			senderAddr.Hex(), senderBalance.String(), r.warningBalance.String())
	}

	// Calculate cumulative amount to send
	totalToSend := big.NewInt(0)
	for _, result := range results {
		if result.Error == nil {
			r.mu.RLock()
			tracked, ok := r.addresses[result.Address]
			r.mu.RUnlock()

			if ok && result.Balance.Cmp(tracked.MinBalance) < 0 {
				amountToSend := new(big.Int).Sub(tracked.TopUpValue, result.Balance)
				totalToSend = new(big.Int).Add(totalToSend, amountToSend)
			}
		}
	}

	// Check if sender has enough balance for all required transfers
	if senderBalance != nil && totalToSend.Cmp(big.NewInt(0)) > 0 && senderBalance.Cmp(totalToSend) < 0 {
		return fmt.Errorf("insufficient sender balance: have %s, need %s",
			senderBalance.String(), totalToSend.String())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().Unix()
	r.metrics.TotalChecks++
	r.metrics.LastCheckTime = now

	for _, result := range results {
		if result.Error != nil {
			r.logger.Warnf("failed to check balance for %s: %v", result.Address.Hex(), result.Error)
			continue
		}

		tracked, ok := r.addresses[result.Address]
		if !ok {
			continue
		}

		tracked.LastCheckAt = now

		if result.NeedsFunds {
			amountToSend := new(big.Int).Sub(tracked.TopUpValue, result.Balance)
			nowTime := r.nowFunc()

			if r.exceedsLimit(tracked, amountToSend, nowTime) {
				continue
			}

			if err := r.sender.Send(ctx, result.Address, amountToSend); err != nil {
				r.logger.Errorf("failed to fund address %s with %s: %v",
					result.Address.Hex(), amountToSend.String(), err)
				continue
			}

			tracked.FundingHistory = append(tracked.FundingHistory, FundingRecord{
				Amount: new(big.Int).Set(amountToSend),
				Time:   nowTime,
			})
			tracked.PruneFundingHistory(nowTime)

			tracked.LastFundedAt = now
			r.metrics.TotalFundings++
			r.metrics.LastFundTime = now
			r.metrics.TotalAmountSent = new(big.Int).Add(r.metrics.TotalAmountSent, amountToSend)

			r.logger.Infof("funded address %s with %s, new balance will be %s",
				result.Address.Hex(), amountToSend.String(), tracked.TopUpValue.String())
		} else {
			r.logger.Debugf("address %s has sufficient balance %s (min %s)",
				result.Address.Hex(), result.Balance.String(), tracked.MinBalance.String())
		}
	}

	return nil
}

// checkBalances checks the balance of multiple addresses in parallel.
func (r *Rebalancer) checkBalances(ctx context.Context, addresses []*TrackedAddress) []BalanceCheckResult {
	results := make([]BalanceCheckResult, len(addresses))
	var wg sync.WaitGroup

	for i, tracked := range addresses {
		wg.Add(1)
		go func(idx int, addr *TrackedAddress) {
			defer wg.Done()

			balance, err := r.balanceChecker.BalanceAt(ctx, addr.Address, nil)
			if err != nil {
				results[idx] = BalanceCheckResult{
					Address: addr.Address,
					Error:   err,
				}
				return
			}

			needsFunds := balance.Cmp(addr.MinBalance) < 0

			results[idx] = BalanceCheckResult{
				Address:    addr.Address,
				Balance:    balance,
				NeedsFunds: needsFunds,
			}
		}(i, tracked)
	}

	wg.Wait()
	return results
}

// exceedsLimit checks if funding the address would exceed its daily or weekly limit.
// Returns true if the topup should be skipped. Caller must hold r.mu.
func (r *Rebalancer) exceedsLimit(tracked *TrackedAddress, amount *big.Int, now time.Time) bool {
	if tracked.DailyLimit != nil && tracked.DailyLimit.Sign() > 0 {
		spent := tracked.AmountInWindow(24*time.Hour, now)
		projected := new(big.Int).Add(spent, amount)
		if projected.Cmp(tracked.DailyLimit) > 0 {
			r.logger.Warnf("daily topup limit reached for %s (spent %s, want %s, limit %s)",
				tracked.Address.Hex(), spent.String(), amount.String(), tracked.DailyLimit.String())
			if r.limitReporter != nil {
				r.limitReporter.ReportLimitReached(tracked.Address, "daily")
			}
			return true
		}
	}

	if tracked.WeeklyLimit != nil && tracked.WeeklyLimit.Sign() > 0 {
		spent := tracked.AmountInWindow(7*24*time.Hour, now)
		projected := new(big.Int).Add(spent, amount)
		if projected.Cmp(tracked.WeeklyLimit) > 0 {
			r.logger.Warnf("weekly topup limit reached for %s (spent %s, want %s, limit %s)",
				tracked.Address.Hex(), spent.String(), amount.String(), tracked.WeeklyLimit.String())
			if r.limitReporter != nil {
				r.limitReporter.ReportLimitReached(tracked.Address, "weekly")
			}
			return true
		}
	}

	return false
}
