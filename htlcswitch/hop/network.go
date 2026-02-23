package hop

// Network indicates the blockchain network that is intended to be the next hop
// for a forwarded HTLC. The existence of this field within the ForwardingInfo
// struct enables the ability for HTLC to cross chain-boundaries at will.
type Network uint8

const (
	// BitcoinNetwork denotes that an HTLC is to be forwarded along the
	// Bitcoin link with the specified short channel ID.
	BitcoinNetwork Network = iota

	// LitecoinNetwork denotes that an HTLC is to be forwarded along the
	// Litecoin link with the specified short channel ID.
	LitecoinNetwork

	// StellarNetwork denotes that an HTLC is to be forwarded across the
	// Prajavahini cross-chain bridge to the Stellar network. The HTLC
	// will be settled via a corresponding Soroban HTLC contract using
	// the same payment hash (SHA-256 preimage) for atomic cross-chain
	// settlement. This enables trustless BTC<->XLM atomic swaps at
	// Layer 2 speed.
	//
	// Prajavahini Bridge Protocol - theINO Research Lab (Praja)
	// https://github.com/Leeladitya/lnd
	StellarNetwork
)

// String returns the string representation of the target Network.
func (c Network) String() string {
	switch c {
	case BitcoinNetwork:
		return "Bitcoin"
	case LitecoinNetwork:
		return "Litecoin"
	case StellarNetwork:
		return "Stellar"
	default:
		return "Unknown"
	}
}

// IsCrossChain returns true if the network requires cross-chain bridge
// settlement rather than native Lightning channel forwarding.
func (c Network) IsCrossChain() bool {
	switch c {
	case StellarNetwork:
		return true
	default:
		return false
	}
}
