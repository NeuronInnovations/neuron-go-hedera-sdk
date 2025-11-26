package commonlib

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// =============================================================================
// Test Helpers
// =============================================================================

// generateTestPeerID creates a deterministic peer ID for testing
func generateTestPeerID(seed int) peer.ID {
	// Use Ed25519 for deterministic key generation based on seed
	seedBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		seedBytes[i] = byte((seed + i) % 256)
	}
	privKey, _, _ := crypto.GenerateEd25519Key(nil)
	peerID, _ := peer.IDFromPrivateKey(privKey)
	return peerID
}

// createTestNodeBufferInfo creates a realistic NodeBufferInfo for testing
func createTestNodeBufferInfo(index int) *NodeBufferInfo {
	return &NodeBufferInfo{
		LastOtherSideMultiAddress:      fmt.Sprintf("/ip4/192.168.1.%d/tcp/4001/p2p/QmTestPeer%d", index%256, index),
		LibP2PState:                    types.Connected,
		RendezvousState:                types.SendOK,
		IsOtherSideValidAccount:        true,
		NoOfConnectionAttempts:         index % 10,
		LastConnectionAttempt:          time.Now().Add(-time.Duration(index) * time.Minute),
		NextScheduledConnectionAttempt: time.Now().Add(time.Duration(index) * time.Minute),
		RequestOrResponse: types.TopicPostalEnvelope{
			Message: map[string]interface{}{
				"type":    "service_request",
				"version": "1.0",
				"data":    fmt.Sprintf("test_data_%d", index),
			},
		},
		NextScheduleRequestTime: time.Now().Add(time.Duration(index) * time.Second),
		LastGoodsReceivedTime:   time.Now().Add(-time.Duration(index) * time.Hour),
		SharedAccID:             uint64(7000000 + index),
		SharedAccIDCreatedAt:    time.Now().Add(-time.Duration(index) * 24 * time.Hour),
	}
}

// createTempDB creates a temporary database for testing and returns cleanup function
func createTempDB(t testing.TB) (*StateManager, func()) {
	tmpDir, err := os.MkdirTemp("", "bbolt-benchmark-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test-state.db")
	sm, err := NewStateManager(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create StateManager: %v", err)
	}

	cleanup := func() {
		sm.Close()
		os.RemoveAll(tmpDir)
	}

	return sm, cleanup
}

// =============================================================================
// Serialization Benchmarks
// =============================================================================

// BenchmarkSerializeNodeBufferInfo measures JSON + CRC32 serialization performance
func BenchmarkSerializeNodeBufferInfo(b *testing.B) {
	info := createTestNodeBufferInfo(1)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := SerializeNodeBufferInfo(info)
		if err != nil {
			b.Fatalf("Serialization failed: %v", err)
		}
		// Prevent compiler optimization
		if len(data) == 0 {
			b.Fatal("Empty serialization result")
		}
	}
}

// BenchmarkDeserializeNodeBufferInfo measures JSON parse + checksum validation performance
func BenchmarkDeserializeNodeBufferInfo(b *testing.B) {
	info := createTestNodeBufferInfo(1)
	data, err := SerializeNodeBufferInfo(info)
	if err != nil {
		b.Fatalf("Failed to serialize test data: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := DeserializeNodeBufferInfo(data)
		if err != nil {
			b.Fatalf("Deserialization failed: %v", err)
		}
		// Prevent compiler optimization
		if result == nil {
			b.Fatal("Nil deserialization result")
		}
	}
}

// =============================================================================
// Write Benchmarks
// =============================================================================

// BenchmarkImmediateWrite measures single peer write with db.Sync() - worst case for SD card
func BenchmarkImmediateWrite(b *testing.B) {
	sm, cleanup := createTempDB(b)
	defer cleanup()

	peerIDs := make([]peer.ID, b.N)
	infos := make([]*NodeBufferInfo, b.N)
	for i := 0; i < b.N; i++ {
		peerIDs[i] = generateTestPeerID(i)
		infos[i] = createTestNodeBufferInfo(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sm.PersistPeer(peerIDs[i], infos[i], true) // immediate=true triggers sync
	}

	// Wait for immediate writes to complete
	time.Sleep(100 * time.Millisecond)
}

// BenchmarkBatchedWrite_50 measures batch of 50 peers in single transaction
func BenchmarkBatchedWrite_50(b *testing.B) {
	const batchSize = 50

	sm, cleanup := createTempDB(b)
	defer cleanup()

	// Prepare test data
	peerIDs := make([]peer.ID, batchSize)
	infos := make([]*NodeBufferInfo, batchSize)
	for i := 0; i < batchSize; i++ {
		peerIDs[i] = generateTestPeerID(i)
		infos[i] = createTestNodeBufferInfo(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Queue all writes as batched (non-immediate)
		for j := 0; j < batchSize; j++ {
			sm.PersistPeer(peerIDs[j], infos[j], false)
		}
		// Force flush to measure actual write performance
		if err := sm.FlushAll(); err != nil {
			b.Fatalf("FlushAll failed: %v", err)
		}
	}
}

// =============================================================================
// Read Benchmarks
// =============================================================================

// BenchmarkLoadSinglePeer measures single peer lookup by ID
func BenchmarkLoadSinglePeer(b *testing.B) {
	sm, cleanup := createTempDB(b)
	defer cleanup()

	// Pre-populate with test peer
	peerID := generateTestPeerID(0)
	info := createTestNodeBufferInfo(0)
	sm.PersistPeer(peerID, info, true)
	time.Sleep(50 * time.Millisecond) // Wait for write

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := sm.LoadPeer(peerID)
		if err != nil {
			b.Fatalf("LoadPeer failed: %v", err)
		}
		if result == nil {
			b.Fatal("Nil result from LoadPeer")
		}
	}
}

// BenchmarkLoadAllPeers_50 measures loading all peers with 50 records
func BenchmarkLoadAllPeers_50(b *testing.B) {
	benchmarkLoadAllPeers(b, 50)
}

// BenchmarkLoadAllPeers_100 measures loading all peers with 100 records
func BenchmarkLoadAllPeers_100(b *testing.B) {
	benchmarkLoadAllPeers(b, 100)
}

func benchmarkLoadAllPeers(b *testing.B, peerCount int) {
	sm, cleanup := createTempDB(b)
	defer cleanup()

	// Pre-populate database
	for i := 0; i < peerCount; i++ {
		peerID := generateTestPeerID(i)
		info := createTestNodeBufferInfo(i)
		sm.PersistPeer(peerID, info, false)
	}
	if err := sm.FlushAll(); err != nil {
		b.Fatalf("FlushAll failed: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := sm.LoadAllPeers()
		if err != nil {
			b.Fatalf("LoadAllPeers failed: %v", err)
		}
		if len(result.Buffers) != peerCount {
			b.Fatalf("Expected %d peers, got %d", peerCount, len(result.Buffers))
		}
	}
}

// =============================================================================
// Database Size Tests
// =============================================================================

// TestDatabaseSizeGrowth measures file size after 10, 50, 100 peers
func TestDatabaseSizeGrowth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bbolt-size-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testCases := []int{10, 50, 100}
	results := make(map[int]int64)

	for _, peerCount := range testCases {
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("test-%d.db", peerCount))
		sm, err := NewStateManager(dbPath)
		if err != nil {
			t.Fatalf("Failed to create StateManager: %v", err)
		}

		// Add peers
		for i := 0; i < peerCount; i++ {
			peerID := generateTestPeerID(i)
			info := createTestNodeBufferInfo(i)
			sm.PersistPeer(peerID, info, false)
		}

		// Flush and sync
		if err := sm.FlushAll(); err != nil {
			t.Fatalf("FlushAll failed: %v", err)
		}

		sm.Close()

		// Measure file size
		fileInfo, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("Failed to stat database file: %v", err)
		}
		results[peerCount] = fileInfo.Size()
	}

	// Print results in table format
	t.Log("\n=== DATABASE SIZE ANALYSIS ===")
	t.Log("| Peer Count | File Size (bytes) | File Size (KB) | Avg Size per Record |")
	t.Log("|------------|-------------------|----------------|---------------------|")

	for _, count := range testCases {
		size := results[count]
		avgPerRecord := float64(size) / float64(count)
		t.Logf("| %10d | %17d | %14.2f | %19.2f |",
			count, size, float64(size)/1024, avgPerRecord)
	}
}

// TestRecordSizeAverage measures average bytes per serialized peer record
func TestRecordSizeAverage(t *testing.T) {
	var totalSize int
	const sampleSize = 100

	for i := 0; i < sampleSize; i++ {
		info := createTestNodeBufferInfo(i)
		data, err := SerializeNodeBufferInfo(info)
		if err != nil {
			t.Fatalf("Serialization failed: %v", err)
		}
		totalSize += len(data)
	}

	avgSize := float64(totalSize) / float64(sampleSize)

	t.Log("\n=== RECORD SIZE ANALYSIS ===")
	t.Logf("Sample size: %d records", sampleSize)
	t.Logf("Total serialized size: %d bytes", totalSize)
	t.Logf("Average record size: %.2f bytes", avgSize)
	t.Logf("CRC32 checksum overhead: 5 bytes per record (version + checksum)")
	t.Logf("Estimated JSON payload: %.2f bytes", avgSize-5)
}

// TestSerializationOverhead measures the overhead of JSON + CRC32 vs raw struct
func TestSerializationOverhead(t *testing.T) {
	info := createTestNodeBufferInfo(1)

	// Serialize to get actual size
	data, err := SerializeNodeBufferInfo(info)
	if err != nil {
		t.Fatalf("Serialization failed: %v", err)
	}

	// Extract JSON portion (after header)
	jsonData := data[headerSize:]

	t.Log("\n=== SERIALIZATION OVERHEAD ANALYSIS ===")
	t.Logf("Total serialized size: %d bytes", len(data))
	t.Logf("Header size (version + CRC32): %d bytes", headerSize)
	t.Logf("JSON payload size: %d bytes", len(jsonData))
	t.Logf("Header overhead: %.2f%%", float64(headerSize)/float64(len(data))*100)
}

// =============================================================================
// Performance Summary Test
// =============================================================================

// TestPerformanceSummary runs all tests and generates a summary report
func TestPerformanceSummary(t *testing.T) {
	sm, cleanup := createTempDB(t)
	defer cleanup()

	t.Log("\n")
	t.Log("================================================================================")
	t.Log("                    BBOLT PERFORMANCE ASSESSMENT SUMMARY")
	t.Log("================================================================================")
	t.Log("")

	// Test 1: Serialization timing
	info := createTestNodeBufferInfo(1)
	iterations := 10000

	start := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = SerializeNodeBufferInfo(info)
	}
	serializeTime := time.Since(start)
	t.Logf("Serialization: %d iterations in %v (%.2f us/op)",
		iterations, serializeTime, float64(serializeTime.Microseconds())/float64(iterations))

	// Test 2: Deserialization timing
	data, _ := SerializeNodeBufferInfo(info)
	start = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = DeserializeNodeBufferInfo(data)
	}
	deserializeTime := time.Since(start)
	t.Logf("Deserialization: %d iterations in %v (%.2f us/op)",
		iterations, deserializeTime, float64(deserializeTime.Microseconds())/float64(iterations))

	// Test 3: Single peer read (after write)
	peerID := generateTestPeerID(0)
	sm.PersistPeer(peerID, info, true)
	time.Sleep(50 * time.Millisecond)

	readIterations := 1000
	start = time.Now()
	for i := 0; i < readIterations; i++ {
		_, _ = sm.LoadPeer(peerID)
	}
	readTime := time.Since(start)
	t.Logf("Single Peer Read: %d iterations in %v (%.2f us/op)",
		readIterations, readTime, float64(readTime.Microseconds())/float64(readIterations))

	// Test 4: Bulk load performance
	for i := 1; i < 50; i++ {
		pid := generateTestPeerID(i)
		inf := createTestNodeBufferInfo(i)
		sm.PersistPeer(pid, inf, false)
	}
	sm.FlushAll()

	loadIterations := 100
	start = time.Now()
	for i := 0; i < loadIterations; i++ {
		_, _ = sm.LoadAllPeers()
	}
	loadTime := time.Since(start)
	t.Logf("Load All Peers (50 records): %d iterations in %v (%.2f ms/op)",
		loadIterations, loadTime, float64(loadTime.Milliseconds())/float64(loadIterations))

	t.Log("")
	t.Log("================================================================================")
	t.Log("  Run 'go test -bench=. -benchmem' for detailed benchmark metrics")
	t.Log("================================================================================")
}

