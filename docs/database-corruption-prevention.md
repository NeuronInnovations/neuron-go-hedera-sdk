# Database Corruption Prevention Implementation

This document describes the technical measures implemented to prevent and recover from database corruption on SD cards and other flash storage media.

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Data Integrity Layer](#data-integrity-layer)
3. [Snapshot and Recovery System](#snapshot-and-recovery-system)
4. [Signal Handling and Graceful Shutdown](#signal-handling-and-graceful-shutdown)
5. [BoltDB Optimization for Flash Storage](#boltdb-optimization-for-flash-storage)
6. [Graceful Degradation](#graceful-degradation)
7. [File Changes Summary](#file-changes-summary)

---

## Problem Statement

SD cards and flash storage devices are susceptible to data corruption due to several factors:

1. **Power Loss During Writes**: Incomplete write operations can leave the database in an inconsistent state
2. **Bit-Flip Errors**: Flash memory cells can experience silent data corruption over time
3. **Wear Leveling Issues**: Excessive writes to the same sectors can cause premature failure
4. **Improper Shutdown**: Abrupt termination without flushing buffers leads to data loss

The BoltDB database used for state persistence (`~/.neuron/state.db`) stores critical data including peer IP addresses, shared account IDs, and connection states. Corruption of this database results in loss of peer reconnection capability and requires expensive Hedera network rediscovery.

---

## Data Integrity Layer

### Implementation Location

- File: `common-lib/state-types.go`

### Serialization Format

All `NodeBufferInfo` records are now serialized with a checksummed format:

```
+----------+------------+------------------+
| Version  | CRC32      | JSON Payload     |
| (1 byte) | (4 bytes)  | (N bytes)        |
+----------+------------+------------------+
```

- **Version Byte**: Indicates the serialization format version (currently `0x01`). Enables future format migrations without breaking existing data.
- **CRC32 Checksum**: IEEE polynomial CRC32 computed over the JSON payload. Detects bit-flip errors and partial writes.
- **JSON Payload**: The actual `SerializedNodeBufferInfo` structure containing peer state.

### Serialization Process

```go
func SerializeNodeBufferInfo(info *NodeBufferInfo) ([]byte, error) {
    // 1. Marshal to JSON
    jsonData, err := json.Marshal(serialized)

    // 2. Build result: [version][checksum][json]
    result := make([]byte, headerSize+len(jsonData))
    result[0] = serializationVersion
    checksum := crc32.ChecksumIEEE(jsonData)
    binary.BigEndian.PutUint32(result[versionSize:headerSize], checksum)
    copy(result[headerSize:], jsonData)

    return result, nil
}
```

### Deserialization and Validation

On read, the checksum is validated before returning data:

```go
func DeserializeNodeBufferInfo(data []byte) (*NodeBufferInfo, error) {
    // 1. Check minimum size
    if len(data) < headerSize + 2 {
        return deserializeLegacyFormat(data)  // Backward compatibility
    }

    // 2. Validate version
    if data[0] != serializationVersion {
        return deserializeLegacyFormat(data)
    }

    // 3. Extract and validate checksum
    storedChecksum := binary.BigEndian.Uint32(data[versionSize:headerSize])
    jsonData := data[headerSize:]
    calculatedChecksum := crc32.ChecksumIEEE(jsonData)

    if calculatedChecksum != storedChecksum {
        return nil, ErrChecksumMismatch  // Corruption detected
    }

    // 4. Deserialize JSON
    return unmarshalJSON(jsonData)
}
```

### Backward Compatibility

The deserialization function automatically detects legacy data (pre-checksum format) by checking:

1. Data length is too short for the new format
2. Version byte does not match (legacy JSON starts with `{` which is `0x7B`)

Legacy data is deserialized without checksum validation, then re-serialized with checksums on the next write.

### Error Types

```go
var (
    ErrChecksumMismatch = errors.New("data integrity check failed: checksum mismatch")
    ErrDataTooShort     = errors.New("data integrity check failed: data too short")
    ErrVersionMismatch  = errors.New("data format version not supported")
)
```

---

## Snapshot and Recovery System

### Implementation Location

- File: `common-lib/state-persistence.go`

### Snapshot Architecture

The `StateManager` maintains a snapshot directory alongside the main database:

```
~/.neuron/
├── state.db           # Primary database
└── snapshots/
    ├── state-20251125-142030.db
    ├── state-20251125-132030.db
    └── state-20251125-122030.db
```

### Snapshot Configuration

```go
const (
    defaultSnapshotInterval = 10 * time.Minute  // Create snapshot every 10 minutes
    defaultMaxSnapshots     = 3                  // Keep 3 most recent snapshots
)
```

### Snapshot Worker

A background goroutine creates periodic snapshots:

```go
func (sm *StateManager) snapshotWorker() {
    defer sm.wg.Done()
    ticker := time.NewTicker(sm.snapshotInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            sm.CreateSnapshot()
        case <-sm.stopChan:
            sm.CreateSnapshot()  // Final snapshot on shutdown
            return
        }
    }
}
```

### Snapshot Creation Process

1. **Generate Filename**: `state-YYYYMMDD-HHMMSS.db`
2. **Consistent Copy**: Uses BoltDB's `tx.CopyFile()` which creates a consistent point-in-time copy
3. **Atomic Rename**: Write to `.tmp` file first, then rename for crash safety
4. **Directory Sync**: Sync parent directory to ensure rename is durable
5. **Rotation**: Remove oldest snapshots exceeding `maxSnapshots`

```go
func (sm *StateManager) CreateSnapshot() error {
    sm.snapshotMutex.Lock()
    defer sm.snapshotMutex.Unlock()

    timestamp := time.Now().Format("20060102-150405")
    snapshotPath := filepath.Join(sm.snapshotDir, fmt.Sprintf("state-%s.db", timestamp))
    tempPath := snapshotPath + ".tmp"

    // Consistent copy using BoltDB transaction
    err := db.View(func(tx *bolt.Tx) error {
        return tx.CopyFile(tempPath, 0600)
    })

    // Atomic rename
    os.Rename(tempPath, snapshotPath)

    // Sync directory
    syncDirectory(sm.snapshotDir)

    // Rotate old snapshots
    sm.rotateSnapshots()
}
```

### Recovery Process

On database open failure, the system attempts recovery in this order:

1. **Try Snapshots (Newest First)**: Each snapshot is copied, opened, and validated
2. **Validate Integrity**: Check that all buckets can be iterated
3. **Backup Corrupted Database**: Rename to `state.db.corrupted.<timestamp>`
4. **Create Fresh Database**: Only if all snapshots fail

```go
func (sm *StateManager) attemptRecoveryFromSnapshots(dbPath string, originalErr error) (*bolt.DB, error) {
    snapshots, _ := sm.getSnapshotsSorted()  // Newest first

    for _, snapshot := range snapshots {
        // Copy snapshot to recovery path
        copyFileWithSync(snapshotPath, dbPath+".recovery")

        // Try to open
        db, err := bolt.Open(dbPath+".recovery", 0600, opts)
        if err != nil {
            continue  // Try next snapshot
        }

        // Validate integrity
        if err := validateDatabaseIntegrity(db); err != nil {
            db.Close()
            continue
        }

        // Success - finalize recovery
        db.Close()
        os.Rename(dbPath, dbPath+".corrupted."+timestamp)  // Backup corrupted
        os.Rename(dbPath+".recovery", dbPath)              // Install recovered
        syncDirectory(filepath.Dir(dbPath))

        return bolt.Open(dbPath, 0600, opts)
    }

    // All snapshots failed - create fresh database
    return createFreshDatabaseWithBackup(dbPath, originalErr)
}
```

### Integrity Validation

```go
func validateDatabaseIntegrity(db *bolt.DB) error {
    return db.View(func(tx *bolt.Tx) error {
        return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
            return b.ForEach(func(k, v []byte) error {
                return nil  // Iteration success indicates no corruption
            })
        })
    })
}
```

---

## Signal Handling and Graceful Shutdown

### Implementation Location

- File: `common-lib/state-persistence.go`
- File: `neuron-sdk.go`

### Signal Handler Setup

The `StateManager` installs signal handlers on initialization:

```go
func (sm *StateManager) setupSignalHandlers() {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan,
        syscall.SIGINT,   // Ctrl+C
        syscall.SIGTERM,  // kill command
        syscall.SIGHUP,   // Terminal hangup
    )

    go func() {
        sig := <-sigChan

        // Mark as closed to prevent new writes
        sm.closed.Store(true)

        // Emergency flush
        if !degraded && db != nil {
            sm.FlushAll()
            db.Sync()
        }

        // Re-raise signal for default handling
        signal.Reset(sig)
        syscall.Kill(syscall.Getpid(), sig.(syscall.Signal))
    }()
}
```

### Graceful Shutdown Sequence

The `neuron-sdk.go` shutdown sequence performs:

1. **Create Final Snapshot**: Ensures latest state is recoverable
2. **Flush Pending Writes**: Drain write queues and execute
3. **Record Metadata**: Store shutdown timestamp and reason
4. **Log Corruption Stats**: Report any detected corrupted records
5. **Close Database**: Proper cleanup

```go
defer func() {
    if stateManager != nil {
        // 1. Final snapshot
        stateManager.CreateSnapshot()

        // 2. Flush writes
        stateManager.FlushAll()

        // 3. Record metadata
        stateManager.PersistMetadata("last_shutdown", time.Now().Format(time.RFC3339Nano))
        stateManager.PersistMetadata("shutdown_reason", "graceful")

        // 4. Log corruption stats
        if count := stateManager.GetCorruptedRecordsCount(); count > 0 {
            log.Printf("Session detected %d corrupted records", count)
        }

        // 5. Close
        stateManager.Close()
    }
}()
```

### Flush Implementation

The `FlushAll()` method drains both write queues:

```go
func (sm *StateManager) FlushAll() error {
    batch := make([]interface{}, 0)

    // Drain batched queue (non-blocking)
    for {
        select {
        case write := <-sm.writeQueue:
            batch = append(batch, write)
        default:
            goto drainImmediate
        }
    }

drainImmediate:
    // Drain immediate queue (non-blocking)
    for {
        select {
        case write := <-sm.immediateQueue:
            batch = append(batch, write)
        default:
            goto execute
        }
    }

execute:
    if len(batch) > 0 {
        sm.executeBatch(batch)
    }

    // Final sync to disk
    return sm.db.Sync()
}
```

---

## BoltDB Optimization for Flash Storage

### Implementation Location

- File: `common-lib/state-persistence.go`

### Database Options

```go
db, err := bolt.Open(dbPath, 0600, &bolt.Options{
    Timeout:      10 * time.Second,
    NoGrowSync:   true,
    FreelistType: bolt.FreelistMapType,
})
```

### Option Explanations

**Timeout (10 seconds)**

- Maximum time to wait for database file lock
- Prevents indefinite blocking if another process holds the lock
- Production-appropriate value for most scenarios

**NoGrowSync (true)**

- Skips `fsync` when the database file grows
- Reduces write amplification on flash storage
- Growth operations are still durable after next regular sync
- Significantly reduces SD card wear for databases that grow frequently

**FreelistType (FreelistMapType)**

- Uses hashmap-based freelist instead of array-based
- Better performance for databases with frequent updates
- Reduces memory usage for large freelists
- More efficient page allocation/deallocation

### Directory Sync for Atomic Operations

After file renames (snapshots, recovery), the directory is synced:

```go
func syncDirectory(dirPath string) error {
    dir, err := os.Open(dirPath)
    if err != nil {
        return err
    }
    defer dir.Close()
    return dir.Sync()
}
```

This ensures that directory entry updates (file renames) are persisted to disk, making the atomic rename truly durable.

### Copy with Sync

File copies use explicit sync for durability:

```go
func copyFileWithSync(src, dst string) error {
    sourceFile, _ := os.Open(src)
    defer sourceFile.Close()

    destFile, _ := os.Create(dst)
    defer destFile.Close()

    io.Copy(destFile, sourceFile)

    return destFile.Sync()  // Ensure data reaches disk
}
```

---

## Graceful Degradation

### Implementation Location

- File: `common-lib/state-persistence.go`

### Corrupted Record Handling

When loading peers, corrupted records are skipped rather than failing the entire operation:

```go
func (sm *StateManager) LoadAllPeers() (*NodeBuffers, error) {
    nodeBuffers := NewNodeBuffers()
    var corruptedCount int
    var loadedCount int

    sm.db.View(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte(bucketPeers))

        return bucket.ForEach(func(k, v []byte) error {
            peerID, err := peer.Decode(string(k))
            if err != nil {
                corruptedCount++
                return nil  // Skip, don't fail
            }

            info, err := DeserializeNodeBufferInfo(v)
            if err != nil {
                if errors.Is(err, ErrChecksumMismatch) {
                    log.Printf("CORRUPTION DETECTED for peer %s", peerID)
                }
                corruptedCount++
                return nil  // Skip, don't fail
            }

            nodeBuffers.Buffers[peerID] = info
            loadedCount++
            return nil
        })
    })

    // Track corruption for monitoring
    sm.corruptedRecordsCount.Add(uint64(corruptedCount))

    return nodeBuffers, nil
}
```

### Degraded Mode

If the database cannot be opened at all, the system enters degraded mode:

```go
type StateManager struct {
    degradedMode      bool
    degradedModeMutex sync.RWMutex
    writesDropped     atomic.Uint64
}
```

In degraded mode:

- All write operations are silently dropped
- Read operations return errors
- A background goroutine periodically attempts recovery
- The system continues operating with in-memory state only

### Corruption Metrics

The `StateManager` tracks corruption statistics:

```go
func (sm *StateManager) GetStats() map[string]interface{} {
    return map[string]interface{}{
        "degraded_mode":           sm.degradedMode,
        "writes_dropped":          sm.writesDropped.Load(),
        "corrupted_records_count": sm.corruptedRecordsCount.Load(),
        "last_snapshot_time":      sm.lastSnapshotTime.Format(time.RFC3339),
        "snapshot_count":          len(snapshots),
        // ... additional stats
    }
}
```

---

## File Changes Summary

### common-lib/state-types.go

| Addition                                                       | Description                         |
| -------------------------------------------------------------- | ----------------------------------- |
| `serializationVersion`                                         | Version byte constant (0x01)        |
| `checksumSize`, `versionSize`, `headerSize`                    | Size constants for header           |
| `ErrChecksumMismatch`, `ErrDataTooShort`, `ErrVersionMismatch` | Error types                         |
| `SerializeNodeBufferInfo()`                                    | Updated to include checksum header  |
| `DeserializeNodeBufferInfo()`                                  | Updated to validate checksum        |
| `deserializeLegacyFormat()`                                    | Backward compatibility for old data |
| `buildNodeBufferInfoFromSerialized()`                          | Extracted helper function           |

### common-lib/state-persistence.go

| Addition                                          | Description                         |
| ------------------------------------------------- | ----------------------------------- |
| `snapshotDir`, `maxSnapshots`, `snapshotInterval` | Snapshot configuration fields       |
| `lastSnapshotTime`, `snapshotMutex`               | Snapshot state management           |
| `corruptedRecordsCount`                           | Corruption metric counter           |
| `setupSignalHandlers()`                           | Signal handler installation         |
| `snapshotWorker()`                                | Background snapshot goroutine       |
| `CreateSnapshot()`                                | Creates point-in-time database copy |
| `rotateSnapshots()`                               | Removes old snapshots               |
| `getSnapshotsSorted()`                            | Lists snapshots newest-first        |
| `openWithRecovery()`                              | Opens database with recovery logic  |
| `validateDatabaseIntegrity()`                     | Validates database structure        |
| `attemptRecoveryFromSnapshots()`                  | Recovery from snapshots             |
| `createFreshDatabaseWithBackup()`                 | Last-resort fresh database          |
| `syncDirectory()`                                 | Directory fsync helper              |
| `copyFileWithSync()`                              | File copy with fsync                |
| `GetSnapshotDirectory()`                          | Accessor for snapshot path          |
| `GetCorruptedRecordsCount()`                      | Accessor for corruption count       |

### neuron-sdk.go

| Change               | Description                                         |
| -------------------- | --------------------------------------------------- |
| Shutdown defer block | Enhanced with snapshot creation, corruption logging |

---

## Operational Considerations

### Monitoring

Monitor these metrics for early warning of storage issues:

- `corrupted_records_count`: Should remain 0; non-zero indicates storage problems
- `writes_dropped`: Should remain 0; non-zero indicates degraded mode
- `snapshot_count`: Should be equal to `maxSnapshots` after initial period

### Recovery Scenarios

| Scenario                 | Recovery Action                              |
| ------------------------ | -------------------------------------------- |
| Power loss during write  | Automatic recovery from latest snapshot      |
| Bit-flip corruption      | Checksum detection, record skipped           |
| Full database corruption | Recovery from snapshots, then fresh database |
| Signal-based termination | Emergency flush preserves pending writes     |

### Storage Requirements

- Snapshot storage: Up to 3x database size (3 snapshots)
- Corrupted backups: Accumulate over time, manual cleanup recommended
- Total overhead: Approximately 4x database size in worst case
