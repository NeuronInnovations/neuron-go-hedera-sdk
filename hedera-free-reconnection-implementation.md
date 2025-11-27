# Hedera-Free Reconnection Implementation

## Overview

This document describes the technical implementation of the Hedera-Free Reconnection system, which enables peers to reconnect after network disruptions without requiring Hedera blockchain communication. The implementation leverages the existing BBolt persistence layer to cache connection state, IP addresses, and SharedAccIDs.

## Problem Statement

Prior to this implementation, reconnection after network disruption required 7 conditions to be true:

1. Hedera blockchain is accessible
2. Buyer has sufficient HBAR for topic messages
3. Seller has sufficient HBAR for topic messages
4. Seller is actively heartbeating
5. Network path to Hedera is available
6. Smart contract is responsive
7. Topic subscriptions are active

This "conjuncts problem" caused the 4dsky application to stop showing planes after network disruptions because the probability of all conditions being true simultaneously was low.

## Solution Architecture

The solution uses cached connection data from BBolt to attempt direct P2P reconnection before falling back to Hedera-based rendezvous.

```
Connection Drops
      |
      v
DisconnectedF triggered
      |
      v
Retrieve cached IP from BBolt (LastOtherSideMultiAddress)
      |
      v
AttemptReconnectFromCache (3 retries, exponential backoff)
      |
      +---> Success: Resume streaming, no Hedera needed
      |
      +---> Failure: Mark for Hedera reconnection on next cycle
```

---

## Implementation Details

### 1. Core Reconnection Function

**File:** `common-lib/connection.go`

#### AttemptReconnectFromCache

```go
func AttemptReconnectFromCache(ctx context.Context, p2pHost host.Host, peerID peer.ID,
    buffers *NodeBuffers, protocolID protocol.ID) error
```

**Purpose:** Attempts to reconnect to a peer using cached IP addresses from BBolt persistence.

**Algorithm:**

1. Validate input parameters (buffers not nil)
2. Retrieve cached buffer info for the peer
3. Extract `LastOtherSideMultiAddress` from buffer
4. Parse multiaddress string into libp2p multiaddr format
5. Execute retry loop with exponential backoff:
   - Attempt 1: Wait 0s, try connection with 10s timeout
   - Attempt 2: Wait 1s, try connection with 10s timeout
   - Attempt 3: Wait 2s, try connection with 10s timeout
6. On success: Update state to `Connected`, reset cache reconnect attempts
7. On failure: Update state to `ConnectionLost`, return error

**Retry Configuration:**

- Maximum retries: 3
- Base backoff: 1 second
- Backoff multiplier: 2^(attempt-1) (1s, 2s, 4s)
- Connection timeout per attempt: 10 seconds

#### parseMultiaddr

```go
func parseMultiaddr(addrStr string) ([]multiaddr.Multiaddr, error)
```

Parses cached multiaddress strings which may contain single or space-separated addresses.

#### splitAddresses

```go
func splitAddresses(s string) []string
```

Handles multiple address formats:

- Single address: `/ip4/1.2.3.4/udp/1234/quic-v1`
- Space-separated: `/ip4/1.2.3.4/udp/1234 /ip4/5.6.7.8/udp/5678`

---

### 2. Connection Event Handler

**File:** `neuron-sdk.go`

#### DisconnectedF Handler Upgrade

The `DisconnectedF` callback in `network.NotifyBundle` was upgraded from logging-only to active reconnection:

```go
DisconnectedF: func(n network.Network, c network.Conn) {
    peerID := c.RemotePeer()

    // Validate buffer exists for this peer
    _, exists := commonlib.NodeBuffersInstance.GetBuffer(peerID)
    if !exists {
        return // No relationship with this peer
    }

    // Spawn reconnection goroutine
    go func() {
        time.Sleep(500 * time.Millisecond) // Avoid reconnect during intentional disconnect

        if n.Connectedness(peerID) == network.Connected {
            return // Already reconnected
        }

        err := commonlib.AttemptReconnectFromCache(ctx, p2pHost, peerID,
            commonlib.NodeBuffersInstance, protocol)
        if err != nil {
            commonlib.NodeBuffersInstance.UpdateBufferLibP2PState(peerID, types.ConnectionLost)
        }
    }()
}
```

**Design Decisions:**

- 500ms delay prevents reconnection during intentional disconnects
- Goroutine prevents blocking the network event loop
- Connectedness check prevents redundant attempts if peer already reconnected

---

### 3. Seller-Side BBolt Reconnection

**File:** `dapp-protocols/stream-buyer-vs-seller/seller-case.go`

#### sellerTryReconnectFromBBolt

```go
func sellerTryReconnectFromBBolt(ctx context.Context, p2pHost host.Host,
    buyerBuffers *commonlib.NodeBuffers, protocol protocol.ID)
```

**Purpose:** On seller startup, attempt to reconnect to all previously connected buyers using cached IPs.

**Algorithm:**

1. Get thread-safe copy of buffer map via `GetBufferMap()`
2. For each buyer with valid `LastOtherSideMultiAddress` and `SharedAccID`:
   - Spawn goroutine to attempt reconnection
   - Use `AttemptReconnectFromCache` for connection
   - On success, open stream to buyer

**Integration Point:**
Called at the beginning of `HandleSellerCase` before Hedera topic listener starts:

```go
if commonlib.GlobalStateManager != nil && !commonlib.GlobalStateManager.IsInDegradedMode() {
    sellerTryReconnectFromBBolt(ctx, p2pHost, buyerBuffers, protocol)
    time.Sleep(2 * time.Second) // Allow reconnection attempts to complete
}
```

---

### 4. Buyer-Side BBolt Reconnection

**File:** `dapp-protocols/stream-buyer-vs-seller/buyer-case.go`

#### tryReconnectFromBBolt

Mirrors seller implementation but for buyer context. Called at startup to reconnect to previously connected sellers.

**Key Difference:** Uses `strings.Fields` to parse space-separated multiaddresses and attempts each address individually.

---

### 5. Connection State Machine Enhancement

**File:** `types/connection.go`

Added new connection state:

```go
const (
    // ... existing states ...

    // Cache-based reconnection states (Hedera-free)
    ReconnectingFromCache ConnectionState = "ReconnectingFromCache"

    // ... hole punching states ...
)
```

**File:** `common-lib/buffers.go`

Added tracking fields to `NodeBufferInfo`:

```go
type NodeBufferInfo struct {
    // ... existing fields ...

    // Cache-based reconnection tracking (Hedera-free reconnection)
    CacheReconnectAttempts int       `json:"cache_reconnect_attempts"`
    LastCacheReconnectTime time.Time `json:"last_cache_reconnect_time"`
}
```

Added methods:

- `IncrementCacheReconnectAttempts(peerID peer.ID)` - Increments attempt counter
- `ResetCacheReconnectAttempts(peerID peer.ID)` - Resets counter on success
- `GetCacheReconnectInfo(peerID peer.ID)` - Returns current attempt info

---

### 6. Invoice Queueing System

**File:** `common-lib/buffers.go`

#### QueuedInvoice Structure

```go
type QueuedInvoice struct {
    PeerID      peer.ID   `json:"peer_id"`
    SharedAccID uint64    `json:"shared_acc_id"`
    Amount      float64   `json:"amount"`
    BuyerStdIn  uint64    `json:"buyer_std_in"`
    QueuedAt    time.Time `json:"queued_at"`
    RetryCount  int       `json:"retry_count"`
}
```

#### Queue Management

```go
var (
    PendingInvoices   []QueuedInvoice
    InvoiceQueueMutex sync.Mutex
)
```

Functions:

- `QueueInvoice(peerID, sharedAccID, amount, buyerStdIn)` - Add invoice to queue
- `GetPendingInvoices()` - Returns thread-safe copy of queue
- `ClearProcessedInvoices(indices)` - Removes processed invoices (sorted descending)
- `IncrementInvoiceRetry(index)` - Increments retry counter
- `GetPendingInvoiceCount()` - Returns queue length

**File:** `hedera/main.go`

#### SellerSendScheduledTransferRequest Modifications

Added network error detection and queueing:

```go
func isNetworkError(err error) bool {
    // Checks for: connection refused, timeout, dial tcp, EOF, UNAVAILABLE, etc.
}

func queueInvoiceForLater(sharedAccID uint64, amount float64, buyerStdIn uint64) {
    // Queues invoice for later retry
}
```

On network error, invoices are queued instead of failing:

```go
if isNetworkError(err) {
    queueInvoiceForLater(sharedAccID.Account, totalAmount, buyerStdIn.Topic)
    return nil // Continue streaming, don't fail
}
```

#### Invoice Flush Worker

```go
func StartInvoiceFlushWorker(stopChan <-chan struct{})
```

Background worker that:

- Runs every 60 seconds
- Attempts to send all queued invoices
- Removes successfully sent invoices
- Increments retry count for failed invoices
- Removes invoices after 5 failed attempts

```go
func FlushPendingInvoices() {
    invoices := commonlib.GetPendingInvoices()
    var processedIndices []int

    for i, invoice := range invoices {
        if invoice.RetryCount >= 5 {
            processedIndices = append(processedIndices, i)
            continue
        }

        err := retryInvoice(invoice)
        if err == nil {
            processedIndices = append(processedIndices, i)
        } else {
            commonlib.IncrementInvoiceRetry(i)
        }
    }

    commonlib.ClearProcessedInvoices(processedIndices)
}
```

---

### 7. SharedAccID Validation Optimization

**File:** `dapp-protocols/stream-buyer-vs-seller/buyer-case.go`

#### Cache Staleness Check

For cached reconnections, SharedAccID validation against Hedera blockchain is skipped if the cache is fresh (less than 24 hours old):

```go
if !commonlib.IsCacheStale(loadedBuffer.SharedAccIDCreatedAt, 24*time.Hour) {
    existingSharedAccID = loadedBuffer.SharedAccID
    // No blockchain validation needed - trust cached data
} else {
    // Cache is stale, validate against blockchain
    if isValid, _ := hedera_helper.ValidateSharedAccount(loadedBuffer.SharedAccID, 100); isValid {
        existingSharedAccID = loadedBuffer.SharedAccID
    }
}
```

This reduces Hedera API calls during reconnection scenarios.

---

### 8. Serialization Updates

**File:** `common-lib/state-types.go`

#### SerializedNodeBufferInfo

Added new fields for persistence:

```go
type SerializedNodeBufferInfo struct {
    // ... existing fields ...

    CacheReconnectAttempts int       `json:"cache_reconnect_attempts"`
    LastCacheReconnectTime time.Time `json:"last_cache_reconnect_time"`
}
```

Updated `SerializeNodeBufferInfo` and `buildNodeBufferInfoFromSerialized` to include new fields.

---

## Thread Safety

### Mutex Protection

| Resource            | Mutex        | Protected Operations |
| ------------------- | ------------ | -------------------- |
| NodeBuffers.Buffers | sync.RWMutex | All buffer access    |
| PendingInvoices     | sync.Mutex   | Queue operations     |

### Race Condition Fixes

1. **Buffer Map Iteration:** Both `tryReconnectFromBBolt` functions now use `GetBufferMap()` instead of direct map access to avoid iteration during modification.

2. **ClearProcessedInvoices:** Indices are now sorted in descending order before removal to maintain index validity:

```go
sort.Sort(sort.Reverse(sort.IntSlice(processedIndices)))
for _, idx := range processedIndices {
    if idx >= 0 && idx < len(PendingInvoices) {
        PendingInvoices = append(PendingInvoices[:idx], PendingInvoices[idx+1:]...)
    }
}
```

---

## Configuration Constants

| Constant                   | Value | Purpose                           |
| -------------------------- | ----- | --------------------------------- |
| Cache reconnection retries | 3     | Maximum attempts before fallback  |
| Base backoff               | 1s    | Initial retry delay               |
| Connection timeout         | 10s   | Per-attempt timeout               |
| Disconnect delay           | 500ms | Delay before reconnection attempt |
| SharedAccID cache TTL      | 24h   | Max age before revalidation       |
| Invoice flush interval     | 60s   | Background worker period          |
| Max invoice retries        | 5     | Attempts before removal           |

---

## Files Modified

| File                                                   | Changes                                                               |
| ------------------------------------------------------ | --------------------------------------------------------------------- |
| `common-lib/connection.go`                             | Added `AttemptReconnectFromCache`, `parseMultiaddr`, `splitAddresses` |
| `common-lib/buffers.go`                                | Added invoice queue, cache reconnect tracking fields and methods      |
| `common-lib/state-types.go`                            | Updated serialization for new fields                                  |
| `types/connection.go`                                  | Added `ReconnectingFromCache` state                                   |
| `neuron-sdk.go`                                        | Upgraded `DisconnectedF` handler                                      |
| `dapp-protocols/stream-buyer-vs-seller/buyer-case.go`  | Added `tryReconnectFromBBolt`, optimized SharedAccID validation       |
| `dapp-protocols/stream-buyer-vs-seller/seller-case.go` | Added `sellerTryReconnectFromBBolt`                                   |
| `hedera/main.go`                                       | Added invoice queueing, network error detection, flush worker         |

---

## Testing Verification

- Build: `go build ./...` passes
- Vet: `go vet ./...` passes
- Common-lib tests: All pass
- Linter: No errors in modified files

---

## Backward Compatibility

The implementation maintains backward compatibility:

1. **Serialization:** Legacy data without new fields deserializes correctly (zero values used)
2. **Fallback:** If cached reconnection fails, existing Hedera-based reconnection continues to work
3. **Degraded Mode:** If BBolt is unavailable, system operates as before

---

## Future Improvements

1. **Per-peer reconnection lock:** Prevent parallel reconnection attempts to same peer
2. **Invoice deduplication:** Prevent same invoice being queued multiple times
3. **Metrics collection:** Track cache hit/miss rates for monitoring
4. **Configurable timeouts:** Allow runtime configuration of retry parameters
