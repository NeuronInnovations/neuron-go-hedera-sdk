# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The **neuron-go-hedera-sdk** is a decentralized SDK for peer-to-peer interactions within the Neuron Network. It uses **Hedera Hashgraph** as a rendezvous mechanism and **libp2p** for direct P2P connectivity. The SDK enables buyer-seller-validator interactions with dual-channel communication: transparent coordination via Hedera topics and encrypted data transfer via P2P streams.

**Core Module**: `github.com/NeuronInnovations/neuron-go-hedera-sdk`
**Go Version**: 1.23+ (toolchain 1.23.2)

## Build and Test Commands

### Building
```bash
# Build the SDK
go build -o neuron-sdk

# Install dependencies
go mod tidy
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./hedera
go test ./keylib
go test ./whoami

# Run a specific test
go test ./hedera -run TestSpecificFunction
```

### Running the SDK

The SDK requires an environment file (`.env`) with Hedera credentials and configuration.

**Buyer mode**:
```bash
./neuron-sdk --mode=peer --buyer-or-seller=buyer --port=30088 --list-of-sellers-source=env
```

**Seller mode**:
```bash
./neuron-sdk --mode=peer --buyer-or-seller=seller --port=20088 --envFile=.env-seller
```

**Testing multiple instances locally**: Use different `.env` files and ports for each instance.

## Architecture Overview

### Dual-Channel Communication Pattern

The SDK uses two complementary communication channels:

1. **Hedera Topics** (public, transparent): Coordination, heartbeats, payment requests, error messages
2. **P2P Streams** (private, encrypted): Actual data transfer via libp2p

Each peer maintains three topics:
- `stdIn`: Receives messages from other peers (service requests, payment demands, errors)
- `stdOut`: Publishes status messages and heartbeats
- `stdErr`: Logs error messages

### Entry Point and Callback Architecture

The SDK is initialized via `LaunchSDK()` in [neuron-sdk.go](neuron-sdk.go), which accepts six callbacks for extensibility:

```go
LaunchSDK(
    version string,                                                  // App version
    protocolID protocol.ID,                                          // Protocol identifier (e.g., "/nrn-mydapp/v1")
    keyLocationConfigurator func(chan bool, string) error,           // Custom key/location setup (nil if unused)
    buyerCaseCallback func(context.Context, host.Host, *NodeBuffers), // Buyer P2P logic
    buyerTopicListener func(hedera.TopicMessage),                    // Buyer Hedera topic listener
    sellerCaseCallback func(context.Context, host.Host, *NodeBuffers), // Seller P2P logic
    sellerTopicListener func(hedera.TopicMessage),                   // Seller Hedera topic listener
)
```

**Initialization Flow**:
```
LaunchSDK()
  ├─ init() - Parse flags & load environment
  ├─ SetupKeysAndLocation() - Derive cryptographic identities
  ├─ createHost() - Initialize libp2p host with QUIC transport
  ├─ launchBuyerVersusSellerApp()
  │   ├─ HandleBuyerCase() / HandleSellerCase()
  │   ├─ hederaAnnounceAndHeartBeat() - Register on Hedera topics
  │   └─ Start connection/message listeners
  └─ Signal handling (Ctrl+C shutdown)
```

### Key Package Organization

**[/hedera](hedera/)**: Hedera network integration
- `GetHederaClientUsingEnv()`: Creates Hedera client from environment
- `CreateTopic()`, `SendToTopic()`: Topic management
- `BuyerPrepareServiceRequest()`, `SellerSendScheduledTransferRequest()`: Transaction handling
- `ListenToTopicAndCallBack()`: Topic subscription with callbacks
- `GetHRpcClient()`: Smart contract interaction via gRPC

**[/common-lib](common-lib/)**: Shared utilities and state management
- `NodeBuffers`: Thread-safe connection state for all peers (tracks `LibP2PState` and `RendezvousState`)
- `InitialConnect()`: P2P connection establishment with hole punching
- `hederaMessages.go`: Message types (`NeuronHeartBeatMsg`, `NeuronServiceRequestMsg`, `NeuronScheduleSignRequestMsg`, etc.)
- `flags.go`: Command-line flag definitions
- `environment.go`: Environment variable loading with support for multiple `.env` files

**[/dapp-protocols/stream-buyer-vs-seller](dapp-protocols/stream-buyer-vs-seller/)**: Primary protocol implementation
- `HandleBuyerCase()`: Buyer-side logic (initiates requests, receives data)
- `HandleSellerCase()`: Seller-side logic (responds to requests, serves data)

**[/keylib](keylib/)**: Cryptographic operations
- Key format conversion (secp256k1, ed25519)
- Key exchange and negotiation

**[/hederacontract](hederacontract/)**: Auto-generated smart contract bindings
- Device registration and peer discovery
- Maps EVM address → (PeerID, Topics, Services)

**[/whoami](whoami/)**: NAT detection and reachability
- Determines if node is publicly reachable
- Detects NAT type and public IP/port

**[/upnp](upnp/)**: UPnP port forwarding for NAT traversal

### Heterogeneous Identity System

All identities derive from a single **secp256k1 private key**:

```
secp256k1 Private Key
  └─ Public Key (Hedera alias key)
      ├─ IPFS Peer ID (libp2p)
      ├─ EVM Address (smart contract registration)
      └─ Future: Cross-chain addresses
```

### State Management

`NodeBuffers` (in [common-lib/buffers.go](common-lib/buffers.go)) maintains connection state:
- `LibP2PState`: Connected, Connecting, ConnectionLost, etc.
- `RendezvousState`: NotInitiated, SendOK, ReceivedOK, etc.
- Thread-safe map of `NodeBufferInfo` objects per peer

### Operational Modes

1. **Peer Mode** (`--mode=peer`): Direct participation as buyer or seller with hole punching
2. **Relay Mode** (`--mode=relay`): Acts as intermediary for NAT-constrained nodes (WIP)

## Message Flow (Buyer-Seller Interaction)

1. Buyer discovers sellers via Explorer API or environment variable list
2. Buyer sends `NeuronServiceRequestMsg` to seller's Hedera `stdIn` topic
3. Seller receives request, establishes P2P stream via libp2p
4. Seller periodically sends `NeuronScheduleSignRequestMsg` (payment requests)
5. Buyer signs scheduled transfers or handles errors
6. Both peers emit `NeuronHeartBeatMsg` to their `stdOut` topics
7. Errors are published as `NeuronPeerErrorMsg` or `NeuronSelfErrorMsg`

## Environment Configuration

Required variables in `.env`:
- `private_key`: secp256k1 private key (hex format)
- `hedera_evm_id`: EVM address of device account
- `hedera_id`: Hedera Account ID (e.g., `0.0.1234`)
- `location`: JSON with lat/lon/alt (e.g., `{"lat":50.1,"lon":1.8898,"alt":0.0}`)
- `list_of_sellers`: Comma-separated public keys (buyer only)
- `eth_rpc_url`: Hedera EVM endpoint (e.g., `https://testnet.hashio.io/api`)
- `mirror_api_url`: Hedera Mirror Node API (e.g., `https://testnet.mirrornode.hedera.com/api/v1`)
- `neuron_explorer_url`: Device discovery service (e.g., `https://explorer.neuron.world/api/v1/device/wip-all`)
- `smart_contract_address`: Hedera smart contract address (required at startup)

## Critical Implementation Notes

### Account Setup
Before using the SDK, create a Hedera account at [explorer.neuron.world](https://explorer.neuron.world). Each peer requires:
- **Device account**: Holds minimal tokens for HCS messages, has three topics (stdIn, stdOut, stdErr)
- **Parent account**: Receives data payments

### NAT Traversal
The SDK includes built-in NAT traversal:
- Hole punching for direct P2P connections
- UPnP support (enable with `--enable-upnp`)
- Manual IP/port override via `--my-public-ip` and `--my-public-port`
- QUIC/UDP transport (TCP not currently supported)

### Smart Contract Dependency
The SDK fails at startup if `smart_contract_address` is not set in the environment file. This contract is essential for peer discovery.

### Port Configuration
The `--port` flag is required and must not be `0` (random port assignment doesn't work yet).

### Protocol-Specific Callbacks
When implementing a DApp, the SDK only executes one role at runtime (buyer OR seller), but you define both callbacks upfront. The `--buyer-or-seller` flag determines which callbacks are used.

### Topic Listeners
The topic listener callbacks (`buyerCaseTopicListener`, `sellerCaseTopicListener`) must handle messages that the core SDK cannot process. The SDK handles connectivity-level messages (heartbeats, connection errors), while DApp-level messages are forwarded to your callbacks.

## Key Files to Understand First

1. [neuron-sdk.go](neuron-sdk.go): Entry point with `LaunchSDK()`
2. [common-lib/hederaMessages.go](common-lib/hederaMessages.go): Message type definitions
3. [dapp-protocols/stream-buyer-vs-seller/buyer-case.go](dapp-protocols/stream-buyer-vs-seller/buyer-case.go): Buyer logic
4. [dapp-protocols/stream-buyer-vs-seller/seller-case.go](dapp-protocols/stream-buyer-vs-seller/seller-case.go): Seller logic
5. [common-lib/buffers.go](common-lib/buffers.go): State management
6. [hedera/main.go](hedera/main.go): Hedera client and topic operations

## Dependencies

**Major external dependencies**:
- `github.com/hashgraph/hedera-sdk-go/v2` - Hedera network SDK
- `github.com/libp2p/go-libp2p` - P2P networking framework
- `github.com/ethereum/go-ethereum` - EVM/smart contract interaction
- `github.com/joho/godotenv` - Environment file loading
- `github.com/spf13/pflag` - Command-line flag parsing
- `github.com/pion/*` - WebRTC/STUN/DTLS stack for NAT detection

## Common Flags

**General**:
- `--mode`: `peer` or `relay` (default: `peer`)
- `--buyer-or-seller`: `buyer` or `seller` (required)
- `--port`: Bind port (required, no random ports)
- `--force-protocol`: `udp` (only UDP/QUIC supported)
- `--enable-upnp`: Enable UPnP port forwarding
- `--envFile`: Path to `.env` file (default: `.env`)

**Buyer-specific**:
- `--list-of-sellers-source`: `explorer` or `env` (default: `explorer`)
- `--radius`: Search radius in km when using explorer (default: `1`)

**Override**:
- `--force-location`: JSON location override
- `--my-public-ip`, `--my-public-port`: Manual public address
- `--clear-cache`: Clear cached data

## Testing Strategy

Tests are located in package subdirectories:
- `/hedera`: HTTP client, RPC, and main Hedera functionality
- `/keylib`: Key conversion and exchange
- `/whoami`: NAT detection
- `/upnp`: UPnP port forwarding

Use standard Go testing patterns. The codebase currently has 7 test files covering critical networking and cryptographic operations.

## Work in Progress

- **Validator role**: Placeholder implementation in [/validator-lib](validator-lib/)
- **Relay mode**: Basic structure exists but not fully functional
- **SLA validation**: Currently hardcoded during Beta
- **Random port binding**: `--port=0` doesn't work yet
