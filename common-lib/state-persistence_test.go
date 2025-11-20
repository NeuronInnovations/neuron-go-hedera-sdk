package commonlib

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStateManager(t *testing.T) {
	// Create temporary directory for test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test creating a new StateManager
	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	require.NotNil(t, sm)
	defer sm.Close()

	assert.Equal(t, dbPath, sm.GetDatabasePath())
	assert.False(t, sm.IsInDegradedMode())
}

func TestStateManager_PersistAndLoadPeer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm.Close()

	// Create a test peer
	peerID, err := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	require.NoError(t, err)

	// Create test peer info
	peerInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
		LibP2PState:               types.Connected,
		RendezvousState:           types.SendOK,
		IsOtherSideValidAccount:   true,
		NoOfConnectionAttempts:    2,
		LastConnectionAttempt:     time.Now(),
	}

	// Persist peer (immediate write)
	sm.PersistPeer(peerID, peerInfo, true)

	// Give immediate writer time to process
	time.Sleep(100 * time.Millisecond)

	// Flush to ensure write completes
	err = sm.FlushAll()
	require.NoError(t, err)

	// Load peer back
	loadedInfo, err := sm.LoadPeer(peerID)
	require.NoError(t, err)
	require.NotNil(t, loadedInfo)

	assert.Equal(t, peerInfo.LastOtherSideMultiAddress, loadedInfo.LastOtherSideMultiAddress)
	assert.Equal(t, peerInfo.LibP2PState, loadedInfo.LibP2PState)
	assert.Equal(t, peerInfo.IsOtherSideValidAccount, loadedInfo.IsOtherSideValidAccount)
	assert.Equal(t, peerInfo.NoOfConnectionAttempts, loadedInfo.NoOfConnectionAttempts)
}

func TestStateManager_LoadAllPeers(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm.Close()

	// Create multiple test peers
	peer1, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	peer2, _ := peer.Decode("QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ")

	peerInfo1 := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
		LibP2PState:               types.Connected,
	}

	peerInfo2 := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.101/udp/4002/quic-v1",
		LibP2PState:               types.ConnectionLost,
	}

	// Persist peers
	sm.PersistPeer(peer1, peerInfo1, true)
	sm.PersistPeer(peer2, peerInfo2, true)

	// Give immediate writer time to process
	time.Sleep(100 * time.Millisecond)

	err = sm.FlushAll()
	require.NoError(t, err)

	// Load all peers
	nodeBuffers, err := sm.LoadAllPeers()
	require.NoError(t, err)
	require.NotNil(t, nodeBuffers)

	assert.Len(t, nodeBuffers.Buffers, 2)
	assert.Contains(t, nodeBuffers.Buffers, peer1)
	assert.Contains(t, nodeBuffers.Buffers, peer2)
}

func TestStateManager_RemovePeer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm.Close()

	// Create and persist a test peer
	peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	peerInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
	}

	sm.PersistPeer(peerID, peerInfo, true)

	// Give immediate writer time to process
	time.Sleep(100 * time.Millisecond)

	err = sm.FlushAll()
	require.NoError(t, err)

	// Verify peer exists
	_, err = sm.LoadPeer(peerID)
	require.NoError(t, err)

	// Remove peer
	err = sm.RemovePeer(peerID)
	require.NoError(t, err)

	// Verify peer is removed
	_, err = sm.LoadPeer(peerID)
	assert.Error(t, err)
}

func TestStateManager_TopicPosition(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)

	// Create test topic position
	topicKey := "0.0.12345"
	timestamp := time.Now().UTC()

	// Persist topic position
	sm.PersistTopicPosition(topicKey, timestamp)

	// Close StateManager to ensure all writes are flushed
	err = sm.Close()
	require.NoError(t, err)

	// Reopen to verify persistence
	sm2, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm2.Close()

	// Load topic position
	loadedTime, err := sm2.LoadTopicPosition(topicKey)
	require.NoError(t, err)

	// Compare timestamps (allowing for small rounding differences)
	assert.WithinDuration(t, timestamp, loadedTime, time.Second)
}

func TestStateManager_ClearAll(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm.Close()

	// Add some data
	peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	peerInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
	}
	sm.PersistPeer(peerID, peerInfo, true)

	topicKey := "0.0.12345"
	sm.PersistTopicPosition(topicKey, time.Now())

	err = sm.FlushAll()
	require.NoError(t, err)

	// Clear all data
	err = sm.ClearAll()
	require.NoError(t, err)

	// Verify data is cleared
	_, err = sm.LoadPeer(peerID)
	assert.Error(t, err)

	_, err = sm.LoadTopicPosition(topicKey)
	assert.Error(t, err)
}

func TestStateManager_CorruptionRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a corrupted database file
	err := os.WriteFile(dbPath, []byte("corrupted data"), 0600)
	require.NoError(t, err)

	// Try to open StateManager - should recover gracefully
	sm, err := NewStateManager(dbPath)
	// Should not error, but might be in degraded mode initially
	// or should recover by creating a fresh database
	require.NoError(t, err)
	defer sm.Close()

	// Check if backup was created
	matches, _ := filepath.Glob(dbPath + ".corrupted.*")
	if len(matches) > 0 {
		t.Log("Corruption was detected and backup was created")
	}
}

func TestStateManager_GracefulDegradation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create StateManager first
	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)

	// Set to degraded mode manually for testing
	sm.degradedModeMutex.Lock()
	sm.degradedMode = true
	if sm.db != nil {
		sm.db.Close()
		sm.db = nil
	}
	sm.degradedModeMutex.Unlock()

	defer sm.Close()

	// Should be in degraded mode
	assert.True(t, sm.IsInDegradedMode())

	// Operations should not crash in degraded mode
	peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	peerInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
	}

	// These should not panic or error in degraded mode
	sm.PersistPeer(peerID, peerInfo, true)
	sm.PersistTopicPosition("0.0.12345", time.Now())
	sm.FlushAll()
}

func TestSerializeDeserializeNodeBufferInfo(t *testing.T) {
	original := &NodeBufferInfo{
		LastOtherSideMultiAddress:      "/ip4/192.168.1.100/udp/4001/quic-v1",
		LibP2PState:                    types.Connected,
		RendezvousState:                types.SendOK,
		IsOtherSideValidAccount:        true,
		NoOfConnectionAttempts:         5,
		LastConnectionAttempt:          time.Now(),
		NextScheduledConnectionAttempt: time.Now().Add(10 * time.Second),
	}

	// Serialize
	data, err := SerializeNodeBufferInfo(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Deserialize
	deserialized, err := DeserializeNodeBufferInfo(data)
	require.NoError(t, err)
	require.NotNil(t, deserialized)

	// Verify fields match
	assert.Equal(t, original.LastOtherSideMultiAddress, deserialized.LastOtherSideMultiAddress)
	assert.Equal(t, original.LibP2PState, deserialized.LibP2PState)
	assert.Equal(t, original.RendezvousState, deserialized.RendezvousState)
	assert.Equal(t, original.IsOtherSideValidAccount, deserialized.IsOtherSideValidAccount)
	assert.Equal(t, original.NoOfConnectionAttempts, deserialized.NoOfConnectionAttempts)
}

func TestStateManager_BatchedWrites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm.Close()

	// Queue multiple batched writes
	for i := 0; i < 10; i++ {
		peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
		peerInfo := &NodeBufferInfo{
			LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
			NoOfConnectionAttempts:    i,
		}
		sm.PersistPeer(peerID, peerInfo, false) // Batched write
	}

	// Flush to ensure all writes complete
	err = sm.FlushAll()
	require.NoError(t, err)

	// Verify last write succeeded
	peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	loadedInfo, err := sm.LoadPeer(peerID)
	require.NoError(t, err)
	assert.Equal(t, 9, loadedInfo.NoOfConnectionAttempts)
}

func TestStateManager_DoubleClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)

	// First close should succeed
	err1 := sm.Close()
	assert.NoError(t, err1)

	// Second close should NOT panic and return same error
	err2 := sm.Close()
	assert.Equal(t, err1, err2)

	// Third close should also be safe
	err3 := sm.Close()
	assert.Equal(t, err1, err3)
}

func TestStateManager_WriteAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)

	// Close the StateManager
	err = sm.Close()
	require.NoError(t, err)

	// Writes after close should NOT panic
	peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	peerInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
	}

	// Should not panic
	sm.PersistPeer(peerID, peerInfo, true)
	sm.PersistTopicPosition("0.0.12345", time.Now())
	sm.PersistMetadata("test", "value")

	// No assertions needed - we're just verifying no panic
}

func TestStateManager_DegradedMode(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a read-only directory to force database open failure
	roDir := filepath.Join(tmpDir, "readonly")
	err := os.Mkdir(roDir, 0500) // Read + execute only, no write
	require.NoError(t, err)

	dbPath := filepath.Join(roDir, "test.db")

	// Open should succeed even though database can't be created (degraded mode)
	sm, err := NewStateManager(dbPath)
	require.NoError(t, err)
	defer sm.Close()

	// Should be in degraded mode due to permission error
	assert.True(t, sm.IsInDegradedMode())

	// Writes in degraded mode should not crash
	peerID, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")
	peerInfo := &NodeBufferInfo{
		LastOtherSideMultiAddress: "/ip4/192.168.1.100/udp/4001/quic-v1",
	}

	// Write multiple times to trigger warning
	for i := 0; i < 150; i++ {
		sm.PersistPeer(peerID, peerInfo, true)
	}

	// Check stats
	stats := sm.GetStats()
	assert.True(t, stats["degraded_mode"].(bool))
	assert.Greater(t, stats["writes_dropped"].(uint64), uint64(0))
}
