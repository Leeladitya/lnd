# Prajavahini ⚡🌉✨

## Cross-Chain Lightning Bridge Protocol

**Bitcoin Lightning ↔ Stellar Atomic Swaps at Layer 2 Speed**

*theINO Research Lab — Praja Digital Civilization*

---

## What is Prajavahini?

Prajavahini (Sanskrit: *"the flow of the people"*) is a cross-chain bridge protocol that enables **trustless, atomic, near-instant payments** between the Bitcoin Lightning Network and the Stellar blockchain.

Unlike existing bridges that use wrapped tokens or custodial lock-and-mint, Prajavahini operates at the **protocol layer** using Hashed Timelock Contracts (HTLCs) as a shared cryptographic primitive. The same SHA-256 preimage settles both sides — no trusted intermediary, no custodial risk.

```
 Bitcoin Lightning                    Stellar Network
┌─────────────────┐                 ┌─────────────────┐
│  Alice sends BTC │                 │  Bob receives XLM│
│  via Lightning   │                 │  via Soroban HTLC│
│                  │   Prajavahini   │                  │
│  HTLC locked to  │◄──── H ────────►│  HTLC locked to  │
│  hash H          │                 │  same hash H     │
│                  │   preimage P    │                  │
│  Settled when P  │◄────────────────│  Bob reveals P   │
│  is extracted    │                 │  to claim XLM    │
└─────────────────┘                 └─────────────────┘
```

## Architecture

### Modified LND (this repo)

The forked [LND](https://github.com/lightningnetwork/lnd) codebase contains a **built-in cross-chain forwarding mechanism** in `htlcswitch/hop/network.go`. We extend it:

| File | Change |
|------|--------|
| `htlcswitch/hop/network.go` | Added `StellarNetwork` constant + `IsCrossChain()` method |
| `stellar/config.go` | Stellar bridge configuration (Horizon, Soroban RPC, HTLC params) |
| `stellar/connector/` | Stellar network connector (Horizon API + Soroban RPC client) |
| `stellar/bridge/` | Bridge relay daemon (swap orchestration, preimage extraction) |
| `stellar/htlc/` | Cross-chain HTLC types with state machine and timelock safety |

### Soroban HTLC Smart Contract

Located in `prajavahini-contracts/htlc/`, this Rust/Soroban contract handles the Stellar side:

- `create_htlc()` — Lock XLM to a SHA-256 hash (same hash as Bitcoin side)
- `claim()` — Receiver reveals preimage to claim XLM (**preimage goes on-chain**)
- `refund()` — Sender reclaims after timeout
- `get_preimage()` — Bridge daemon extracts preimage for Bitcoin settlement

### Cross-Chain Protocol Flow

```
1. Bob generates preimage P, computes H = SHA256(P)
2. Alice sends BTC to bridge via Lightning, locked to H
3. Bridge HTLC interceptor catches StellarNetwork destination
4. Bridge creates Soroban HTLC on Stellar, locked to same H
5. Bob claims Stellar HTLC by revealing P on-chain
6. Bridge extracts P from Stellar event/state
7. Bridge settles Bitcoin HTLC using P
8. ✅ Atomic: both sides settled, or neither
```

### Timelock Safety

```
BTC_timeout (24h) > Stellar_timeout (4h) + safety_margin (2h)
```

The Bitcoin-side HTLC always has a longer timeout than Stellar. This ensures the bridge can always extract the preimage and settle before Bitcoin expires.

## Quick Start

### 1. Build Modified LND

```bash
git clone https://github.com/Leeladitya/lnd.git
cd lnd
git checkout prajavahini/cross-chain-bridge
make install
```

### 2. Deploy Soroban Contract (Stellar Testnet)

```bash
cd prajavahini-contracts/htlc
cargo build --target wasm32-unknown-unknown --release

# Install Stellar CLI
stellar contract deploy \
  --wasm target/wasm32-unknown-unknown/release/prajavahini_htlc.wasm \
  --source <YOUR_SECRET_KEY> \
  --network testnet
```

### 3. Configure LND for Prajavahini

Add to your `lnd.conf`:

```ini
[prajavahini]
prajavahini.active=true
prajavahini.horizon-url=https://horizon-testnet.stellar.org
prajavahini.soroban-rpc-url=https://soroban-testnet.stellar.org
prajavahini.htlc-contract-id=<DEPLOYED_CONTRACT_ADDRESS>
prajavahini.bridge-account-seed=<STELLAR_SECRET_SEED>
prajavahini.network=testnet
```

### 4. Start LND with Bridge

```bash
lnd --bitcoin.active --bitcoin.testnet --prajavahini.active
```

## Project Structure

```
lnd/
├── htlcswitch/
│   └── hop/
│       └── network.go          ← StellarNetwork added here
├── stellar/
│   ├── doc.go                  ← Package documentation
│   ├── config.go               ← Bridge configuration
│   ├── connector/
│   │   ├── connector.go        ← Stellar Horizon + Soroban client
│   │   └── log.go
│   ├── bridge/
│   │   ├── relay.go            ← Core swap orchestration
│   │   └── log.go
│   └── htlc/
│       └── types.go            ← Cross-chain HTLC state machine
├── prajavahini-contracts/
│   └── htlc/
│       ├── Cargo.toml
│       └── src/
│           └── lib.rs          ← Soroban HTLC smart contract
└── README.md
```

## Integration with Praja

Prajavahini is the settlement infrastructure for [Praja](https://praja.live), humanity's first gated digital civilization:

- **Commission DeX**: BTC/XLM/PJ trading pairs at Lightning speed
- **PJ Token**: Bridge routing currency (BTC → PJ → XLM)
- **Pracharam**: Accept tips in BTC via Lightning
- **Shodo**: Pay for courses in XLM
- **Chayaa**: Multi-currency lifestyle payments

## Status

🔬 **Research & Development** — Phase 1 (Foundation)

- [x] LND codebase analysis
- [x] StellarNetwork hop type added
- [x] Stellar connector architecture
- [x] Bridge relay daemon
- [x] Cross-chain HTLC types + state machine
- [x] Soroban HTLC smart contract
- [x] Timelock safety analysis
- [ ] Exchange rate oracle integration
- [ ] Full htlcswitch integration
- [ ] Testnet end-to-end atomic swap
- [ ] Watchtower for Stellar channels
- [ ] Multi-hop cross-chain routing
- [ ] Production hardening

## Contributing

This is a theINO Research Lab project. Contributions welcome — open an issue or PR.

## License

MIT — same as LND upstream.

## Credits

- **Leeladitya (Ade)** — Principal Researcher, theINO
- **theINO** — Innovation Research Lab, Praja
- **LND** by Lightning Labs (upstream)
- **Stellar Development Foundation** — State channel specification & Soroban

---

*Prajavahini — The flow of the people, across every chain.* ⚡✨
