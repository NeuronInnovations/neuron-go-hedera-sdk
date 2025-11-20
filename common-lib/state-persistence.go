package commonlib

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"
)

const (
	// Database bucket names
	bucketPeers    = "peers"
	bucketTopics   = "topics"
	bucketMetadata = "metadata"

	// Default configuration
	defaultBatchInterval = 5 * time.Minute
	defaultBatchSize     = 50
	retryInterval        = 5 * time.Minute
)

// StateManager handles persistent state storage using bbolt
type StateManager struct {
	db                *bolt.DB
	dbPath            string
	writeQueue        chan interface{} // Channel for batched writes
	immediateQueue    chan interface{} // Channel for immediate writes
	stopChan          chan struct{}
	wg                sync.WaitGroup
	degradedMode      bool // True if persistence is failing
	degradedModeMutex sync.RWMutex
	closed            atomic.Bool      // Prevents double-close and write-after-close
	closeOnce         sync.Once        // Ensures Close() is called only once
	closeErr          error            // Stores error from Close()
	writesDropped     atomic.Uint64    // Counter for dropped writes (metrics)
}

// NewStateManager creates a new StateManager with the specified database path
// If dbPath is empty, uses default location: ~/.neuron/state.db
func NewStateManager(dbPath string) (*StateManager, error) {
	// Determine database path
	if dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(homeDir, ".neuron", "state.db")
	}

	// Ensure directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Create StateManager with channels
	sm := &StateManager{
		dbPath:         dbPath,
		writeQueue:     make(chan interface{}, 1000),
		immediateQueue: make(chan interface{}, 100),
		stopChan:       make(chan struct{}),
	}

	// Always start background workers (even in degraded mode)
	sm.wg.Add(2)
	go sm.batchWriter()
	go sm.immediateWriter()

	// Open database with corruption recovery
	db, err := openWithRecovery(dbPath)
	if err != nil {
		log.Printf("WARNING: Failed to open database, entering degraded mode: %v", err)
		sm.degradedMode = true
		// Start retry goroutine to attempt recovery
		sm.wg.Add(1)
		go sm.retryDatabaseOpen()
		log.Printf("StateManager running in degraded mode (in-memory only)")
		return sm, nil
	}

	// Initialize buckets
	if err := initializeBuckets(db); err != nil {
		db.Close()
		log.Printf("WARNING: Failed to initialize buckets, entering degraded mode: %v", err)
		sm.degradedMode = true
		sm.wg.Add(1)
		go sm.retryDatabaseOpen()
		return sm, nil
	}

	// Success - set database and mark as not degraded
	sm.db = db
	sm.degradedMode = false

	log.Printf("StateManager initialized successfully at %s", dbPath)
	return sm, nil
}

// openWithRecovery attempts to open the database with corruption detection and recovery
func openWithRecovery(dbPath string) (*bolt.DB, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{
		Timeout: 10 * time.Second, // Production-ready timeout for lock acquisition
	})

	if err != nil {
		// Check if it's a corruption error
		if err == bolt.ErrInvalid || err == bolt.ErrVersionMismatch || err == bolt.ErrChecksum {
			log.Printf("Database corruption detected: %v", err)
			// Backup corrupted file
			backupPath := fmt.Sprintf("%s.corrupted.%d", dbPath, time.Now().Unix())
			if renameErr := os.Rename(dbPath, backupPath); renameErr != nil {
				log.Printf("Failed to backup corrupted database: %v", renameErr)
			} else {
				log.Printf("Corrupted database backed up to: %s", backupPath)
			}

			// Try to create fresh database
			db, err = bolt.Open(dbPath, 0600, &bolt.Options{
				Timeout: 1 * time.Second,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create fresh database after corruption: %w", err)
			}
			log.Println("Created fresh database after corruption recovery")
			return db, nil
		}
		return nil, err
	}

	return db, nil
}

// initializeBuckets creates the necessary buckets if they don't exist
func initializeBuckets(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		buckets := []string{bucketPeers, bucketTopics, bucketMetadata}
		for _, bucket := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
}

// retryDatabaseOpen attempts to recover from degraded mode periodically
func (sm *StateManager) retryDatabaseOpen() {
	defer sm.wg.Done()
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.degradedModeMutex.Lock()
			if sm.degradedMode && sm.db == nil {
				db, err := openWithRecovery(sm.dbPath)
				if err == nil {
					if initErr := initializeBuckets(db); initErr != nil {
						log.Printf("Failed to initialize buckets during recovery: %v", initErr)
						db.Close()
					} else {
						sm.db = db
						sm.degradedMode = false
						log.Println("Successfully recovered from degraded mode")
						sm.degradedModeMutex.Unlock()
						return
					}
				}
			}
			sm.degradedModeMutex.Unlock()
		case <-sm.stopChan:
			return
		}
	}
}

// PersistPeer queues a peer for persistence
func (sm *StateManager) PersistPeer(peerID peer.ID, info *NodeBufferInfo, immediate bool) {
	// Check if closed first to prevent write to closed channel
	if sm.closed.Load() {
		return // StateManager is closed, skip write
	}

	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		// Track dropped writes and warn periodically
		dropped := sm.writesDropped.Add(1)
		if dropped%100 == 0 {
			log.Printf("WARNING: %d writes dropped in degraded mode - persistence unavailable", dropped)
		}
		return
	}

	write := PeerWrite{
		PeerID:         peerID,
		Info:           info,
		Classification: WriteBatched,
	}

	if immediate {
		write.Classification = WriteImmediate
		select {
		case sm.immediateQueue <- write:
		default:
			log.Printf("Immediate write queue full, dropping write for peer %s", peerID)
		}
	} else {
		select {
		case sm.writeQueue <- write:
		default:
			log.Printf("Batched write queue full, dropping write for peer %s", peerID)
		}
	}
}

// LoadPeer loads a single peer's information from the database
func (sm *StateManager) LoadPeer(peerID peer.ID) (*NodeBufferInfo, error) {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil, fmt.Errorf("database in degraded mode")
	}

	var info *NodeBufferInfo
	err := sm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeers))
		if bucket == nil {
			return fmt.Errorf("peers bucket not found")
		}

		data := bucket.Get([]byte(peerID.String()))
		if data == nil {
			return fmt.Errorf("peer not found")
		}

		var err error
		info, err = DeserializeNodeBufferInfo(data)
		return err
	})

	return info, err
}

// LoadAllPeers loads all peers from the database into a NodeBuffers instance
func (sm *StateManager) LoadAllPeers() (*NodeBuffers, error) {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil, fmt.Errorf("database in degraded mode")
	}

	nodeBuffers := NewNodeBuffers()

	err := sm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeers))
		if bucket == nil {
			return nil // No peers bucket yet, return empty
		}

		return bucket.ForEach(func(k, v []byte) error {
			peerID, err := peer.Decode(string(k))
			if err != nil {
				log.Printf("Failed to decode peer ID %s: %v", string(k), err)
				return nil // Skip invalid peer ID
			}

			info, err := DeserializeNodeBufferInfo(v)
			if err != nil {
				log.Printf("Failed to deserialize peer %s: %v", peerID, err)
				return nil // Skip invalid data
			}

			nodeBuffers.Buffers[peerID] = info
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	log.Printf("Loaded %d peers from persistent state", len(nodeBuffers.Buffers))
	return nodeBuffers, nil
}

// RemovePeer removes a peer from the database
func (sm *StateManager) RemovePeer(peerID peer.ID) error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil // Silently skip in degraded mode
	}

	return sm.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeers))
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(peerID.String()))
	})
}

// PersistTopicPosition queues a topic position for persistence (batched)
func (sm *StateManager) PersistTopicPosition(topicKey string, timestamp time.Time) {
	// Check if closed first to prevent write to closed channel
	if sm.closed.Load() {
		return // StateManager is closed, skip write
	}

	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return
	}

	write := TopicPositionWrite{
		TopicKey:  topicKey,
		Timestamp: timestamp,
	}

	select {
	case sm.writeQueue <- write:
	default:
		log.Printf("Write queue full, dropping topic position write for %s", topicKey)
	}
}

// LoadTopicPosition loads the last processed timestamp for a topic
func (sm *StateManager) LoadTopicPosition(topicKey string) (time.Time, error) {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return time.Time{}, fmt.Errorf("database in degraded mode")
	}

	var timestamp time.Time
	err := sm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketTopics))
		if bucket == nil {
			return fmt.Errorf("topics bucket not found")
		}

		data := bucket.Get([]byte(topicKey))
		if data == nil {
			return fmt.Errorf("topic position not found")
		}

		return timestamp.UnmarshalText(data)
	})

	return timestamp, err
}

// PersistMetadata persists a metadata key-value pair (batched)
func (sm *StateManager) PersistMetadata(key, value string) {
	// Check if closed first to prevent write to closed channel
	if sm.closed.Load() {
		return // StateManager is closed, skip write
	}

	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return
	}

	write := MetadataWrite{
		Key:   key,
		Value: value,
	}

	select {
	case sm.writeQueue <- write:
	default:
		log.Printf("Write queue full, dropping metadata write for %s", key)
	}
}

// ClearAll removes all data from the database
func (sm *StateManager) ClearAll() error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil // Silently skip in degraded mode
	}

	return sm.db.Update(func(tx *bolt.Tx) error {
		// Delete and recreate all buckets
		buckets := []string{bucketPeers, bucketTopics, bucketMetadata}
		for _, bucket := range buckets {
			if err := tx.DeleteBucket([]byte(bucket)); err != nil && err != bolt.ErrBucketNotFound {
				return err
			}
			if _, err := tx.CreateBucket([]byte(bucket)); err != nil {
				return err
			}
		}
		log.Println("Cleared all persistent state")
		return nil
	})
}

// batchWriter handles batched writes in the background
func (sm *StateManager) batchWriter() {
	defer sm.wg.Done()
	ticker := time.NewTicker(defaultBatchInterval)
	defer ticker.Stop()

	batch := make([]interface{}, 0, defaultBatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := sm.executeBatch(batch); err != nil {
			log.Printf("Failed to execute batched writes: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case write := <-sm.writeQueue:
			batch = append(batch, write)
			if len(batch) >= defaultBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-sm.stopChan:
			flush()
			return
		}
	}
}

// immediateWriter handles immediate writes
func (sm *StateManager) immediateWriter() {
	defer sm.wg.Done()

	for {
		select {
		case write := <-sm.immediateQueue:
			if err := sm.executeWrite(write); err != nil {
				log.Printf("Failed to execute immediate write: %v", err)
			}
		case <-sm.stopChan:
			return
		}
	}
}

// executeBatch executes a batch of writes in a single transaction
func (sm *StateManager) executeBatch(writes []interface{}) error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	db := sm.db
	sm.degradedModeMutex.RUnlock()

	if degraded || db == nil {
		return nil
	}

	return db.Update(func(tx *bolt.Tx) error {
		for _, write := range writes {
			if err := sm.executeWriteInTx(tx, write); err != nil {
				return err
			}
		}
		return nil
	})
}

// executeWrite executes a single write with immediate Sync for durability
func (sm *StateManager) executeWrite(write interface{}) error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	db := sm.db
	sm.degradedModeMutex.RUnlock()

	if degraded || db == nil {
		return nil
	}

	// Execute write in transaction
	err := db.Update(func(tx *bolt.Tx) error {
		return sm.executeWriteInTx(tx, write)
	})

	if err != nil {
		return err
	}

	// Sync critical writes to disk immediately for durability
	// This ensures peer connections, IP addresses survive power failure
	return db.Sync()
}

// executeWriteInTx executes a write within a transaction
func (sm *StateManager) executeWriteInTx(tx *bolt.Tx, write interface{}) error {
	switch w := write.(type) {
	case PeerWrite:
		bucket := tx.Bucket([]byte(bucketPeers))
		if bucket == nil {
			return fmt.Errorf("peers bucket not found")
		}

		data, err := SerializeNodeBufferInfo(w.Info)
		if err != nil {
			return fmt.Errorf("failed to serialize peer info: %w", err)
		}

		return bucket.Put([]byte(w.PeerID.String()), data)

	case TopicPositionWrite:
		bucket := tx.Bucket([]byte(bucketTopics))
		if bucket == nil {
			return fmt.Errorf("topics bucket not found")
		}

		data, err := w.Timestamp.MarshalText()
		if err != nil {
			return fmt.Errorf("failed to marshal timestamp: %w", err)
		}

		return bucket.Put([]byte(w.TopicKey), data)

	case MetadataWrite:
		bucket := tx.Bucket([]byte(bucketMetadata))
		if bucket == nil {
			return fmt.Errorf("metadata bucket not found")
		}

		return bucket.Put([]byte(w.Key), []byte(w.Value))

	default:
		return fmt.Errorf("unknown write type: %T", write)
	}
}

// FlushAll flushes all pending writes
func (sm *StateManager) FlushAll() error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil
	}

	// Drain queues and execute all pending writes
	batch := make([]interface{}, 0)

	// Drain batched queue
	for {
		select {
		case write := <-sm.writeQueue:
			batch = append(batch, write)
		default:
			goto drainImmediate
		}
	}

drainImmediate:
	// Drain immediate queue
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
		if err := sm.executeBatch(batch); err != nil {
			return fmt.Errorf("failed to flush pending writes: %w", err)
		}
		log.Printf("Flushed %d pending writes", len(batch))
	}

	// Ensure all writes are synced to disk
	if sm.db != nil {
		return sm.db.Sync()
	}

	return nil
}

// GetStats returns database statistics (for debugging/monitoring)
func (sm *StateManager) GetStats() map[string]interface{} {
	sm.degradedModeMutex.RLock()
	defer sm.degradedModeMutex.RUnlock()

	stats := map[string]interface{}{
		"degraded_mode":       sm.degradedMode,
		"write_queue_length":  len(sm.writeQueue),
		"immediate_queue_len": len(sm.immediateQueue),
		"writes_dropped":      sm.writesDropped.Load(),
		"closed":              sm.closed.Load(),
	}

	if sm.db != nil {
		dbStats := sm.db.Stats()
		stats["db_stats"] = dbStats
	}

	return stats
}

// Close gracefully shuts down the StateManager
// Safe to call multiple times - subsequent calls return the same error
func (sm *StateManager) Close() error {
	sm.closeOnce.Do(func() {
		log.Println("Closing StateManager...")

		// Mark as closed to prevent new writes
		sm.closed.Store(true)

		// Signal background workers to stop
		close(sm.stopChan)

		// Wait for background workers to finish
		sm.wg.Wait()

		// Flush any remaining writes
		if err := sm.FlushAll(); err != nil {
			log.Printf("Error flushing writes during close: %v", err)
			if sm.closeErr == nil {
				sm.closeErr = err
			}
		}

		// Close database
		sm.degradedModeMutex.RLock()
		db := sm.db
		sm.degradedModeMutex.RUnlock()

		if db != nil {
			if err := db.Close(); err != nil {
				log.Printf("Error closing database: %v", err)
				if sm.closeErr == nil {
					sm.closeErr = fmt.Errorf("failed to close database: %w", err)
				}
			}
		}

		if sm.closeErr == nil {
			log.Println("StateManager closed successfully")
		} else {
			log.Printf("StateManager closed with errors: %v", sm.closeErr)
		}
	})

	return sm.closeErr
}

// SaveNodeBuffersSnapshot saves a complete snapshot of NodeBuffers to the database
// This is useful for one-time bulk saves (e.g., during shutdown)
func (sm *StateManager) SaveNodeBuffersSnapshot(nb *NodeBuffers) error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil
	}

	nb.mu.RLock()
	defer nb.mu.RUnlock()

	return sm.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeers))
		if bucket == nil {
			return fmt.Errorf("peers bucket not found")
		}

		for peerID, info := range nb.Buffers {
			data, err := SerializeNodeBufferInfo(info)
			if err != nil {
				log.Printf("Failed to serialize peer %s: %v", peerID, err)
				continue
			}

			if err := bucket.Put([]byte(peerID.String()), data); err != nil {
				log.Printf("Failed to save peer %s: %v", peerID, err)
			}
		}

		return nil
	})
}

// GetDatabasePath returns the path to the database file
func (sm *StateManager) GetDatabasePath() string {
	return sm.dbPath
}

// IsInDegradedMode returns true if the StateManager is in degraded mode
func (sm *StateManager) IsInDegradedMode() bool {
	sm.degradedModeMutex.RLock()
	defer sm.degradedModeMutex.RUnlock()
	return sm.degradedMode
}

// ExportToJSON exports the entire database to a JSON file (for debugging/backup)
func (sm *StateManager) ExportToJSON(outputPath string) error {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return fmt.Errorf("cannot export in degraded mode")
	}

	export := make(map[string]interface{})

	err := sm.db.View(func(tx *bolt.Tx) error {
		// Export peers
		peersBucket := tx.Bucket([]byte(bucketPeers))
		if peersBucket != nil {
			peers := make(map[string]interface{})
			peersBucket.ForEach(func(k, v []byte) error {
				peers[string(k)] = json.RawMessage(v)
				return nil
			})
			export["peers"] = peers
		}

		// Export topics
		topicsBucket := tx.Bucket([]byte(bucketTopics))
		if topicsBucket != nil {
			topics := make(map[string]string)
			topicsBucket.ForEach(func(k, v []byte) error {
				topics[string(k)] = string(v)
				return nil
			})
			export["topics"] = topics
		}

		// Export metadata
		metadataBucket := tx.Bucket([]byte(bucketMetadata))
		if metadataBucket != nil {
			metadata := make(map[string]string)
			metadataBucket.ForEach(func(k, v []byte) error {
				metadata[string(k)] = string(v)
				return nil
			})
			export["metadata"] = metadata
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Write to file
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, data, 0644)
}
