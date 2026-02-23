// Package stellar implements the Prajavahini cross-chain bridge protocol
// between Bitcoin Lightning Network and the Stellar blockchain.
//
// Prajavahini (Sanskrit: "the flow of the people") enables trustless,
// atomic, near-instant cross-chain payments using Hashed Timelock Contracts
// (HTLCs) as a shared cryptographic primitive across both chains.
//
// Architecture:
//
//   Bitcoin Lightning (LND)          Prajavahini Bridge          Stellar Network
//   ┌──────────────────┐          ┌─────────────────┐          ┌──────────────────┐
//   │  HTLC Switch     │◄────────►│  Bridge Relay    │◄────────►│  Soroban HTLC    │
//   │  (htlcswitch/)   │          │  Daemon          │          │  Contract        │
//   │                  │          │                  │          │                  │
//   │  PaymentCircuit  │          │  Preimage Watch  │          │  State Channels  │
//   │  PaymentHash[32] │──HASH───►│  Oracle Module   │──HASH───►│  BUMP_SEQUENCE   │
//   └──────────────────┘          └─────────────────┘          └──────────────────┘
//
// The bridge operates by intercepting HTLCs destined for StellarNetwork
// (defined in htlcswitch/hop/network.go) and creating corresponding HTLCs
// on Stellar via a Soroban smart contract. Settlement is atomic: the same
// SHA-256 preimage settles both sides.
//
// theINO Research Lab - Praja Digital Civilization
// https://github.com/Leeladitya/lnd
package stellar
