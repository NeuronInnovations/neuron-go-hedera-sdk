package commonlib

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"
)

const (
	// Database bucket names
	bucketPeers         = "peers"
	bucketTopics        = "topics"
	bucketMetadata      = "metadata"
	bucketPeerInfoCache = "peer_info_cache" // Cache for blockchain PeerInfo data

	// Special key for storing the full peer list in the cache bucket
	peerListCacheKey = "_peer_list_"

	// Default configuration
	defaultBatchInterval    = 5 * time.Minute
	defaultBatchSize        = 50
	retryInterval           = 5 * time.Minute
	defaultSnapshotInterval = 10 * time.Minute
	defaultMaxSnapshots     = 3
)

// StateManager handles persistent state storage using bbolt with corruption prevention
type StateManager struct {
	db                *bolt.DB
	dbPath            string
	writeQueue        chan interface{} // Channel for batched writes
	immediateQueue    chan interface{} // Channel for immediate writes
	stopChan          chan struct{}
	wg                sync.WaitGroup
	degradedMode      bool // True if persistence is failing
	degradedModeMutex sync.RWMutex
	closed            atomic.Bool   // Prevents double-close and write-after-close
	closeOnce         sync.Once     // Ensures Close() is called only once
	closeErr          error         // Stores error from Close()
	writesDropped     atomic.Uint64 // Counter for dropped writes (metrics)

	// Snapshot management for corruption recovery
	snapshotDir           string
	maxSnapshots          int
	snapshotInterval      time.Duration
	lastSnapshotTime      time.Time
	snapshotMutex         sync.Mutex
	corruptedRecordsCount atomic.Uint64 // Counter for corrupted records detected
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

	// Create snapshot directory
	snapshotDir := filepath.Join(dbDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		log.Printf("⚠️ Warning: Failed to create snapshot directory: %v", err)
		// Non-fatal - continue without snapshots
	}

	// Create StateManager with channels
	sm := &StateManager{
		dbPath:           dbPath,
		writeQueue:       make(chan interface{}, 1000),
		immediateQueue:   make(chan interface{}, 100),
		stopChan:         make(chan struct{}),
		snapshotDir:      snapshotDir,
		maxSnapshots:     defaultMaxSnapshots,
		snapshotInterval: defaultSnapshotInterval,
	}

	// Setup signal handlers for graceful shutdown on unexpected termination
	sm.setupSignalHandlers()

	// Always start background workers (even in degraded mode)
	sm.wg.Add(3) // batchWriter, immediateWriter, snapshotWorker
	go sm.batchWriter()
	go sm.immediateWriter()
	go sm.snapshotWorker()

	// Open database with corruption recovery
	db, err := sm.openWithRecovery(dbPath)
	if err != nil {
		log.Printf("⚠️ WARNING: Failed to open database, entering degraded mode: %v", err)
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
		log.Printf("⚠️ WARNING: Failed to initialize buckets, entering degraded mode: %v", err)
		sm.degradedMode = true
		sm.wg.Add(1)
		go sm.retryDatabaseOpen()
		return sm, nil
	}

	// Success - set database and mark as not degraded
	sm.db = db
	sm.degradedMode = false

	log.Printf("✅ StateManager initialized successfully at %s", dbPath)
	log.Printf("📁 Snapshot directory: %s", snapshotDir)
	return sm, nil
}

// setupSignalHandlers configures handlers for graceful shutdown signals
func (sm *StateManager) setupSignalHandlers() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // Termination
		syscall.SIGHUP,  // Hangup
	)

	go func() {
		sig := <-sigChan
		log.Printf("🛑 StateManager received signal %v, initiating emergency flush...", sig)

		// Mark as closed to prevent new writes
		sm.closed.Store(true)

		// Emergency flush - sync immediately
		sm.degradedModeMutex.RLock()
		db := sm.db
		degraded := sm.degradedMode
		sm.degradedModeMutex.RUnlock()

		if !degraded && db != nil {
			// Flush pending writes
			if err := sm.FlushAll(); err != nil {
				log.Printf("⚠️ Emergency flush error: %v", err)
			}

			// Force sync to disk
			if err := db.Sync(); err != nil {
				log.Printf("⚠️ Emergency sync error: %v", err)
			}

			log.Printf("✅ Emergency flush completed")
		}

		// Re-raise signal for default handling (allows proper process exit)
		signal.Reset(sig)
		if sigNum, ok := sig.(syscall.Signal); ok {
			syscall.Kill(syscall.Getpid(), sigNum)
		}
	}()
}

// openWithRecovery attempts to open the database with corruption detection and recovery
func (sm *StateManager) openWithRecovery(dbPath string) (*bolt.DB, error) {
	// Try to open database with optimized settings for SD card
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{
		Timeout:      10 * time.Second, // Production-ready timeout for lock acquisition
		NoGrowSync:   true,             // Don't sync on file growth - reduces SD card wear
		FreelistType: bolt.FreelistMapType, // Better for frequent updates
	})

	if err != nil {
		log.Printf("🔴 Database open failed: %v", err)
		return sm.attemptRecoveryFromSnapshots(dbPath, err)
	}

	// Validate database integrity
	if err := validateDatabaseIntegrity(db); err != nil {
		log.Printf("🔴 Database integrity check failed: %v", err)
		db.Close()
		return sm.attemptRecoveryFromSnapshots(dbPath, err)
	}

	return db, nil
}

// validateDatabaseIntegrity performs basic integrity checks on the database
func validateDatabaseIntegrity(db *bolt.DB) error {
	return db.View(func(tx *bolt.Tx) error {
		// Check if we can iterate all buckets (will fail if corrupted)
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			// Try to iterate bucket keys (will fail if corrupted)
			return b.ForEach(func(k, v []byte) error {
				return nil // Just checking iteration works
			})
		})
	})
}

// attemptRecoveryFromSnapshots tries to recover from available snapshots
func (sm *StateManager) attemptRecoveryFromSnapshots(dbPath string, originalErr error) (*bolt.DB, error) {
	// Get sorted list of snapshots (newest first)
	snapshots, err := sm.getSnapshotsSorted()
	if err != nil || len(snapshots) == 0 {
		log.Printf("⚠️ No snapshots available for recovery")
		return sm.createFreshDatabaseWithBackup(dbPath, originalErr)
	}

	// Try each snapshot from newest to oldest
	for i, snapshot := range snapshots {
		snapshotPath := filepath.Join(sm.snapshotDir, snapshot)
		log.Printf("🔄 Attempting recovery from snapshot %d/%d: %s", i+1, len(snapshots), snapshot)

		recoveryPath := dbPath + ".recovery"

		// Copy snapshot to recovery path
		if err := copyFileWithSync(snapshotPath, recoveryPath); err != nil {
			log.Printf("⚠️ Failed to copy snapshot: %v", err)
			continue
		}

		// Try to open recovered database
		db, err := bolt.Open(recoveryPath, 0600, &bolt.Options{
			Timeout: 10 * time.Second,
		})
		if err != nil {
			log.Printf("⚠️ Snapshot %s is also corrupted: %v", snapshot, err)
			os.Remove(recoveryPath)
			continue
		}

		// Validate integrity
		if err := validateDatabaseIntegrity(db); err != nil {
			log.Printf("⚠️ Snapshot %s failed integrity check: %v", snapshot, err)
			db.Close()
			os.Remove(recoveryPath)
			continue
		}

		// Success! Close and rename
		db.Close()

		// Backup corrupted database
		if _, statErr := os.Stat(dbPath); statErr == nil {
			backupPath := fmt.Sprintf("%s.corrupted.%d", dbPath, time.Now().Unix())
			if renameErr := os.Rename(dbPath, backupPath); renameErr != nil {
				log.Printf("⚠️ Failed to backup corrupted database: %v", renameErr)
			} else {
				log.Printf("📦 Corrupted database backed up to: %s", backupPath)
			}
		}

		// Move recovered database into place
		if err := os.Rename(recoveryPath, dbPath); err != nil {
			log.Printf("🔴 Failed to finalize recovery: %v", err)
			os.Remove(recoveryPath)
			continue
		}

		// Sync directory to ensure rename is durable
		if err := syncDirectory(filepath.Dir(dbPath)); err != nil {
			log.Printf("⚠️ Warning: failed to sync directory: %v", err)
		}

		log.Printf("✅ Successfully recovered from snapshot: %s", snapshot)
		return bolt.Open(dbPath, 0600, &bolt.Options{
			Timeout:      10 * time.Second,
			NoGrowSync:   true,
			FreelistType: bolt.FreelistMapType,
		})
	}

	// All snapshots failed
	log.Printf("🔴 All snapshots failed, creating fresh database")
	return sm.createFreshDatabaseWithBackup(dbPath, originalErr)
}

// createFreshDatabaseWithBackup backs up corrupted database and creates a fresh one
func (sm *StateManager) createFreshDatabaseWithBackup(dbPath string, originalErr error) (*bolt.DB, error) {
	// Backup corrupted file if exists
	if _, err := os.Stat(dbPath); err == nil {
		backupPath := fmt.Sprintf("%s.corrupted.%d", dbPath, time.Now().Unix())
		if renameErr := os.Rename(dbPath, backupPath); renameErr != nil {
			log.Printf("⚠️ Failed to backup corrupted database: %v", renameErr)
		} else {
			log.Printf("📦 Corrupted database backed up to: %s", backupPath)
		}
	}

	// Create fresh database
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{
		Timeout:      1 * time.Second,
		NoGrowSync:   true,
		FreelistType: bolt.FreelistMapType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create fresh database after corruption: %w (original error: %v)", err, originalErr)
	}

	log.Println("🆕 Created fresh database after corruption recovery")
	return db, nil
}

// getSnapshotsSorted returns snapshot filenames sorted by timestamp (newest first)
func (sm *StateManager) getSnapshotsSorted() ([]string, error) {
	entries, err := os.ReadDir(sm.snapshotDir)
	if err != nil {
		return nil, err
	}

	var snapshots []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "state-") && strings.HasSuffix(entry.Name(), ".db") {
			snapshots = append(snapshots, entry.Name())
		}
	}

	// Sort newest first (filenames contain timestamps)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i] > snapshots[j]
	})

	return snapshots, nil
}

// initializeBuckets creates the necessary buckets if they don't exist
func initializeBuckets(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		buckets := []string{bucketPeers, bucketTopics, bucketMetadata, bucketPeerInfoCache}
		for _, bucket := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
}

// snapshotWorker periodically creates database snapshots for recovery
func (sm *StateManager) snapshotWorker() {
	defer sm.wg.Done()
	ticker := time.NewTicker(sm.snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := sm.CreateSnapshot(); err != nil {
				log.Printf("⚠️ Snapshot creation failed: %v", err)
			}
		case <-sm.stopChan:
			// Create final snapshot before shutdown
			log.Println("📸 Creating final snapshot before shutdown...")
			if err := sm.CreateSnapshot(); err != nil {
				log.Printf("⚠️ Final snapshot creation failed: %v", err)
			}
			return
		}
	}
}

// CreateSnapshot creates a point-in-time backup of the database
func (sm *StateManager) CreateSnapshot() error {
	sm.snapshotMutex.Lock()
	defer sm.snapshotMutex.Unlock()

	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	db := sm.db
	sm.degradedModeMutex.RUnlock()

	if degraded || db == nil {
		return fmt.Errorf("cannot create snapshot: database unavailable")
	}

	// Generate snapshot filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	snapshotPath := filepath.Join(sm.snapshotDir, fmt.Sprintf("state-%s.db", timestamp))
	tempPath := snapshotPath + ".tmp"

	// Use bbolt's consistent snapshot feature
	err := db.View(func(tx *bolt.Tx) error {
		return tx.CopyFile(tempPath, 0600)
	})
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Atomic rename for crash safety
	if err := os.Rename(tempPath, snapshotPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to finalize snapshot: %w", err)
	}

	// Sync directory to ensure rename is durable
	if err := syncDirectory(sm.snapshotDir); err != nil {
		log.Printf("⚠️ Warning: failed to sync snapshot directory: %v", err)
	}

	sm.lastSnapshotTime = time.Now()
	log.Printf("✅ Created snapshot: %s", filepath.Base(snapshotPath))

	// Rotate old snapshots
	sm.rotateSnapshots()

	return nil
}

// rotateSnapshots removes old snapshots keeping only maxSnapshots
func (sm *StateManager) rotateSnapshots() {
	snapshots, err := sm.getSnapshotsSorted()
	if err != nil {
		log.Printf("⚠️ Warning: failed to read snapshot directory: %v", err)
		return
	}

	// Remove oldest snapshots exceeding maxSnapshots
	for len(snapshots) > sm.maxSnapshots {
		oldestIndex := len(snapshots) - 1
		oldPath := filepath.Join(sm.snapshotDir, snapshots[oldestIndex])
		if err := os.Remove(oldPath); err != nil {
			log.Printf("⚠️ Warning: failed to remove old snapshot %s: %v", snapshots[oldestIndex], err)
		} else {
			log.Printf("🗑️ Removed old snapshot: %s", snapshots[oldestIndex])
		}
		snapshots = snapshots[:oldestIndex]
	}
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
				db, err := sm.openWithRecovery(sm.dbPath)
				if err == nil {
					if initErr := initializeBuckets(db); initErr != nil {
						log.Printf("Failed to initialize buckets during recovery: %v", initErr)
						db.Close()
					} else {
						sm.db = db
						sm.degradedMode = false
						log.Println("✅ Successfully recovered from degraded mode")
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
			log.Printf("⚠️ WARNING: %d writes dropped in degraded mode - persistence unavailable", dropped)
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
			log.Printf("⚠️ Immediate write queue full, dropping write for peer %s", peerID)
		}
	} else {
		select {
		case sm.writeQueue <- write:
		default:
			log.Printf("⚠️ Batched write queue full, dropping write for peer %s", peerID)
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
		if err != nil {
			// Check if it's a checksum error (corruption)
			if errors.Is(err, ErrChecksumMismatch) {
				sm.corruptedRecordsCount.Add(1)
				return fmt.Errorf("peer data corrupted: %w", err)
			}
			return err
		}
		return nil
	})

	return info, err
}

// LoadAllPeers loads all peers from the database into a NodeBuffers instance
// Corrupted individual records are logged and skipped to allow partial recovery
func (sm *StateManager) LoadAllPeers() (*NodeBuffers, error) {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil, fmt.Errorf("database in degraded mode")
	}

	nodeBuffers := NewNodeBuffers()
	var corruptedCount int
	var loadedCount int

	err := sm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeers))
		if bucket == nil {
			return nil // No peers bucket yet, return empty
		}

		return bucket.ForEach(func(k, v []byte) error {
			peerID, err := peer.Decode(string(k))
			if err != nil {
				log.Printf("⚠️ Skipping invalid peer ID %s: %v", string(k), err)
				corruptedCount++
				return nil // Skip invalid peer ID
			}

			info, err := DeserializeNodeBufferInfo(v)
			if err != nil {
				// Check if it's a checksum error (corruption detected)
				if errors.Is(err, ErrChecksumMismatch) {
					log.Printf("🔴 CORRUPTION DETECTED for peer %s: checksum mismatch, skipping", peerID)
				} else {
					log.Printf("⚠️ Failed to deserialize peer %s: %v, skipping", peerID, err)
				}
				corruptedCount++
				return nil // Skip corrupted data - don't fail entire load
			}

			nodeBuffers.Buffers[peerID] = info
			loadedCount++
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	// Update corruption counter
	if corruptedCount > 0 {
		sm.corruptedRecordsCount.Add(uint64(corruptedCount))
		log.Printf("⚠️ WARNING: %d corrupted peer records were skipped during load", corruptedCount)
	}

	log.Printf("✅ Loaded %d valid peers from persistent state", loadedCount)
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
		log.Printf("⚠️ Write queue full, dropping topic position write for %s", topicKey)
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
		log.Printf("⚠️ Write queue full, dropping metadata write for %s", key)
	}
}

// ============================================================================
// PeerInfo Cache Methods - For blockchain fallback strategy
// ============================================================================

// PersistPeerInfo caches PeerInfo data from blockchain for fallback use
// This write is batched as it's not critical for immediate persistence
func (sm *StateManager) PersistPeerInfo(evmAddress string, info *CachedPeerInfo) {
	// Check if closed first to prevent write to closed channel
	if sm.closed.Load() {
		return
	}

	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return
	}

	write := PeerInfoCacheWrite{
		EvmAddress: evmAddress,
		Info:       info,
	}

	select {
	case sm.writeQueue <- write:
	default:
		log.Printf("⚠️ Write queue full, dropping PeerInfo cache write for %s", evmAddress)
	}
}

// LoadPeerInfo loads cached PeerInfo for a given EVM address
// Returns error if not found or corrupted
func (sm *StateManager) LoadPeerInfo(evmAddress string) (*CachedPeerInfo, error) {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil, fmt.Errorf("database in degraded mode")
	}

	var info *CachedPeerInfo
	err := sm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeerInfoCache))
		if bucket == nil {
			return fmt.Errorf("peer info cache bucket not found")
		}

		data := bucket.Get([]byte(evmAddress))
		if data == nil {
			return fmt.Errorf("peer info not found in cache for %s", evmAddress)
		}

		var err error
		info, err = DeserializeCachedPeerInfo(data)
		if err != nil {
			// Check if it's a checksum error (corruption)
			if errors.Is(err, ErrChecksumMismatch) {
				sm.corruptedRecordsCount.Add(1)
				return fmt.Errorf("cached peer info corrupted for %s: %w", evmAddress, err)
			}
			return err
		}
		return nil
	})

	return info, err
}

// PersistPeerList caches the list of all registered peer addresses
// This write is batched as it's not critical for immediate persistence
func (sm *StateManager) PersistPeerList(list *CachedPeerList) {
	// Check if closed first to prevent write to closed channel
	if sm.closed.Load() {
		return
	}

	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return
	}

	write := PeerListCacheWrite{
		List: list,
	}

	select {
	case sm.writeQueue <- write:
	default:
		log.Printf("⚠️ Write queue full, dropping peer list cache write")
	}
}

// LoadPeerList loads the cached list of all registered peer addresses
// Returns error if not found or corrupted
func (sm *StateManager) LoadPeerList() (*CachedPeerList, error) {
	sm.degradedModeMutex.RLock()
	degraded := sm.degradedMode
	sm.degradedModeMutex.RUnlock()

	if degraded {
		return nil, fmt.Errorf("database in degraded mode")
	}

	var list *CachedPeerList
	err := sm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketPeerInfoCache))
		if bucket == nil {
			return fmt.Errorf("peer info cache bucket not found")
		}

		data := bucket.Get([]byte(peerListCacheKey))
		if data == nil {
			return fmt.Errorf("peer list not found in cache")
		}

		var err error
		list, err = DeserializeCachedPeerList(data)
		if err != nil {
			// Check if it's a checksum error (corruption)
			if errors.Is(err, ErrChecksumMismatch) {
				sm.corruptedRecordsCount.Add(1)
				return fmt.Errorf("cached peer list corrupted: %w", err)
			}
			return err
		}
		return nil
	})

	return list, err
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
		buckets := []string{bucketPeers, bucketTopics, bucketMetadata, bucketPeerInfoCache}
		for _, bucket := range buckets {
			if err := tx.DeleteBucket([]byte(bucket)); err != nil && err != bolt.ErrBucketNotFound {
				return err
			}
			if _, err := tx.CreateBucket([]byte(bucket)); err != nil {
				return err
			}
		}
		log.Println("🗑️ Cleared all persistent state")
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
			log.Printf("⚠️ Failed to execute batched writes: %v", err)
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
				log.Printf("⚠️ Failed to execute immediate write: %v", err)
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

	case PeerInfoCacheWrite:
		bucket := tx.Bucket([]byte(bucketPeerInfoCache))
		if bucket == nil {
			return fmt.Errorf("peer info cache bucket not found")
		}

		data, err := SerializeCachedPeerInfo(w.Info)
		if err != nil {
			return fmt.Errorf("failed to serialize cached peer info: %w", err)
		}

		return bucket.Put([]byte(w.EvmAddress), data)

	case PeerListCacheWrite:
		bucket := tx.Bucket([]byte(bucketPeerInfoCache))
		if bucket == nil {
			return fmt.Errorf("peer info cache bucket not found")
		}

		data, err := SerializeCachedPeerList(w.List)
		if err != nil {
			return fmt.Errorf("failed to serialize cached peer list: %w", err)
		}

		return bucket.Put([]byte(peerListCacheKey), data)

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
		log.Printf("💾 Flushed %d pending writes", len(batch))
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
		"degraded_mode":            sm.degradedMode,
		"write_queue_length":       len(sm.writeQueue),
		"immediate_queue_len":      len(sm.immediateQueue),
		"writes_dropped":           sm.writesDropped.Load(),
		"closed":                   sm.closed.Load(),
		"corrupted_records_count":  sm.corruptedRecordsCount.Load(),
		"last_snapshot_time":       sm.lastSnapshotTime.Format(time.RFC3339),
		"snapshot_dir":             sm.snapshotDir,
		"max_snapshots":            sm.maxSnapshots,
	}

	// Count current snapshots
	if snapshots, err := sm.getSnapshotsSorted(); err == nil {
		stats["snapshot_count"] = len(snapshots)
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
		log.Println("🛑 Closing StateManager...")

		// Mark as closed to prevent new writes
		sm.closed.Store(true)

		// Signal background workers to stop
		close(sm.stopChan)

		// Wait for background workers to finish
		sm.wg.Wait()

		// Flush any remaining writes
		if err := sm.FlushAll(); err != nil {
			log.Printf("⚠️ Error flushing writes during close: %v", err)
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
				log.Printf("⚠️ Error closing database: %v", err)
				if sm.closeErr == nil {
					sm.closeErr = fmt.Errorf("failed to close database: %w", err)
				}
			}
		}

		if sm.closeErr == nil {
			log.Println("✅ StateManager closed successfully")
		} else {
			log.Printf("⚠️ StateManager closed with errors: %v", sm.closeErr)
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
				log.Printf("⚠️ Failed to serialize peer %s: %v", peerID, err)
				continue
			}

			if err := bucket.Put([]byte(peerID.String()), data); err != nil {
				log.Printf("⚠️ Failed to save peer %s: %v", peerID, err)
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

// GetSnapshotDirectory returns the path to the snapshot directory
func (sm *StateManager) GetSnapshotDirectory() string {
	return sm.snapshotDir
}

// GetCorruptedRecordsCount returns the number of corrupted records detected
func (sm *StateManager) GetCorruptedRecordsCount() uint64 {
	return sm.corruptedRecordsCount.Load()
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

// Helper functions

// syncDirectory syncs a directory to ensure metadata is written to disk
func syncDirectory(dirPath string) error {
	dir, err := os.Open(dirPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// copyFileWithSync copies a file from src to dst with fsync for durability
// This is critical for SD card corruption prevention
func copyFileWithSync(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Sync to ensure data is written to disk - critical for SD cards
	return destFile.Sync()
}

