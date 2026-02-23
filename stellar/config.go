package stellar

import (
	"fmt"
	"time"
)

// Config holds the configuration for the Prajavahini Stellar bridge.
type Config struct {
	// HorizonURL is the URL of the Stellar Horizon API server.
	// For testnet: https://horizon-testnet.stellar.org
	// For mainnet: https://horizon.stellar.org
	HorizonURL string `long:"horizon-url" description:"Stellar Horizon API URL"`

	// SorobanRPCURL is the URL of the Soroban RPC server for smart
	// contract interactions.
	SorobanRPCURL string `long:"soroban-rpc-url" description:"Soroban RPC server URL"`

	// HTLCContractID is the deployed Soroban HTLC contract address on
	// the Stellar network. This contract handles cross-chain HTLC
	// creation, preimage verification, and timeout refunds.
	HTLCContractID string `long:"htlc-contract-id" description:"Deployed Soroban HTLC contract address"`

	// BridgeAccountSeed is the Stellar secret seed for the bridge
	// operator's account. This account creates and funds Stellar-side
	// HTLCs.
	BridgeAccountSeed string `long:"bridge-account-seed" description:"Stellar bridge account secret seed"`

	// Network specifies which Stellar network to connect to.
	// Valid values: "testnet", "mainnet", "futurenet"
	Network string `long:"network" description:"Stellar network (testnet/mainnet/futurenet)" choice:"testnet" choice:"mainnet" choice:"futurenet"`

	// DefaultHTLCTimeout is the default timeout for Stellar-side HTLCs
	// in seconds. Must be shorter than the Bitcoin-side HTLC timeout to
	// maintain atomic safety.
	DefaultHTLCTimeout time.Duration `long:"default-htlc-timeout" description:"Default Stellar HTLC timeout" default:"4h"`

	// MinHTLCAmountXLM is the minimum HTLC amount in stroops (1 XLM = 10^7 stroops).
	MinHTLCAmountXLM int64 `long:"min-htlc-xlm" description:"Minimum HTLC amount in stroops" default:"10000000"`

	// MaxHTLCAmountXLM is the maximum HTLC amount in stroops.
	MaxHTLCAmountXLM int64 `long:"max-htlc-xlm" description:"Maximum HTLC amount in stroops" default:"100000000000"`

	// OracleURL is the exchange rate oracle endpoint for BTC/XLM pricing.
	OracleURL string `long:"oracle-url" description:"BTC/XLM exchange rate oracle URL"`

	// PreimageMonitorInterval is how often the bridge polls Stellar for
	// preimage reveals on active HTLCs.
	PreimageMonitorInterval time.Duration `long:"preimage-monitor-interval" description:"Stellar preimage monitoring interval" default:"2s"`

	// Active determines whether the Prajavahini bridge is enabled.
	Active bool `long:"active" description:"Enable the Prajavahini Stellar cross-chain bridge"`
}

// Validate checks the Config for internal consistency.
func (c *Config) Validate() error {
	if !c.Active {
		return nil
	}

	if c.HorizonURL == "" {
		return fmt.Errorf("prajavahini: horizon-url is required when bridge is active")
	}
	if c.SorobanRPCURL == "" {
		return fmt.Errorf("prajavahini: soroban-rpc-url is required when bridge is active")
	}
	if c.HTLCContractID == "" {
		return fmt.Errorf("prajavahini: htlc-contract-id is required when bridge is active")
	}
	if c.BridgeAccountSeed == "" {
		return fmt.Errorf("prajavahini: bridge-account-seed is required when bridge is active")
	}
	if c.Network == "" {
		return fmt.Errorf("prajavahini: network must be specified (testnet/mainnet/futurenet)")
	}
	if c.DefaultHTLCTimeout < 10*time.Minute {
		return fmt.Errorf("prajavahini: default-htlc-timeout must be at least 10 minutes")
	}
	if c.MinHTLCAmountXLM <= 0 {
		return fmt.Errorf("prajavahini: min-htlc-xlm must be positive")
	}
	if c.MaxHTLCAmountXLM <= c.MinHTLCAmountXLM {
		return fmt.Errorf("prajavahini: max-htlc-xlm must be greater than min-htlc-xlm")
	}

	return nil
}

// DefaultConfig returns the default Prajavahini configuration for testnet.
func DefaultConfig() *Config {
	return &Config{
		HorizonURL:              "https://horizon-testnet.stellar.org",
		SorobanRPCURL:           "https://soroban-testnet.stellar.org",
		Network:                 "testnet",
		DefaultHTLCTimeout:      4 * time.Hour,
		MinHTLCAmountXLM:        10000000,   // 1 XLM
		MaxHTLCAmountXLM:        100000000000, // 10,000 XLM
		PreimageMonitorInterval: 2 * time.Second,
		Active:                  false,
	}
}
