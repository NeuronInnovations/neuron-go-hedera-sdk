# feat(persistence): Add PeerInfo blockchain cache with corruption defense

## Summary

This PR implements two complementary resilience features for the Neuron SDK:

1. **PeerInfo Blockchain Cache**: A blockchain-first, local-fallback caching strategy for Hedera PeerInfo data. When blockchain queries fail (network issues, insufficient funds, or blockchain downtime), the system gracefully falls back to locally cached data stored in bbolt.

2. **Database Corruption Defense**: Comprehensive protection against data corruption from SD card failures and power loss, including CRC32 checksums, automatic snapshots, cascade recovery, and graceful degradation.

## Problem Statement

The Neuron SDK faces two critical reliability challenges:

1. **Blockchain Availability**: When the Hedera blockchain is unavailable, the application would fail to start or operate. Topics and peer information must be queryable even during blockchain outages.

2. **Data Corruption**: Running on embedded/IoT devices with SD cards exposes the application to bit-flip corruption from storage failures and data loss from unexpected power cycles. Without protection, corrupted state data could crash the application or cause undefined behavior.

## Changes

### `common-lib/state-types.go`

- Added `CachedPeerInfo` and `CachedPeerList` structs for blockchain data caching
- Implemented CRC32 checksummed serialization format: `[version:1byte][crc32:4bytes][json:Nbytes]`
- Added legacy format backward compatibility for pre-checksum data
- Added `IsCacheStale()` helper with configurable TTL (default: 24 hours)
- Added `DefaultPeerInfoCacheTTL` constant

### `common-lib/state-persistence.go`

- Added `peer_info_cache` bucket for blockchain data storage
- Implemented `PersistPeerInfo()` and `LoadPeerInfo()` for individual peer caching
- Implemented `PersistPeerList()` and `LoadPeerList()` for peer list caching
- Updated `initializeBuckets()` and `ClearAll()` to include new bucket
- Added `PeerInfoCacheWrite` and `PeerListCacheWrite` to batch write system

### `hedera/rpc.go`

- Refactored `GetPeerInfo()` with blockchain-first, cache-fallback pattern
- Reduced retry count from 25 to 3 for faster fallback (~7s vs ~50s)
- Refactored `GetAllPeers()` with same fallback pattern
- Extracted blockchain query logic into `getPeerInfoFromBlockchain()` and `getAllPeersFromBlockchain()`
- Added stale cache warning logs while still using stale data when blockchain unavailable

## Architecture

### Data Flow

```
GetPeerInfo(evmAddress)
    │
    ├──► Try blockchain (3 retries with exponential backoff)
    │       │
    │       ├──► Success: Cache result via PersistPeerInfo(), return data
    │       │
    │       └──► Failure: Attempt cache fallback
    │               │
    │               ├──► Cache hit (fresh < 24h): Return cached data
    │               ├──► Cache hit (stale > 24h): Log warning, return cached data
    │               └──► Cache miss: Return error with context
```

### Serialization Format

```
┌─────────┬──────────────┬─────────────────────┐
│ Version │   CRC32      │     JSON Payload    │
│ (1 byte)│  (4 bytes)   │     (N bytes)       │
└─────────┴──────────────┴─────────────────────┘
```

- **Version byte**: Currently `0x01`, enables future format changes
- **CRC32 checksum**: Detects bit-flip corruption from SD card failures
- **JSON payload**: Human-readable, debuggable data

### Database Structure

```
~/.neuron/
├── state.db
│   ├── peers           # libp2p peer connection state
│   ├── topics          # HCS topic positions
│   ├── metadata        # Shutdown timestamps, version info
│   └── peer_info_cache # NEW: Blockchain PeerInfo cache
└── snapshots/
    └── state-YYYYMMDD-HHMMSS.db  # Automatic backups (max 3)
```

## Corruption Defense

This PR includes comprehensive database corruption defense mechanisms designed for embedded/IoT environments where SD card failures and power loss are common.

### 1. CRC32 Checksums

Every serialized record includes a CRC32 checksum to detect bit-flip corruption:

```
[version:1byte][crc32:4bytes][json:Nbytes]
```

- On read: checksum is validated before deserializing
- On mismatch: `ErrChecksumMismatch` returned, record skipped
- Corrupted records are logged but don't crash the application

### 2. Automatic Snapshot System

```
┌─────────────────────────────────────────────────────┐
│                  Snapshot Lifecycle                  │
├─────────────────────────────────────────────────────┤
│  Every 10 minutes  ──►  CreateSnapshot()            │
│  On Close()        ──►  Final snapshot + metadata   │
│  Max 3 kept        ──►  Oldest auto-deleted         │
└─────────────────────────────────────────────────────┘
```

Snapshots are consistent copies created via bbolt's `tx.CopyFile()` within a read transaction.

### 3. Recovery Mechanism

On startup, if the main database is corrupted:

```
1. Detect corruption (bbolt.Open fails or integrity check fails)
2. Backup corrupted file as state.db.corrupted.TIMESTAMP
3. Try snapshots newest-to-oldest until one works
4. If all fail: create fresh database
5. Log recovery actions for debugging
```

### 4. Graceful Degradation

If persistence completely fails:

- `degradedMode = true`
- Writes silently dropped (no crashes)
- Loads return "database in degraded mode" error
- Background retry every 5 minutes
- Application continues operating with in-memory state

### 5. Signal Handling

Clean shutdown on SIGINT, SIGTERM, SIGHUP:

```go
1. FlushAll()     // Drain write queues
2. db.Sync()      // fsync to disk
3. CreateSnapshot // Final backup
4. Close()        // Release resources
```

Double-close protection via `sync.Once` prevents panics.

## Documentation

- `database-corruption-prevention.md` - StateManager architecture, snapshot system, recovery mechanisms
- `peerinfo-cache-implementation.md` - PeerInfo caching strategy, TTL logic, implementation details

## Checklist

- [x] CRC32 checksums for data integrity
- [x] Legacy format backward compatibility
- [x] Snapshot-based recovery system
- [x] Graceful degradation when persistence fails
- [x] Signal handling for clean shutdown (SIGINT, SIGTERM, SIGHUP)
- [x] Double-close protection with sync.Once
- [x] Comprehensive QA testing (145 tests)
- [x] Documentation added
- [x] No breaking changes
