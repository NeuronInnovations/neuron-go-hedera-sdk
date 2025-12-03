package commonlib

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
)

// =============================================================================
// Test Setup Helpers are defined in state-persistence_test.go:
// - setupGlobalStateManager(t *testing.T) func()
// - setupGlobalStateManagerWithPeers(t *testing.T, peerCount int) ([]string, func())
// - createTempDB(t testing.TB) (*StateManager, func())
// - generateTestPeerID(seed int) peer.ID
// - createTestNodeBufferInfo(index int) *NodeBufferInfo
// =============================================================================

// =============================================================================
// 1. Empty Database Tests
// =============================================================================

func TestInterrogation_EmptyDatabase_IsDatabaseHealthy(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	if !IsDatabaseHealthy() {
		t.Error("Empty database should be healthy")
	}
}

func TestInterrogation_EmptyDatabase_GetDatabaseStats(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	stats := GetDatabaseStats()

	// Should not have error
	if err, hasErr := stats["error"]; hasErr {
		t.Errorf("Empty database stats should not have error, got: %v", err)
	}

	// Should have degraded_mode = false
	if degraded, ok := stats["degraded_mode"].(bool); !ok || degraded {
		t.Errorf("Expected degraded_mode=false, got: %v", stats["degraded_mode"])
	}
}

func TestInterrogation_EmptyDatabase_GetAllPeerStates(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	peers, err := GetAllPeerStates()
	if err != nil {
		t.Fatalf("GetAllPeerStates failed: %v", err)
	}

	if len(peers) != 0 {
		t.Errorf("Empty database should have 0 peers, got: %d", len(peers))
	}
}

func TestInterrogation_EmptyDatabase_DumpDatabaseState(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	snapshot, err := DumpDatabaseState()
	if err != nil {
		t.Fatalf("DumpDatabaseState failed: %v", err)
	}

	// Verify structure
	if snapshot == nil {
		t.Fatal("Snapshot should not be nil")
	}

	if snapshot.Peers == nil {
		t.Error("Peers map should be initialized (not nil)")
	}

	if len(snapshot.Peers) != 0 {
		t.Errorf("Empty database should have 0 peers, got: %d", len(snapshot.Peers))
	}

	if snapshot.Topics == nil {
		t.Error("Topics map should be initialized (not nil)")
	}

	if snapshot.ExportedAt.IsZero() {
		t.Error("ExportedAt should be set")
	}

	// ExportedAt should be recent (within last minute)
	if time.Since(snapshot.ExportedAt) > time.Minute {
		t.Error("ExportedAt should be recent")
	}

	if snapshot.Stats == nil {
		t.Error("Stats map should be initialized (not nil)")
	}
}

func TestInterrogation_EmptyDatabase_GetInvoiceQueueStatus(t *testing.T) {
	// Note: GetInvoiceQueueStatus doesn't require GlobalStateManager
	status := GetInvoiceQueueStatus()

	if status.Count != 0 {
		t.Errorf("Empty invoice queue should have count=0, got: %d", status.Count)
	}

	// When empty, times should be zero
	if !status.OldestTime.IsZero() && status.Count == 0 {
		t.Error("OldestTime should be zero when queue is empty")
	}
}

// =============================================================================
// 2. Populated Database Tests
// =============================================================================

func TestInterrogation_Populated_GetAllPeerStates(t *testing.T) {
	peerCount := 5
	peerIDs, cleanup := setupGlobalStateManagerWithPeers(t, peerCount)
	defer cleanup()

	peers, err := GetAllPeerStates()
	if err != nil {
		t.Fatalf("GetAllPeerStates failed: %v", err)
	}

	if len(peers) != peerCount {
		t.Errorf("Expected %d peers, got: %d", peerCount, len(peers))
	}

	// Verify each peer exists
	for _, expectedID := range peerIDs {
		if _, exists := peers[expectedID]; !exists {
			t.Errorf("Peer %s not found in result", expectedID)
		}
	}
}

func TestInterrogation_Populated_GetPeerStateByID(t *testing.T) {
	peerIDs, cleanup := setupGlobalStateManagerWithPeers(t, 3)
	defer cleanup()

	// Test retrieving specific peer
	targetPeerID := peerIDs[1]
	state, err := GetPeerStateByID(targetPeerID)
	if err != nil {
		t.Fatalf("GetPeerStateByID failed: %v", err)
	}

	if state == nil {
		t.Fatal("State should not be nil for existing peer")
	}

	// Verify SharedAccID is populated (from createTestNodeBufferInfo)
	// The helper sets SharedAccID to 7000000 + index
	if state.SharedAccID == 0 {
		t.Error("SharedAccID should be non-zero")
	}

	// Verify IP address is populated
	if state.LastOtherSideMultiAddress == "" {
		t.Error("LastOtherSideMultiAddress should not be empty")
	}
}

func TestInterrogation_Populated_DumpDatabaseState(t *testing.T) {
	peerCount := 10
	_, cleanup := setupGlobalStateManagerWithPeers(t, peerCount)
	defer cleanup()

	snapshot, err := DumpDatabaseState()
	if err != nil {
		t.Fatalf("DumpDatabaseState failed: %v", err)
	}

	// Verify peer count
	if len(snapshot.Peers) != peerCount {
		t.Errorf("Expected %d peers, got: %d", peerCount, len(snapshot.Peers))
	}

	// Verify each peer has valid data
	for peerID, state := range snapshot.Peers {
		if state.LastOtherSideMultiAddress == "" {
			t.Errorf("Peer %s missing LastOtherSideMultiAddress", peerID)
		}
		if state.SharedAccID == 0 {
			t.Errorf("Peer %s missing SharedAccID", peerID)
		}
	}

	// Verify stats are included
	if snapshot.Stats == nil || len(snapshot.Stats) == 0 {
		t.Error("Stats should be populated")
	}
}

func TestInterrogation_Populated_DataIntegrity(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	// Create a specific test peer with known values
	peerID := generateTestPeerID(42)
	originalInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/10.0.0.42/tcp/4001/p2p/QmTestPeer42",
		LibP2PState:               types.Connected,
		RendezvousState:           types.SendOK,
		IsOtherSideValidAccount:   true,
		SharedAccID:               9876543,
		SharedAccIDCreatedAt:      time.Now().Add(-24 * time.Hour),
		CacheReconnectAttempts:    3,
		LastCacheReconnectTime:    time.Now().Add(-1 * time.Hour),
	}

	GlobalStateManager.PersistPeer(peerID, originalInfo, true)
	time.Sleep(100 * time.Millisecond)

	// Retrieve and verify
	state, err := GetPeerStateByID(peerID.String())
	if err != nil {
		t.Fatalf("GetPeerStateByID failed: %v", err)
	}

	// Verify fields match
	if state.LastOtherSideMultiAddress != originalInfo.LastOtherSideMultiAddress {
		t.Errorf("LastOtherSideMultiAddress mismatch: %q vs %q",
			state.LastOtherSideMultiAddress, originalInfo.LastOtherSideMultiAddress)
	}

	if state.SharedAccID != originalInfo.SharedAccID {
		t.Errorf("SharedAccID mismatch: %d vs %d",
			state.SharedAccID, originalInfo.SharedAccID)
	}

	if state.CacheReconnectAttempts != originalInfo.CacheReconnectAttempts {
		t.Errorf("CacheReconnectAttempts mismatch: %d vs %d",
			state.CacheReconnectAttempts, originalInfo.CacheReconnectAttempts)
	}

	if state.LibP2PState != originalInfo.LibP2PState {
		t.Errorf("LibP2PState mismatch: %v vs %v",
			state.LibP2PState, originalInfo.LibP2PState)
	}
}

// =============================================================================
// 3. Error Condition Tests
// =============================================================================

func TestInterrogation_NilStateManager_IsDatabaseHealthy(t *testing.T) {
	// Ensure GlobalStateManager is nil
	oldSM := GlobalStateManager
	GlobalStateManager = nil
	defer func() { GlobalStateManager = oldSM }()

	if IsDatabaseHealthy() {
		t.Error("IsDatabaseHealthy should return false when GlobalStateManager is nil")
	}
}

func TestInterrogation_NilStateManager_GetDatabaseStats(t *testing.T) {
	oldSM := GlobalStateManager
	GlobalStateManager = nil
	defer func() { GlobalStateManager = oldSM }()

	stats := GetDatabaseStats()

	if stats["error"] == nil {
		t.Error("GetDatabaseStats should return error when GlobalStateManager is nil")
	}
}

func TestInterrogation_NilStateManager_GetAllPeerStates(t *testing.T) {
	oldSM := GlobalStateManager
	GlobalStateManager = nil
	defer func() { GlobalStateManager = oldSM }()

	_, err := GetAllPeerStates()
	if err == nil {
		t.Error("GetAllPeerStates should return error when GlobalStateManager is nil")
	}
}

func TestInterrogation_NilStateManager_DumpDatabaseState(t *testing.T) {
	oldSM := GlobalStateManager
	GlobalStateManager = nil
	defer func() { GlobalStateManager = oldSM }()

	_, err := DumpDatabaseState()
	if err == nil {
		t.Error("DumpDatabaseState should return error when GlobalStateManager is nil")
	}
}

func TestInterrogation_NilStateManager_GetPeerStateByID(t *testing.T) {
	oldSM := GlobalStateManager
	GlobalStateManager = nil
	defer func() { GlobalStateManager = oldSM }()

	_, err := GetPeerStateByID("16Uiu2HAmTestPeerID")
	if err == nil {
		t.Error("GetPeerStateByID should return error when GlobalStateManager is nil")
	}
}

func TestInterrogation_InvalidPeerID_GetPeerStateByID(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	// Test with invalid peer ID format
	_, err := GetPeerStateByID("not-a-valid-peer-id")
	if err == nil {
		t.Error("GetPeerStateByID should return error for invalid peer ID format")
	}
}

func TestInterrogation_NonExistentPeerID_GetPeerStateByID(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	// Use a valid peer ID format but non-existent
	nonExistentPeerID := generateTestPeerID(99999)
	_, err := GetPeerStateByID(nonExistentPeerID.String())

	// This should return error or nil state depending on implementation
	// The API documentation says it returns error if peer not found
	if err == nil {
		t.Error("GetPeerStateByID should return error for non-existent peer")
	}
}

func TestInterrogation_DegradedMode_DumpDatabaseState(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	// Force degraded mode
	GlobalStateManager.degradedModeMutex.Lock()
	GlobalStateManager.degradedMode = true
	GlobalStateManager.degradedModeMutex.Unlock()

	// Verify IsDatabaseHealthy returns false
	if IsDatabaseHealthy() {
		t.Error("IsDatabaseHealthy should return false in degraded mode")
	}

	// DumpDatabaseState should fail in degraded mode
	_, err := DumpDatabaseState()
	if err == nil {
		t.Error("DumpDatabaseState should return error in degraded mode")
	}
}

// =============================================================================
// 4. Stats Validation Tests
// =============================================================================

func TestInterrogation_Stats_ContainsExpectedKeys(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	stats := GetDatabaseStats()

	// Verify expected keys exist
	expectedKeys := []string{
		"degraded_mode",
		"writes_dropped",
		"corrupted_records_count",
	}

	for _, key := range expectedKeys {
		if _, exists := stats[key]; !exists {
			t.Errorf("Stats missing expected key: %s", key)
		}
	}
}

func TestInterrogation_Stats_WriteMetrics(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	// Add some peers to generate writes
	for i := 0; i < 5; i++ {
		peerID := generateTestPeerID(i)
		info := createTestNodeBufferInfo(i)
		GlobalStateManager.PersistPeer(peerID, info, true)
	}
	time.Sleep(100 * time.Millisecond)

	stats := GetDatabaseStats()

	// writes_dropped should be 0 in normal operation
	if dropped, ok := stats["writes_dropped"].(uint64); !ok {
		t.Log("writes_dropped key exists but may have different type")
	} else if dropped != 0 {
		t.Logf("Note: writes_dropped=%d (expected 0 in normal operation)", dropped)
	}
}

// =============================================================================
// 5. Concurrency Tests
// =============================================================================

func TestInterrogation_Concurrent_ReadOperations(t *testing.T) {
	_, cleanup := setupGlobalStateManagerWithPeers(t, 10)
	defer cleanup()

	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	errChan := make(chan error, goroutines*4) // 4 operations per goroutine

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Test IsDatabaseHealthy
				_ = IsDatabaseHealthy()

				// Test GetDatabaseStats
				stats := GetDatabaseStats()
				if stats["error"] != nil {
					errChan <- fmt.Errorf("GetDatabaseStats error: %v", stats["error"])
				}

				// Test GetAllPeerStates
				_, err := GetAllPeerStates()
				if err != nil {
					errChan <- fmt.Errorf("GetAllPeerStates error: %v", err)
				}

				// Test DumpDatabaseState
				_, err = DumpDatabaseState()
				if err != nil {
					errChan <- fmt.Errorf("DumpDatabaseState error: %v", err)
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("Concurrent read operations had %d errors", len(errors))
		for i, err := range errors {
			if i < 5 { // Show first 5 errors
				t.Errorf("  Error %d: %v", i, err)
			}
		}
	}
}

func TestInterrogation_Concurrent_ReadsDuringWrites(t *testing.T) {
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	const writeGoroutines = 5
	const readGoroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	stopWrite := make(chan struct{})
	errChan := make(chan error, (writeGoroutines+readGoroutines)*iterations)

	// Start writers
	for i := 0; i < writeGoroutines; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-stopWrite:
					return
				default:
					peerID := generateTestPeerID(writerID*1000 + j)
					info := createTestNodeBufferInfo(j)
					GlobalStateManager.PersistPeer(peerID, info, j%2 == 0) // Mix immediate and batched
				}
			}
		}(i)
	}

	// Start readers
	for i := 0; i < readGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Various read operations
				_ = IsDatabaseHealthy()

				peers, err := GetAllPeerStates()
				if err != nil {
					errChan <- fmt.Errorf("GetAllPeerStates error: %v", err)
				}
				_ = len(peers) // Use the result

				_, err = DumpDatabaseState()
				if err != nil {
					errChan <- fmt.Errorf("DumpDatabaseState error: %v", err)
				}
			}
		}()
	}

	wg.Wait()
	close(stopWrite)
	close(errChan)

	// Check for errors
	errorCount := 0
	for err := range errChan {
		errorCount++
		if errorCount <= 3 {
			t.Logf("Concurrent error: %v", err)
		}
	}

	if errorCount > 0 {
		t.Errorf("Had %d errors during concurrent read/write operations", errorCount)
	}
}

// =============================================================================
// 6. Invoice Queue Tests
// =============================================================================

func TestInterrogation_InvoiceQueue_AfterQueueing(t *testing.T) {
	// Create a test invoice (value, not pointer)
	testInvoice := QueuedInvoice{
		QueuedAt:    time.Now(),
		RetryCount:  0,
		SharedAccID: 12345,
	}

	// Queue it
	InvoiceQueueMutex.Lock()
	PendingInvoices = append(PendingInvoices, testInvoice)
	InvoiceQueueMutex.Unlock()

	// Clean up after test
	defer func() {
		InvoiceQueueMutex.Lock()
		PendingInvoices = nil
		InvoiceQueueMutex.Unlock()
	}()

	status := GetInvoiceQueueStatus()

	if status.Count != 1 {
		t.Errorf("Expected count=1, got: %d", status.Count)
	}

	if status.OldestTime.IsZero() {
		t.Error("OldestTime should not be zero when queue has items")
	}

	if status.NewestTime.IsZero() {
		t.Error("NewestTime should not be zero when queue has items")
	}
}

func TestInterrogation_InvoiceQueue_MultipleItems(t *testing.T) {
	// Clean up any existing invoices
	InvoiceQueueMutex.Lock()
	PendingInvoices = nil
	InvoiceQueueMutex.Unlock()

	// Queue multiple invoices with different times (values, not pointers)
	now := time.Now()
	invoices := []QueuedInvoice{
		{QueuedAt: now.Add(-5 * time.Minute), SharedAccID: 1},
		{QueuedAt: now.Add(-1 * time.Minute), SharedAccID: 2},
		{QueuedAt: now, SharedAccID: 3},
	}

	InvoiceQueueMutex.Lock()
	PendingInvoices = invoices
	InvoiceQueueMutex.Unlock()

	defer func() {
		InvoiceQueueMutex.Lock()
		PendingInvoices = nil
		InvoiceQueueMutex.Unlock()
	}()

	status := GetInvoiceQueueStatus()

	if status.Count != 3 {
		t.Errorf("Expected count=3, got: %d", status.Count)
	}

	// Oldest should be 5 minutes ago
	if status.OldestTime.After(now.Add(-4 * time.Minute)) {
		t.Error("OldestTime should be the oldest queued invoice")
	}

	// Newest should be recent
	if status.NewestTime.Before(now.Add(-30 * time.Second)) {
		t.Error("NewestTime should be the most recently queued invoice")
	}
}

// =============================================================================
// 7. Edge Case Tests
// =============================================================================

func TestInterrogation_LargePeerCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large peer count test in short mode")
	}

	peerCount := 100
	cleanup := setupGlobalStateManager(t)
	defer cleanup()

	// Manually add peers with batched writes and flush
	for i := 0; i < peerCount; i++ {
		peerID := generateTestPeerID(i)
		info := createTestNodeBufferInfo(i)
		GlobalStateManager.PersistPeer(peerID, info, false) // batched
	}
	// Flush to ensure all writes are persisted
	if err := GlobalStateManager.FlushAll(); err != nil {
		t.Fatalf("FlushAll failed: %v", err)
	}

	// Verify all peers are retrievable
	peers, err := GetAllPeerStates()
	if err != nil {
		t.Fatalf("GetAllPeerStates failed: %v", err)
	}

	if len(peers) != peerCount {
		t.Errorf("Expected %d peers, got: %d", peerCount, len(peers))
	}

	// Dump should also work
	snapshot, err := DumpDatabaseState()
	if err != nil {
		t.Fatalf("DumpDatabaseState failed: %v", err)
	}

	if len(snapshot.Peers) != peerCount {
		t.Errorf("Snapshot has %d peers, expected %d", len(snapshot.Peers), peerCount)
	}
}

func TestInterrogation_RepeatedDumps(t *testing.T) {
	_, cleanup := setupGlobalStateManagerWithPeers(t, 5)
	defer cleanup()

	// Multiple dumps should be consistent
	var firstPeerCount int
	for i := 0; i < 10; i++ {
		snapshot, err := DumpDatabaseState()
		if err != nil {
			t.Fatalf("DumpDatabaseState iteration %d failed: %v", i, err)
		}

		if i == 0 {
			firstPeerCount = len(snapshot.Peers)
		} else if len(snapshot.Peers) != firstPeerCount {
			t.Errorf("Inconsistent peer count: iteration 0 had %d, iteration %d has %d",
				firstPeerCount, i, len(snapshot.Peers))
		}
	}
}

// =============================================================================
// 8. Benchmark Tests for Interrogation APIs
// =============================================================================

func BenchmarkInterrogation_IsDatabaseHealthy(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-interrogation-*")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	sm, err := NewStateManager(dbPath)
	if err != nil {
		b.Fatalf("Failed to create StateManager: %v", err)
	}
	defer sm.Close()

	GlobalStateManager = sm
	defer func() { GlobalStateManager = nil }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsDatabaseHealthy()
	}
}

func BenchmarkInterrogation_GetDatabaseStats(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-interrogation-*")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	sm, err := NewStateManager(dbPath)
	if err != nil {
		b.Fatalf("Failed to create StateManager: %v", err)
	}
	defer sm.Close()

	GlobalStateManager = sm
	defer func() { GlobalStateManager = nil }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetDatabaseStats()
	}
}

func BenchmarkInterrogation_GetAllPeerStates_50(b *testing.B) {
	benchmarkGetAllPeerStates(b, 50)
}

func BenchmarkInterrogation_GetAllPeerStates_100(b *testing.B) {
	benchmarkGetAllPeerStates(b, 100)
}

func benchmarkGetAllPeerStates(b *testing.B, peerCount int) {
	tmpDir, err := os.MkdirTemp("", "bench-interrogation-*")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	sm, err := NewStateManager(dbPath)
	if err != nil {
		b.Fatalf("Failed to create StateManager: %v", err)
	}
	defer sm.Close()

	GlobalStateManager = sm
	defer func() { GlobalStateManager = nil }()

	// Populate with peers
	for i := 0; i < peerCount; i++ {
		peerID := generateTestPeerID(i)
		info := createTestNodeBufferInfo(i)
		sm.PersistPeer(peerID, info, false)
	}
	sm.FlushAll()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetAllPeerStates()
	}
}

func BenchmarkInterrogation_DumpDatabaseState_50(b *testing.B) {
	benchmarkDumpDatabaseState(b, 50)
}

func benchmarkDumpDatabaseState(b *testing.B, peerCount int) {
	tmpDir, err := os.MkdirTemp("", "bench-interrogation-*")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	sm, err := NewStateManager(dbPath)
	if err != nil {
		b.Fatalf("Failed to create StateManager: %v", err)
	}
	defer sm.Close()

	GlobalStateManager = sm
	defer func() { GlobalStateManager = nil }()

	// Populate with peers
	for i := 0; i < peerCount; i++ {
		peerID := generateTestPeerID(i)
		info := createTestNodeBufferInfo(i)
		sm.PersistPeer(peerID, info, false)
	}
	sm.FlushAll()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DumpDatabaseState()
	}
}

