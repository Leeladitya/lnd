package htlc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// State represents the lifecycle state of a cross-chain HTLC.
type State uint8

const (
	// StateInit means the HTLC has been intercepted on the Bitcoin side
	// but not yet created on Stellar.
	StateInit State = iota

	// StateStellarPending means the Stellar-side HTLC has been submitted
	// to the Soroban contract but not yet confirmed.
	StateStellarPending

	// StateStellarActive means the Stellar-side HTLC is confirmed and
	// active on-chain, waiting for preimage reveal or timeout.
	StateStellarActive

	// StatePreimageRevealed means the preimage has been revealed on the
	// Stellar side (receiver claimed). Bridge needs to settle BTC side.
	StatePreimageRevealed

	// StateBitcoinSettled means the Bitcoin-side HTLC has been settled
	// using the extracted preimage. Cross-chain swap complete.
	StateBitcoinSettled

	// StateRefunded means the Stellar-side HTLC timed out and funds
	// were returned. Bitcoin-side HTLC should also be failed.
	StateRefunded

	// StateFailed means the cross-chain HTLC failed at some point
	// in the pipeline.
	StateFailed
)

// String returns a human-readable state name.
func (s State) String() string {
	switch s {
	case StateInit:
		return "INIT"
	case StateStellarPending:
		return "STELLAR_PENDING"
	case StateStellarActive:
		return "STELLAR_ACTIVE"
	case StatePreimageRevealed:
		return "PREIMAGE_REVEALED"
	case StateBitcoinSettled:
		return "BITCOIN_SETTLED"
	case StateRefunded:
		return "REFUNDED"
	case StateFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// IsTerminal returns true if the HTLC is in a final state.
func (s State) IsTerminal() bool {
	switch s {
	case StateBitcoinSettled, StateRefunded, StateFailed:
		return true
	default:
		return false
	}
}

// CrossChainHTLC represents a single atomic cross-chain HTLC that spans
// both the Bitcoin Lightning Network and the Stellar network.
type CrossChainHTLC struct {
	// ID is a unique identifier for this cross-chain HTLC.
	ID string

	// PaymentHash is the SHA-256 hash that locks both sides of the
	// atomic swap. This is the same hash used in the Bitcoin Lightning
	// HTLC and the Stellar Soroban HTLC contract.
	PaymentHash [32]byte

	// Preimage is the 32-byte preimage that unlocks PaymentHash.
	// This is nil until the receiver reveals it on the Stellar side.
	Preimage *[32]byte

	// ── Bitcoin Side ──

	// BtcAmountMsat is the amount on the Bitcoin Lightning side in
	// milli-satoshis.
	BtcAmountMsat int64

	// BtcIncomingChanID is the incoming channel ID on the Bitcoin side.
	BtcIncomingChanID uint64

	// BtcIncomingHTLCID is the incoming HTLC ID on the Bitcoin side.
	BtcIncomingHTLCID uint64

	// BtcTimeoutHeight is the absolute block height at which the
	// Bitcoin-side HTLC expires.
	BtcTimeoutHeight uint32

	// ── Stellar Side ──

	// XlmAmountStroops is the amount on the Stellar side in stroops
	// (1 XLM = 10^7 stroops).
	XlmAmountStroops int64

	// StellarHTLCID is the on-chain HTLC ID returned by the Soroban
	// contract after creation.
	StellarHTLCID uint64

	// StellarSender is the Stellar account that funds the HTLC
	// (the bridge operator).
	StellarSender string

	// StellarReceiver is the Stellar account that can claim the HTLC
	// by revealing the preimage.
	StellarReceiver string

	// StellarTimeout is the absolute timestamp at which the Stellar-side
	// HTLC expires. Must be < BtcTimeout to maintain atomic safety.
	StellarTimeout time.Time

	// StellarTxHash is the transaction hash of the Stellar HTLC creation.
	StellarTxHash string

	// ── State ──

	// State is the current lifecycle state of this cross-chain HTLC.
	State State

	// CreatedAt is when this cross-chain HTLC was first intercepted.
	CreatedAt time.Time

	// UpdatedAt is the last state transition timestamp.
	UpdatedAt time.Time

	// ErrorMsg contains any error message if State is StateFailed.
	ErrorMsg string

	// ExchangeRate is the BTC/XLM rate used for this swap.
	ExchangeRate float64

	mu sync.RWMutex
}

// NewCrossChainHTLC creates a new cross-chain HTLC from an intercepted
// Bitcoin Lightning payment.
func NewCrossChainHTLC(
	paymentHash [32]byte,
	btcAmountMsat int64,
	incomingChanID uint64,
	incomingHTLCID uint64,
	btcTimeoutHeight uint32,
) *CrossChainHTLC {

	now := time.Now()
	return &CrossChainHTLC{
		ID:                fmt.Sprintf("pv-%s-%d", hex.EncodeToString(paymentHash[:8]), now.UnixNano()),
		PaymentHash:       paymentHash,
		BtcAmountMsat:     btcAmountMsat,
		BtcIncomingChanID: incomingChanID,
		BtcIncomingHTLCID: incomingHTLCID,
		BtcTimeoutHeight:  btcTimeoutHeight,
		State:             StateInit,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// SetStellarParams sets the Stellar-side parameters after exchange rate
// calculation and before HTLC creation on Soroban.
func (h *CrossChainHTLC) SetStellarParams(
	xlmAmount int64,
	sender string,
	receiver string,
	timeout time.Time,
	exchangeRate float64,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.XlmAmountStroops = xlmAmount
	h.StellarSender = sender
	h.StellarReceiver = receiver
	h.StellarTimeout = timeout
	h.ExchangeRate = exchangeRate
	h.UpdatedAt = time.Now()
}

// Transition moves the HTLC to a new state with validation.
func (h *CrossChainHTLC) Transition(newState State) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.State.IsTerminal() {
		return fmt.Errorf("cannot transition from terminal state %s", h.State)
	}

	// Validate allowed transitions.
	if !isValidTransition(h.State, newState) {
		return fmt.Errorf("invalid transition: %s -> %s", h.State, newState)
	}

	h.State = newState
	h.UpdatedAt = time.Now()
	return nil
}

// SetPreimage stores the revealed preimage and validates it against the hash.
func (h *CrossChainHTLC) SetPreimage(preimage [32]byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Verify the preimage matches the payment hash.
	computed := sha256.Sum256(preimage[:])
	if computed != h.PaymentHash {
		return fmt.Errorf("preimage does not match payment hash: got %x, want %x",
			computed, h.PaymentHash)
	}

	h.Preimage = &preimage
	h.State = StatePreimageRevealed
	h.UpdatedAt = time.Now()
	return nil
}

// GetState returns the current state (thread-safe).
func (h *CrossChainHTLC) GetState() State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.State
}

// isValidTransition checks if a state transition is allowed.
func isValidTransition(from, to State) bool {
	allowed := map[State][]State{
		StateInit:             {StateStellarPending, StateFailed},
		StateStellarPending:   {StateStellarActive, StateFailed},
		StateStellarActive:    {StatePreimageRevealed, StateRefunded, StateFailed},
		StatePreimageRevealed: {StateBitcoinSettled, StateFailed},
	}

	validTargets, ok := allowed[from]
	if !ok {
		return false
	}

	for _, valid := range validTargets {
		if to == valid {
			return true
		}
	}
	return false
}

// TimelockSafety verifies that the Stellar timeout is sufficiently earlier
// than the Bitcoin timeout to ensure atomic safety.
//
// The invariant is: BtcTimeout > StellarTimeout + SafetyMargin
// where SafetyMargin accounts for:
//   - Stellar network latency (~5s per ledger close)
//   - Soroban contract execution time
//   - Bridge daemon monitoring interval
//   - Bitcoin block confirmation variance
const MinSafetyMarginSeconds = 7200 // 2 hours

func (h *CrossChainHTLC) TimelockSafety(currentBtcHeight uint32, avgBlockTimeSec int64) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Estimate Bitcoin timeout as absolute time.
	blocksRemaining := int64(h.BtcTimeoutHeight) - int64(currentBtcHeight)
	if blocksRemaining <= 0 {
		return fmt.Errorf("bitcoin HTLC already expired at height %d (current: %d)",
			h.BtcTimeoutHeight, currentBtcHeight)
	}

	btcTimeoutEstimate := time.Now().Add(time.Duration(blocksRemaining*avgBlockTimeSec) * time.Second)

	// Check safety margin.
	margin := btcTimeoutEstimate.Sub(h.StellarTimeout)
	if margin.Seconds() < MinSafetyMarginSeconds {
		return fmt.Errorf(
			"insufficient timelock safety margin: %.0fs (minimum: %ds). "+
				"BTC timeout: %s, Stellar timeout: %s",
			margin.Seconds(), MinSafetyMarginSeconds,
			btcTimeoutEstimate.Format(time.RFC3339),
			h.StellarTimeout.Format(time.RFC3339),
		)
	}

	return nil
}
