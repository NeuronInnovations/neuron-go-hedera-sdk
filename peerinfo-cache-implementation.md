# PeerInfo Cache Implementation

## Overview

This document describes the implementation of a blockchain-first, local-fallback caching strategy for PeerInfo data in the Neuron SDK. The feature enables the system to continue operating when the Hedera blockchain is temporarily unavailable by falling back to locally cached data stored in bbolt.

## Problem Statement

The Neuron SDK relies on querying the Hedera blockchain to retrieve peer information (topics, availability status, peer IDs). This creates several failure scenarios:

1. **Network Outages**: Blockchain RPC endpoints may be unreachable
2. **Rate Limiting**: Hedera may rate-limit excessive queries
3. **Insufficient Funds**: Queries may fail if the account lacks HBAR
4. **Temporary Downtime**: Blockchain maintenance or congestion

Previously, any of these failures would cause the node to fail completely. The cache implementation provides resilience by storing successful blockchain queries locally.

## Architecture

### Data Flow

```
GetPeerInfo(evmAddress)
        |
        v
[1. Try Blockchain]-----(success)-----> Cache result --> Return
        |
    (failure)
        |
        v
[2. Try Local Cache]----(hit)--------> Return cached data
        |
    (miss/corrupted)
        |
        v
[3. Return Error] with context about both failures
```

### Components

| Component       | File                              | Purpose                              |
| --------------- | --------------------------------- | ------------------------------------ |
| CachedPeerInfo  | `common-lib/state-types.go`       | Data structure for cached peer data  |
| CachedPeerList  | `common-lib/state-types.go`       | Data structure for cached peer list  |
| PersistPeerInfo | `common-lib/state-persistence.go` | Write cache entry to bbolt           |
| LoadPeerInfo    | `common-lib/state-persistence.go` | Read cache entry from bbolt          |
| GetPeerInfo     | `hedera/rpc.go`                   | Main entry point with fallback logic |
| GetAllPeers     | `hedera/rpc.go`                   | Peer list with fallback logic        |

## Data Structures

### CachedPeerInfo

```go
type CachedPeerInfo struct {
    Available   bool      `json:"available"`
    PeerID      string    `json:"peer_id"`
    StdOutTopic uint64    `json:"std_out_topic"`
    StdInTopic  uint64    `json:"std_in_topic"`
    StdErrTopic uint64    `json:"std_err_topic"`
    CachedAt    time.Time `json:"cached_at"`
}
```

### CachedPeerList

```go
type CachedPeerList struct {
    Addresses []string  `json:"addresses"`
    CachedAt  time.Time `json:"cached_at"`
}
```

### Serialization Format

All cached data uses a checksummed binary format for data integrity:

```
[version: 1 byte][crc32: 4 bytes][json: N bytes]
```

- **Version byte**: Enables future format migrations
- **CRC32 checksum**: Detects SD card corruption and bit flips
- **JSON payload**: The serialized data structure

## Storage

### bbolt Bucket

Cache entries are stored in the `peer_info_cache` bucket within the existing bbolt database at `~/.neuron/state.db`.

| Key            | Value                     |
| -------------- | ------------------------- |
| `{evmAddress}` | Serialized CachedPeerInfo |
| `_peer_list_`  | Serialized CachedPeerList |

### Storage Overhead

| Scale          | Approximate Size |
| -------------- | ---------------- |
| Per peer entry | ~212 bytes       |
| 100 peers      | ~20 KB           |
| 1000 peers     | ~207 KB          |

## Cache TTL (Time-To-Live)

The default TTL is 24 hours, defined in `state-types.go`:

```go
const DefaultPeerInfoCacheTTL = 24 * time.Hour
```

### Staleness Behavior

| Scenario                         | Behavior                         |
| -------------------------------- | -------------------------------- |
| Cache age < 24h                  | Use cache silently               |
| Cache age > 24h, blockchain up   | Prefer fresh blockchain data     |
| Cache age > 24h, blockchain down | Use stale cache with warning log |

## Implementation Details

### GetPeerInfo Function

The main entry point implements the following logic:

```go
func GetPeerInfo(hederaAccEvmAddress string) (PeerInfo, error) {
    // 1. Try blockchain with reduced retries (3 instead of 25)
    peerInfo, err := getPeerInfoFromBlockchain(hederaAccEvmAddress, 3)
    if err == nil {
        // Success - cache for future fallback
        commonlib.GlobalStateManager.PersistPeerInfo(hederaAccEvmAddress, cachedInfo)
        return peerInfo, nil
    }

    // 2. Fallback to cached data
    cached, cacheErr := commonlib.GlobalStateManager.LoadPeerInfo(hederaAccEvmAddress)
    if cacheErr == nil {
        // Log staleness warning if applicable
        return convertToPeerInfo(cached), nil
    }

    // 3. Both failed
    return PeerInfo{}, fmt.Errorf("blockchain unavailable and no valid cache: %w", err)
}
```

### Retry Configuration

The blockchain query uses exponential backoff with reduced retries:

| Retry     | Delay          |
| --------- | -------------- |
| 1st       | 1 second       |
| 2nd       | 2 seconds      |
| 3rd       | 4 seconds      |
| **Total** | **~7 seconds** |

This reduction from 25 retries enables faster fallback to cache while still handling transient failures.

### Write Behavior

Cache writes are batched (non-blocking) since they are not critical for immediate persistence:

```go
func (sm *StateManager) PersistPeerInfo(evmAddress string, info *CachedPeerInfo) {
    write := PeerInfoCacheWrite{
        EvmAddress: evmAddress,
        Info:       info,
    }

    select {
    case sm.writeQueue <- write:
        // Queued successfully
    default:
        log.Printf("Write queue full, dropping PeerInfo cache write for %s", evmAddress)
    }
}
```

## Error Handling

### Graceful Degradation

The implementation handles several failure modes:

| Failure Mode                   | Behavior                                             |
| ------------------------------ | ---------------------------------------------------- |
| GlobalStateManager is nil      | Skip caching, return blockchain result only          |
| Database in degraded mode      | Return error from LoadPeerInfo                       |
| Checksum mismatch (corruption) | Increment counter, return error, treat as cache miss |
| Bucket not found               | Return error, treat as cache miss                    |

### Error Messages

Error messages include context about both the blockchain failure and cache lookup:

```
blockchain unavailable and no valid cache for 0x... [contract: 0x...]: original error
```

## Initialization Order

The StateManager must be initialized before any blockchain queries that use the cache. The SDK ensures this:

1. **Line 174**: `GlobalStateManager = stateManager` (initialization)
2. **Line 581**: `hederaAnnounceAndHeartBeat()` (first blockchain query)

All code paths check for nil GlobalStateManager before attempting cache operations.

## Backward Compatibility

### Legacy Format Support

The deserialization functions support data without checksums for backward compatibility:

```go
func DeserializeCachedPeerInfo(data []byte) (*CachedPeerInfo, error) {
    if version != serializationVersion {
        // Try legacy format (plain JSON)
        return deserializeCachedPeerInfoLegacy(data)
    }
    // ... checksum validation and deserialization
}
```

### Database Migration

The new `peer_info_cache` bucket is created automatically during database initialization. Existing databases continue to work without modification.

## Known Limitations

### First Boot with Blockchain Down

If the node starts for the first time (empty cache) and the blockchain is unavailable, the node will fail to start. This is expected behavior since peer discovery requires at least one successful blockchain query.

### Stale Data Risks

If a peer changes their availability status or topics on the blockchain while the cache is stale and the blockchain is down, the node will use outdated information. This may result in:

- Failed connection attempts to unavailable peers
- Messages sent to incorrect topics

These failures are graceful (connection errors) and do not cause data corruption.

## Testing

### Unit Tests Required

The following test cases should be implemented:

1. `TestSerializeCachedPeerInfo_RoundTrip` - Verify serialization/deserialization
2. `TestDeserializeCachedPeerInfo_CorruptionDetection` - Verify CRC32 catches bit flips
3. `TestIsCacheStale_Fresh` - Verify TTL logic for fresh data
4. `TestIsCacheStale_Expired` - Verify TTL logic for expired data
5. `TestGetPeerInfo_BlockchainSuccess` - Verify caching on success
6. `TestGetPeerInfo_FallbackToCache` - Verify fallback behavior

### Manual Testing Procedure

1. Start node with network connectivity
2. Verify successful blockchain query (check logs)
3. Verify cache file exists: `~/.neuron/state.db`
4. Block network access to Hedera endpoints
5. Restart node
6. Verify cache fallback (look for "Using cached PeerInfo" in logs)

## Configuration

### Constants

| Constant                  | Value               | Location                          |
| ------------------------- | ------------------- | --------------------------------- |
| `DefaultPeerInfoCacheTTL` | 24 hours            | `common-lib/state-types.go`       |
| `bucketPeerInfoCache`     | `"peer_info_cache"` | `common-lib/state-persistence.go` |
| `peerListCacheKey`        | `"_peer_list_"`     | `common-lib/state-persistence.go` |

### Modifying TTL

To change the cache TTL, update the constant in `state-types.go`:

```go
const DefaultPeerInfoCacheTTL = 12 * time.Hour // Example: 12 hours
```

## Logging

The implementation uses consistent log prefixes for debugging:

| Log Pattern                                           | Meaning                                 |
| ----------------------------------------------------- | --------------------------------------- |
| `Blockchain query failed...attempting cache fallback` | Blockchain failed, trying cache         |
| `Using cached PeerInfo for...`                        | Cache hit with fresh data               |
| `Cache for...is stale...but using anyway`             | Cache hit with stale data               |
| `Cache lookup also failed`                            | Both blockchain and cache failed        |
| `Write queue full, dropping...`                       | Cache write dropped due to backpressure |

## Related Documentation

- `database-corruption-prevention.md` - Details on bbolt corruption prevention
- `bbolt-ip-and-shared-account-flow.md` - Related persistence flows

## Changelog

| Version | Date       | Changes                |
| ------- | ---------- | ---------------------- |
| 1.0.0   | 2025-11-25 | Initial implementation |
