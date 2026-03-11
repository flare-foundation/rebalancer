package rebalancer

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// newBigInt creates a *big.Int from a decimal string.
func newBigInt(s string) *big.Int {
	b, _ := new(big.Int).SetString(s, 10)
	return b
}

func TestTrackedAddressConfigUnmarshalTOML(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		want    *TrackedAddressConfig
		wantErr bool
	}{
		{
			name: "parse int64 values",
			data: map[string]any{
				"address":          "0x1234567890123456789012345678901234567890",
				"min_balance_wei":  int64(100),
				"top_up_value_wei": int64(200),
			},
			want: &TrackedAddressConfig{
				Address:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
				MinBalance: big.NewInt(100),
				TopUpValue: big.NewInt(200),
			},
		},
		{
			name: "parse string values",
			data: map[string]any{
				"address":          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
				"min_balance_wei":  "1000000000000000000",
				"top_up_value_wei": "2000000000000000000",
			},
			want: &TrackedAddressConfig{
				Address:    common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
				MinBalance: newBigInt("1000000000000000000"),
				TopUpValue: newBigInt("2000000000000000000"),
			},
		},
		{
			name: "nil balances with zero int64 values",
			data: map[string]any{
				"address":          "0x1111111111111111111111111111111111111111",
				"min_balance_wei":  int64(0),
				"top_up_value_wei": int64(0),
			},
			want: &TrackedAddressConfig{
				Address:    common.HexToAddress("0x1111111111111111111111111111111111111111"),
				MinBalance: nil,
				TopUpValue: nil,
			},
		},
		{
			name: "nil balances with empty string values",
			data: map[string]any{
				"address":          "0x2222222222222222222222222222222222222222",
				"min_balance_wei":  "",
				"top_up_value_wei": "",
			},
			want: &TrackedAddressConfig{
				Address:    common.HexToAddress("0x2222222222222222222222222222222222222222"),
				MinBalance: nil,
				TopUpValue: nil,
			},
		},
		{
			name: "missing balance fields",
			data: map[string]any{
				"address": "0x3333333333333333333333333333333333333333",
			},
			want: &TrackedAddressConfig{
				Address:    common.HexToAddress("0x3333333333333333333333333333333333333333"),
				MinBalance: nil,
				TopUpValue: nil,
			},
		},
		{
			name: "large wei values as strings",
			data: map[string]any{
				"address":          "0x4444444444444444444444444444444444444444",
				"min_balance_wei":  "20000000000000000000",
				"top_up_value_wei": "200000000000000000000",
			},
			want: &TrackedAddressConfig{
				Address:    common.HexToAddress("0x4444444444444444444444444444444444444444"),
				MinBalance: newBigInt("20000000000000000000"),
				TopUpValue: newBigInt("200000000000000000000"),
			},
		},
		{
			name: "parse limit int64 values",
			data: map[string]any{
				"address":          "0x5555555555555555555555555555555555555555",
				"min_balance_wei":  int64(100),
				"top_up_value_wei": int64(200),
				"daily_limit_wei":  int64(500),
				"weekly_limit_wei": int64(2000),
			},
			want: &TrackedAddressConfig{
				Address:     common.HexToAddress("0x5555555555555555555555555555555555555555"),
				MinBalance:  big.NewInt(100),
				TopUpValue:  big.NewInt(200),
				DailyLimit:  big.NewInt(500),
				WeeklyLimit: big.NewInt(2000),
			},
		},
		{
			name: "parse limit string values",
			data: map[string]any{
				"address":          "0x6666666666666666666666666666666666666666",
				"min_balance_wei":  "100",
				"top_up_value_wei": "200",
				"daily_limit_wei":  "500000000000000000000",
				"weekly_limit_wei": "2000000000000000000000",
			},
			want: &TrackedAddressConfig{
				Address:     common.HexToAddress("0x6666666666666666666666666666666666666666"),
				MinBalance:  newBigInt("100"),
				TopUpValue:  newBigInt("200"),
				DailyLimit:  newBigInt("500000000000000000000"),
				WeeklyLimit: newBigInt("2000000000000000000000"),
			},
		},
		{
			name: "missing limit fields are nil",
			data: map[string]any{
				"address":          "0x7777777777777777777777777777777777777777",
				"min_balance_wei":  int64(100),
				"top_up_value_wei": int64(200),
			},
			want: &TrackedAddressConfig{
				Address:     common.HexToAddress("0x7777777777777777777777777777777777777777"),
				MinBalance:  big.NewInt(100),
				TopUpValue:  big.NewInt(200),
				DailyLimit:  nil,
				WeeklyLimit: nil,
			},
		},
		{
			name: "non-map data returns nil error",
			data: "not a map",
			want: &TrackedAddressConfig{
				MinBalance: nil,
				TopUpValue: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &TrackedAddressConfig{}
			err := cfg.UnmarshalTOML(tt.data)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.want.Address, cfg.Address)

			// Compare MinBalance
			if tt.want.MinBalance == nil {
				require.Nil(t, cfg.MinBalance)
			} else {
				require.NotNil(t, cfg.MinBalance)
				require.Equal(t, tt.want.MinBalance.String(), cfg.MinBalance.String())
			}

			// Compare TopUpValue
			if tt.want.TopUpValue == nil {
				require.Nil(t, cfg.TopUpValue)
			} else {
				require.NotNil(t, cfg.TopUpValue)
				require.Equal(t, tt.want.TopUpValue.String(), cfg.TopUpValue.String())
			}

			// Compare DailyLimit
			if tt.want.DailyLimit == nil {
				require.Nil(t, cfg.DailyLimit)
			} else {
				require.NotNil(t, cfg.DailyLimit)
				require.Equal(t, tt.want.DailyLimit.String(), cfg.DailyLimit.String())
			}

			// Compare WeeklyLimit
			if tt.want.WeeklyLimit == nil {
				require.Nil(t, cfg.WeeklyLimit)
			} else {
				require.NotNil(t, cfg.WeeklyLimit)
				require.Equal(t, tt.want.WeeklyLimit.String(), cfg.WeeklyLimit.String())
			}
		})
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		cfgFile string
		want    *Config
		wantErr bool
	}{
		{
			name:    "valid full config",
			cfgFile: "testcfg/valid-full.toml",
			want: &Config{
				CheckInterval:     5 * 60 * 1000 * 1000 * 1000, // 5m in nanoseconds
				WarningBalanceFLR: 1000,
				TxTimeout:         10 * 1000 * 1000 * 1000, // 10s in nanoseconds
				MaxRetries:        3,
				Addresses: []TrackedAddressConfig{
					{
						Address:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
						MinBalance: newBigInt("10000000000000000000"),
						TopUpValue: newBigInt("100000000000000000000"),
					},
					{
						Address:    common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
						MinBalance: newBigInt("20000000000000000000"),
						TopUpValue: newBigInt("200000000000000000000"),
					},
				},
			},
		},
		{
			name:    "valid minimal config",
			cfgFile: "testcfg/valid-minimal.toml",
			want: &Config{
				Addresses: []TrackedAddressConfig{
					{
						Address:    common.HexToAddress("0x1111111111111111111111111111111111111111"),
						MinBalance: nil,
						TopUpValue: nil,
					},
				},
			},
		},
		{
			name:    "valid config with limits",
			cfgFile: "testcfg/valid-with-limits.toml",
			want: &Config{
				CheckInterval:     5 * 60 * 1000 * 1000 * 1000,
				WarningBalanceFLR: 1000,
				Addresses: []TrackedAddressConfig{
					{
						Address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
						MinBalance:  newBigInt("10000000000000000000"),
						TopUpValue:  newBigInt("100000000000000000000"),
						DailyLimit:  newBigInt("500000000000000000000"),
						WeeklyLimit: newBigInt("2000000000000000000000"),
					},
					{
						Address:    common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
						MinBalance: newBigInt("20000000000000000000"),
						TopUpValue: newBigInt("200000000000000000000"),
					},
				},
			},
		},
		{
			name:    "valid config with wei strings",
			cfgFile: "testcfg/valid-wei-strings.toml",
			want: &Config{
				CheckInterval:     2 * 60 * 1000 * 1000 * 1000, // 2m in nanoseconds
				WarningBalanceFLR: 500,
				TxTimeout:         5 * 1000 * 1000 * 1000, // 5s in nanoseconds
				MaxRetries:        2,
				Addresses: []TrackedAddressConfig{
					{
						Address:    common.HexToAddress("0x2222222222222222222222222222222222222222"),
						MinBalance: newBigInt("15000000000000000000"),
						TopUpValue: newBigInt("150000000000000000000"),
					},
					{
						Address:    common.HexToAddress("0x3333333333333333333333333333333333333333"),
						MinBalance: nil,
						TopUpValue: nil,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.cfgFile)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Compare top-level fields
			require.Equal(t, tt.want.CheckInterval, cfg.CheckInterval)
			require.Equal(t, tt.want.WarningBalanceFLR, cfg.WarningBalanceFLR)
			require.Equal(t, tt.want.TxTimeout, cfg.TxTimeout)
			require.Equal(t, tt.want.MaxRetries, cfg.MaxRetries)

			// Compare addresses
			require.Equal(t, len(tt.want.Addresses), len(cfg.Addresses))
			for i, wantAddr := range tt.want.Addresses {
				require.Equal(t, wantAddr.Address, cfg.Addresses[i].Address)

				// Compare MinBalance
				if wantAddr.MinBalance == nil {
					require.Nil(t, cfg.Addresses[i].MinBalance)
				} else {
					require.NotNil(t, cfg.Addresses[i].MinBalance)
					require.Equal(t, wantAddr.MinBalance.String(), cfg.Addresses[i].MinBalance.String())
				}

				// Compare TopUpValue
				if wantAddr.TopUpValue == nil {
					require.Nil(t, cfg.Addresses[i].TopUpValue)
				} else {
					require.NotNil(t, cfg.Addresses[i].TopUpValue)
					require.Equal(t, wantAddr.TopUpValue.String(), cfg.Addresses[i].TopUpValue.String())
				}

				// Compare DailyLimit
				if wantAddr.DailyLimit == nil {
					require.Nil(t, cfg.Addresses[i].DailyLimit)
				} else {
					require.NotNil(t, cfg.Addresses[i].DailyLimit)
					require.Equal(t, wantAddr.DailyLimit.String(), cfg.Addresses[i].DailyLimit.String())
				}

				// Compare WeeklyLimit
				if wantAddr.WeeklyLimit == nil {
					require.Nil(t, cfg.Addresses[i].WeeklyLimit)
				} else {
					require.NotNil(t, cfg.Addresses[i].WeeklyLimit)
					require.Equal(t, wantAddr.WeeklyLimit.String(), cfg.Addresses[i].WeeklyLimit.String())
				}
			}
		})
	}
}
