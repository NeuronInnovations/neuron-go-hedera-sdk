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

| File                                                   | Changes                                                                           |
| ------------------------------------------------------ | --------------------------------------------------------------------------------- |
| `common-lib/connection.go`                             | Added `AttemptReconnectFromCache`, `parseMultiaddr`, `splitAddresses`             |
| `common-lib/buffers.go`                                | Added invoice queue, cache reconnect tracking fields and methods                  |
| `common-lib/state-types.go`                            | Updated serialization for new fields                                              |
| `types/connection.go`                                  | Added `ReconnectingFromCache` state                                               |
| `neuron-sdk.go`                                        | Upgraded `DisconnectedF` handler                                                  |
| `dapp-protocols/stream-buyer-vs-seller/buyer-case.go`  | Added `tryReconnectFromBBolt`, optimized SharedAccID validation                   |
| `dapp-protocols/stream-buyer-vs-seller/seller-case.go` | Added `sellerTryReconnectFromBBolt`                                               |
| `hedera/main.go`                                       | Added invoice queueing, network error detection, flush worker, `SanitizeRPCError` |
| `hedera/rpc.go`                                        | Updated error logging to use sanitized errors, detect HTML error pages            |

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

### 9. RPC Error Sanitization

**File:** `hedera/main.go`

When RPC providers like hashio experience backend issues, Cloudflare returns HTML error pages instead of JSON responses. These HTML pages pollute logs and make debugging difficult.

#### isNetworkError Enhancement

Extended to detect Cloudflare/RPC HTML error pages:

```go
func isNetworkError(err error) bool {
    // Standard network patterns
    networkPatterns := []string{
        "connection refused", "timeout", "eof", "unavailable", ...
    }

    // Cloudflare/RPC HTML error page patterns
    htmlErrorPatterns := []string{
        "<!doctype html>",       // HTML error page marker
        "bad gateway",           // 502 error
        "service temporarily",   // 503 error
        "gateway timeout",       // 504 error
        "cloudflare",            // Cloudflare proxy errors
        "502:", "503:", "504:",  // HTTP status codes
        "origin is unreachable", // Cloudflare 523
        "web server is down",    // Cloudflare 521
    }
    // ... check patterns ...
}
```

#### SanitizeRPCError Function

Converts verbose HTML error pages into clean, human-readable messages:

```go
func SanitizeRPCError(err error) string
```

| Error Type              | Sanitized Message                                                 |
| ----------------------- | ----------------------------------------------------------------- |
| 502 Bad Gateway + HTML  | "RPC unavailable (502 Bad Gateway - backend server down)"         |
| 503 Service Unavailable | "RPC unavailable (503 Service Unavailable - backend overloaded)"  |
| 504 Gateway Timeout     | "RPC unavailable (504 Gateway Timeout - backend not responding)"  |
| Cloudflare 521          | "RPC unavailable (521 Web Server Down - origin offline)"          |
| Cloudflare 522          | "RPC unavailable (522 Connection Timed Out - origin unreachable)" |
| Cloudflare 523          | "RPC unavailable (523 Origin Unreachable - DNS or routing issue)" |
| Generic HTML error      | "RPC unavailable (HTML error page received instead of JSON)"      |
| Connection refused      | "RPC unavailable (connection refused - server not listening)"     |
| Timeout                 | "RPC unavailable (request timeout)"                               |

**File:** `hedera/rpc.go`

All blockchain query error logs now use `SanitizeRPCError()`:

```go
// Before
log.Printf("Blockchain query failed: %v", err)  // Dumps full HTML page

// After
log.Printf("Blockchain query failed: %s", SanitizeRPCError(err))  // Clean message
```

**Impact:**

- Logs are now readable during RPC outages
- HTML pollution eliminated from error chains
- Network errors properly detected for cache fallback

---

### 10. Database Interrogation API

**File:** `common-lib/state-types.go`, `common-lib/state-persistence.go`

A public API for SDK consumers to interrogate the running BBolt database state without requiring HTTP/WebSocket wrappers.

#### Types

```go
// DatabaseSnapshot represents a complete snapshot of the BBolt database state
type DatabaseSnapshot struct {
    Peers           map[string]*SerializedNodeBufferInfo `json:"peers"`
    Topics          map[string]time.Time                 `json:"topics"`
    CachedPeerList  *CachedPeerList                      `json:"cached_peer_list,omitempty"`
    CachedPeerInfos map[string]*CachedPeerInfo           `json:"cached_peer_infos,omitempty"`
    Stats           map[string]interface{}               `json:"stats"`
    ExportedAt      time.Time                            `json:"exported_at"`
}

// InvoiceQueueStatus provides status of the pending invoice queue
type InvoiceQueueStatus struct {
    Count      int       `json:"count"`
    OldestTime time.Time `json:"oldest_time,omitempty"`
    NewestTime time.Time `json:"newest_time,omitempty"`
}
```

#### Public Functions

| Function                           | Purpose                                                                      |
| ---------------------------------- | ---------------------------------------------------------------------------- |
| `DumpDatabaseState()`              | Returns complete database snapshot (peers, topics, cached peer infos, stats) |
| `GetDatabaseStats()`               | Returns database statistics for monitoring                                   |
| `GetAllPeerStates()`               | Returns all persisted peer states keyed by peer ID                           |
| `GetPeerStateByID(peerIDStr)`      | Returns state for a specific peer by ID string                               |
| `IsDatabaseHealthy()`              | Health check - returns true if DB is operational                             |
| `GetInvoiceQueueStatus()`          | Returns pending invoice queue status                                         |
| `ToSerializedNodeBufferInfo(info)` | Converts NodeBufferInfo to serializable form                                 |

---

## Testing & Usage

### How to Use the Database Interrogation API

SDK consumers can interrogate the running BBolt database by importing the common-lib package:

```go
import commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
```

#### 1. Full Database Dump

Get a complete snapshot of all database state:

```go
snapshot, err := commonlib.DumpDatabaseState()
if err != nil {
    log.Printf("Failed to dump database: %v", err)
    return
}

// Pretty-print as JSON
jsonBytes, _ := json.MarshalIndent(snapshot, "", "  ")
fmt.Println(string(jsonBytes))

// Access specific data
fmt.Printf("Total peers: %d\n", len(snapshot.Peers))
fmt.Printf("Total topics: %d\n", len(snapshot.Topics))
fmt.Printf("Exported at: %v\n", snapshot.ExportedAt)
```

#### 2. Quick Stats Check

Monitor database health and performance:

```go
stats := commonlib.GetDatabaseStats()

fmt.Printf("Degraded mode: %v\n", stats["degraded_mode"])
fmt.Printf("Writes dropped: %v\n", stats["writes_dropped"])
fmt.Printf("Corrupted records: %v\n", stats["corrupted_records_count"])
fmt.Printf("Write queue length: %v\n", stats["write_queue_length"])
fmt.Printf("Snapshot count: %v\n", stats["snapshot_count"])
```

#### 3. Query All Peers

Iterate over all known peers and their cached data:

```go
peers, err := commonlib.GetAllPeerStates()
if err != nil {
    log.Printf("Failed to get peers: %v", err)
    return
}

for peerID, state := range peers {
    fmt.Printf("Peer: %s\n", peerID)
    fmt.Printf("  SharedAccID: %d\n", state.SharedAccID)
    fmt.Printf("  IP: %s\n", state.LastOtherSideMultiAddress)
    fmt.Printf("  State: %s\n", state.LibP2PState)
    fmt.Printf("  Cache reconnect attempts: %d\n", state.CacheReconnectAttempts)
}
```

#### 4. Query Specific Peer

Look up a specific peer by ID string:

```go
peerIDStr := "16Uiu2HAmRweAijoixB48FtgLGyrdMWxYXS1Zxf91T5dHt6ugGDMY"
state, err := commonlib.GetPeerStateByID(peerIDStr)
if err != nil {
    log.Printf("Peer not found: %v", err)
    return
}

fmt.Printf("SharedAccID: %d\n", state.SharedAccID)
fmt.Printf("Last IP: %s\n", state.LastOtherSideMultiAddress)
fmt.Printf("Last active: %v\n", state.LastGoodsReceivedTime)
```

#### 5. Health Check

Check if the database is operational:

```go
if !commonlib.IsDatabaseHealthy() {
    log.Warn("Database is in degraded mode!")
    // Take appropriate action (e.g., alert, fallback behavior)
}
```

#### 6. Monitor Invoice Queue

Check the status of pending Hedera invoices:

```go
status := commonlib.GetInvoiceQueueStatus()

fmt.Printf("Pending invoices: %d\n", status.Count)
if status.Count > 0 {
    fmt.Printf("Oldest queued: %v\n", status.OldestTime)
    fmt.Printf("Newest queued: %v\n", status.NewestTime)

    // Calculate queue age
    queueAge := time.Since(status.OldestTime)
    if queueAge > 10*time.Minute {
        log.Warn("Hedera connectivity issue - invoices queued for %v", queueAge)
    }
}
```

### Integration Testing Example

Create a simple test wrapper to verify the system is working:

```go
func TestDatabaseInterrogation(t *testing.T) {
    // Prerequisite: SDK must be running with GlobalStateManager initialized

    // Test 1: Health check
    if !commonlib.IsDatabaseHealthy() {
        t.Fatal("Database should be healthy")
    }

    // Test 2: Stats should return data
    stats := commonlib.GetDatabaseStats()
    if stats["error"] != nil {
        t.Fatalf("Unexpected error: %v", stats["error"])
    }

    // Test 3: Dump should succeed
    snapshot, err := commonlib.DumpDatabaseState()
    if err != nil {
        t.Fatalf("DumpDatabaseState failed: %v", err)
    }

    // Verify snapshot structure
    if snapshot.ExportedAt.IsZero() {
        t.Error("ExportedAt should be set")
    }

    // Test 4: Invoice queue should be accessible
    status := commonlib.GetInvoiceQueueStatus()
    if status.Count < 0 {
        t.Error("Invoice count should be non-negative")
    }

    t.Logf("Database state: %d peers, %d topics, %d pending invoices",
        len(snapshot.Peers), len(snapshot.Topics), status.Count)
}
```

### Debugging Tips

1. **Check persistence is working:**

   ```go
   stats := commonlib.GetDatabaseStats()
   if stats["degraded_mode"].(bool) {
       log.Error("Persistence is failing - check disk space and permissions")
   }
   ```

2. **Verify SharedAccID caching:**

   ```go
   peers, _ := commonlib.GetAllPeerStates()
   for id, p := range peers {
       if p.SharedAccID > 0 && !p.SharedAccIDCreatedAt.IsZero() {
           age := time.Since(p.SharedAccIDCreatedAt)
           log.Printf("Peer %s: SharedAccID=%d (cached %v ago)", id, p.SharedAccID, age)
       }
   }
   ```

3. **Monitor reconnection attempts:**

   ```go
   peers, _ := commonlib.GetAllPeerStates()
   for id, p := range peers {
       if p.CacheReconnectAttempts > 0 {
           log.Printf("Peer %s: %d cache reconnect attempts, last at %v",
               id, p.CacheReconnectAttempts, p.LastCacheReconnectTime)
       }
   }
   ```

4. **Export full state for analysis:**
   ```go
   snapshot, _ := commonlib.DumpDatabaseState()
   jsonBytes, _ := json.MarshalIndent(snapshot, "", "  ")
   os.WriteFile("/tmp/neuron-db-snapshot.json", jsonBytes, 0644)
   log.Printf("Database snapshot saved to /tmp/neuron-db-snapshot.json")
   ```

---

## Future Improvements

1. **Per-peer reconnection lock:** Prevent parallel reconnection attempts to same peer
2. **Invoice deduplication:** Prevent same invoice being queued multiple times
3. **Metrics collection:** Track cache hit/miss rates for monitoring
4. **Configurable timeouts:** Allow runtime configuration of retry parameters
