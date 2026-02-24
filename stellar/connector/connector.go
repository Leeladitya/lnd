package connector

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

// StellarConnector manages the connection to the Stellar network via
// Horizon API and Soroban RPC. It handles HTLC creation, preimage
// monitoring, and refund execution on the Stellar side of the
// Prajavahini bridge.
type StellarConnector struct {
	horizonURL    string
	sorobanRPCURL string
	contractID    string
	bridgeSeed    string
	networkPass   string
	httpClient    *http.Client

	// activeHTLCs tracks all Stellar-side HTLCs being monitored for
	// preimage reveals.
	activeHTLCs map[uint64]*StellarHTLC
	mu          sync.RWMutex

	// preimageCallbacks stores functions to call when a preimage is
	// revealed on a specific payment hash.
	preimageCallbacks map[[32]byte]func(preimage [32]byte)
	cbMu              sync.RWMutex

	quit chan struct{}
}

// StellarHTLC represents an HTLC on the Stellar/Soroban side.
type StellarHTLC struct {
	ID          uint64
	PaymentHash [32]byte
	Amount      int64  // stroops
	Sender      string // Stellar address
	Receiver    string // Stellar address
	Timeout     time.Time
	TxHash      string
	Claimed     bool
	Preimage    *[32]byte
	CreatedAt   time.Time
}

// SorobanRPCRequest is the JSON-RPC request format for Soroban.
type SorobanRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  interface{}   `json:"params,omitempty"`
}

// SorobanRPCResponse is the JSON-RPC response format.
type SorobanRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *SorobanError   `json:"error,omitempty"`
}

// SorobanError represents a Soroban RPC error.
type SorobanError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewStellarConnector creates a new connector to the Stellar network.
func NewStellarConnector(
	horizonURL string,
	sorobanRPCURL string,
	contractID string,
	bridgeSeed string,
	network string,
) (*StellarConnector, error) {

	var networkPass string
	switch network {
	case "testnet":
		networkPass = "Test SDF Network ; September 2015"
	case "mainnet":
		networkPass = "Public Global Stellar Network ; September 2015"
	case "futurenet":
		networkPass = "Test SDF Future Network ; October 2022"
	default:
		return nil, fmt.Errorf("unknown stellar network: %s", network)
	}

	return &StellarConnector{
		horizonURL:        horizonURL,
		sorobanRPCURL:     sorobanRPCURL,
		contractID:        contractID,
		bridgeSeed:        bridgeSeed,
		networkPass:       networkPass,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
		activeHTLCs:       make(map[uint64]*StellarHTLC),
		preimageCallbacks: make(map[[32]byte]func(preimage [32]byte)),
		quit:              make(chan struct{}),
	}, nil
}

// Start begins the Stellar connector's background processes including
// preimage monitoring and HTLC timeout tracking.
func (sc *StellarConnector) Start(ctx context.Context) error {
	log.Infof("Prajavahini: Starting Stellar connector (horizon=%s, soroban=%s)",
		sc.horizonURL, sc.sorobanRPCURL)

	// Verify connectivity to Horizon.
	if err := sc.pingHorizon(ctx); err != nil {
		return fmt.Errorf("failed to connect to Horizon: %w", err)
	}

	// Verify Soroban RPC connectivity.
	if err := sc.pingSoroban(ctx); err != nil {
		return fmt.Errorf("failed to connect to Soroban RPC: %w", err)
	}

	// Start the preimage monitoring loop.
	go sc.monitorPreimages(ctx)

	// Start the timeout checker.
	go sc.checkTimeouts(ctx)

	log.Infof("Prajavahini: Stellar connector started successfully")
	return nil
}

// Stop gracefully shuts down the connector.
func (sc *StellarConnector) Stop() {
	log.Infof("Prajavahini: Stopping Stellar connector")
	close(sc.quit)
}

// CreateHTLC creates a new HTLC on the Stellar network via the Soroban
// contract. This locks XLM that can be claimed by revealing the preimage.
func (sc *StellarConnector) CreateHTLC(
	ctx context.Context,
	paymentHash [32]byte,
	receiverAddress string,
	amountStroops int64,
	timeout time.Time,
) (uint64, string, error) {

	log.Infof("Prajavahini: Creating Stellar HTLC (hash=%s, receiver=%s, amount=%d stroops, timeout=%s)",
		hex.EncodeToString(paymentHash[:8]), receiverAddress, amountStroops,
		timeout.Format(time.RFC3339))

	// Build the Soroban contract invocation to create_htlc.
	// The contract call passes:
	//   - sender: bridge account address
	//   - receiver: destination Stellar address
	//   - amount: XLM in stroops
	//   - hash_lock: the SHA-256 payment hash (same as Bitcoin side)
	//   - timeout: UNIX timestamp for expiry
	params := map[string]interface{}{
		"contract_id": sc.contractID,
		"function":    "create_htlc",
		"args": []interface{}{
			receiverAddress,
			amountStroops,
			hex.EncodeToString(paymentHash[:]),
			timeout.Unix(),
		},
	}

	result, err := sc.invokeSoroban(ctx, params)
	if err != nil {
		return 0, "", fmt.Errorf("soroban create_htlc failed: %w", err)
	}

	// Parse the returned HTLC ID and transaction hash.
	var response struct {
		HTLCID uint64 `json:"htlc_id"`
		TxHash string `json:"tx_hash"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return 0, "", fmt.Errorf("failed to parse create_htlc response: %w", err)
	}

	// Track the HTLC for preimage monitoring.
	stellarHTLC := &StellarHTLC{
		ID:          response.HTLCID,
		PaymentHash: paymentHash,
		Amount:      amountStroops,
		Sender:      "", // Bridge account, resolved from seed
		Receiver:    receiverAddress,
		Timeout:     timeout,
		TxHash:      response.TxHash,
		CreatedAt:   time.Now(),
	}

	sc.mu.Lock()
	sc.activeHTLCs[response.HTLCID] = stellarHTLC
	sc.mu.Unlock()

	log.Infof("Prajavahini: Stellar HTLC created (id=%d, tx=%s)",
		response.HTLCID, response.TxHash)

	return response.HTLCID, response.TxHash, nil
}

// OnPreimageRevealed registers a callback that fires when the preimage
// for a specific payment hash is revealed on the Stellar side. This is
// the critical bridge event that triggers Bitcoin-side settlement.
func (sc *StellarConnector) OnPreimageRevealed(paymentHash [32]byte, callback func(preimage [32]byte)) {
	sc.cbMu.Lock()
	defer sc.cbMu.Unlock()
	sc.preimageCallbacks[paymentHash] = callback
}

// QueryHTLCState queries the current state of an HTLC on the Soroban contract.
func (sc *StellarConnector) QueryHTLCState(ctx context.Context, htlcID uint64) (*StellarHTLC, error) {
	params := map[string]interface{}{
		"contract_id": sc.contractID,
		"function":    "get_htlc",
		"args":        []interface{}{htlcID},
	}

	result, err := sc.invokeSoroban(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("soroban get_htlc failed: %w", err)
	}

	var htlc StellarHTLC
	if err := json.Unmarshal(result, &htlc); err != nil {
		return nil, fmt.Errorf("failed to parse get_htlc response: %w", err)
	}

	return &htlc, nil
}

// monitorPreimages continuously monitors all active Stellar HTLCs for
// preimage reveals. When a receiver claims an HTLC by revealing the
// preimage, this function extracts it and triggers the Bitcoin-side
// settlement callback.
func (sc *StellarConnector) monitorPreimages(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sc.mu.RLock()
			htlcs := make([]*StellarHTLC, 0, len(sc.activeHTLCs))
			for _, h := range sc.activeHTLCs {
				if !h.Claimed {
					htlcs = append(htlcs, h)
				}
			}
			sc.mu.RUnlock()

			for _, htlc := range htlcs {
				sc.checkHTLCClaimed(ctx, htlc)
			}

		case <-sc.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}

// checkHTLCClaimed checks if a specific HTLC has been claimed (preimage
// revealed) on the Stellar side.
func (sc *StellarConnector) checkHTLCClaimed(ctx context.Context, htlc *StellarHTLC) {
	state, err := sc.QueryHTLCState(ctx, htlc.ID)
	if err != nil {
		log.Warnf("Prajavahini: Failed to query HTLC %d state: %v", htlc.ID, err)
		return
	}

	if state.Claimed && state.Preimage != nil {
		log.Infof("Prajavahini: Preimage revealed on Stellar for HTLC %d (hash=%s)",
			htlc.ID, hex.EncodeToString(htlc.PaymentHash[:8]))

		// Update our tracked state.
		sc.mu.Lock()
		htlc.Claimed = true
		htlc.Preimage = state.Preimage
		sc.mu.Unlock()

		// Fire the callback to settle the Bitcoin side.
		sc.cbMu.RLock()
		callback, ok := sc.preimageCallbacks[htlc.PaymentHash]
		sc.cbMu.RUnlock()

		if ok {
			log.Infof("Prajavahini: Triggering Bitcoin-side settlement for hash=%s",
				hex.EncodeToString(htlc.PaymentHash[:8]))
			callback(*state.Preimage)
		}
	}
}

// checkTimeouts monitors for expired HTLCs and triggers refunds.
func (sc *StellarConnector) checkTimeouts(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			sc.mu.RLock()
			for _, htlc := range sc.activeHTLCs {
				if !htlc.Claimed && now.After(htlc.Timeout) {
					log.Infof("Prajavahini: Stellar HTLC %d timed out, initiating refund", htlc.ID)
					go sc.refundHTLC(ctx, htlc.ID)
				}
			}
			sc.mu.RUnlock()

		case <-sc.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}

// refundHTLC triggers a refund on an expired Stellar HTLC.
func (sc *StellarConnector) refundHTLC(ctx context.Context, htlcID uint64) {
	params := map[string]interface{}{
		"contract_id": sc.contractID,
		"function":    "refund",
		"args":        []interface{}{htlcID},
	}

	_, err := sc.invokeSoroban(ctx, params)
	if err != nil {
		log.Errorf("Prajavahini: Failed to refund HTLC %d: %v", htlcID, err)
		return
	}

	sc.mu.Lock()
	delete(sc.activeHTLCs, htlcID)
	sc.mu.Unlock()

	log.Infof("Prajavahini: Stellar HTLC %d refunded successfully", htlcID)
}

// invokeSoroban sends a JSON-RPC call to the Soroban RPC server.
func (sc *StellarConnector) invokeSoroban(ctx context.Context, params interface{}) (json.RawMessage, error) {
	req := SorobanRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "simulateTransaction",
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", sc.sorobanRPCURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := sc.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("soroban RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var rpcResp SorobanRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("soroban RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// pingHorizon verifies connectivity to the Horizon API.
func (sc *StellarConnector) pingHorizon(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", sc.horizonURL, nil)
	if err != nil {
		return err
	}

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("horizon returned status %d", resp.StatusCode)
	}

	return nil
}

// pingSoroban verifies connectivity to the Soroban RPC.
func (sc *StellarConnector) pingSoroban(ctx context.Context) error {
	req := SorobanRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getHealth",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", sc.sorobanRPCURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := sc.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("soroban RPC returned status %d", resp.StatusCode)
	}

	return nil
}
