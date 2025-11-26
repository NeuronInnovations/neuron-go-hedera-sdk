package commonlib

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

// WriteClassification specifies whether a write should be immediate or batched
type WriteClassification int

const (
	// WriteBatched indicates a non-critical write that can be batched
	WriteBatched WriteClassification = iota
	// WriteImmediate indicates a critical write that should be persisted immediately
	WriteImmediate
)

// Serialization format constants for data integrity
const (
	// serializationVersion is used for format migrations
	serializationVersion byte = 1
	// checksumSize is the size of CRC32 checksum in bytes
	checksumSize int = 4
	// versionSize is the size of version byte
	versionSize int = 1
	// headerSize is the total header size (version + checksum)
	headerSize int = versionSize + checksumSize
)

// Data integrity errors
var (
	// ErrChecksumMismatch indicates data corruption was detected
	ErrChecksumMismatch = errors.New("data integrity check failed: checksum mismatch")
	// ErrDataTooShort indicates the data is too short to contain valid header
	ErrDataTooShort = errors.New("data integrity check failed: data too short")
	// ErrVersionMismatch indicates an unsupported serialization version
	ErrVersionMismatch = errors.New("data format version not supported")
)

// SerializedNodeBufferInfo is the JSON-serializable representation of NodeBufferInfo
type SerializedNodeBufferInfo struct {
	LastOtherSideMultiAddress      string                    `json:"last_other_side_multi_address"`
	LibP2PState                    types.ConnectionState     `json:"lib_p2p_state"`
	RendezvousState                types.RendezvousState     `json:"rendezvous_state"`
	IsOtherSideValidAccount        bool                      `json:"is_other_side_valid_account"`
	NoOfConnectionAttempts         int                       `json:"no_of_connection_attempts"`
	LastConnectionAttempt          time.Time                 `json:"last_connection_attempt"`
	NextScheduledConnectionAttempt time.Time                 `json:"next_scheduled_connection_attempt"`
	RequestOrResponse              types.TopicPostalEnvelope `json:"request_or_response"`
	NextScheduleRequestTime        time.Time                 `json:"next_schedule_request_time"`
	LastGoodsReceivedTime          time.Time                 `json:"last_goods_received_time"`
	SharedAccID                    uint64                    `json:"shared_acc_id"`
	SharedAccIDCreatedAt           time.Time                 `json:"shared_acc_id_created_at"`
}

// SerializeNodeBufferInfo converts NodeBufferInfo to checksummed bytes
// Format: [version:1byte][crc32:4bytes][json:Nbytes]
// This format allows detection of silent data corruption on SD cards
func SerializeNodeBufferInfo(info *NodeBufferInfo) ([]byte, error) {
	serialized := SerializedNodeBufferInfo{
		LastOtherSideMultiAddress:      info.LastOtherSideMultiAddress,
		LibP2PState:                    info.LibP2PState,
		RendezvousState:                info.RendezvousState,
		IsOtherSideValidAccount:        info.IsOtherSideValidAccount,
		NoOfConnectionAttempts:         info.NoOfConnectionAttempts,
		LastConnectionAttempt:          info.LastConnectionAttempt,
		NextScheduledConnectionAttempt: info.NextScheduledConnectionAttempt,
		RequestOrResponse:              info.RequestOrResponse,
		NextScheduleRequestTime:        info.NextScheduleRequestTime,
		LastGoodsReceivedTime:          info.LastGoodsReceivedTime,
		SharedAccID:                    info.SharedAccID,
		SharedAccIDCreatedAt:           info.SharedAccIDCreatedAt,
	}

	jsonData, err := json.Marshal(serialized)
	if err != nil {
		return nil, err
	}

	// Build result: [version][checksum][json]
	result := make([]byte, headerSize+len(jsonData))
	result[0] = serializationVersion
	checksum := crc32.ChecksumIEEE(jsonData)
	binary.BigEndian.PutUint32(result[versionSize:headerSize], checksum)
	copy(result[headerSize:], jsonData)

	return result, nil
}

// DeserializeNodeBufferInfo converts checksummed bytes to NodeBufferInfo
// It validates the checksum to detect data corruption and falls back to
// legacy format for backward compatibility with existing data
func DeserializeNodeBufferInfo(data []byte) (*NodeBufferInfo, error) {
	// Minimum valid data: header + "{}" (empty JSON object)
	minSize := headerSize + 2
	if len(data) < minSize {
		// Attempt legacy format (no checksum) for backward compatibility
		return deserializeLegacyFormat(data)
	}

	version := data[0]

	// Check if this looks like versioned data (version 1) or legacy JSON
	// Legacy JSON would start with '{' (0x7B) which is not a valid version
	if version != serializationVersion {
		// Unknown version or legacy format - try legacy deserialization
		return deserializeLegacyFormat(data)
	}

	// Extract and validate checksum
	storedChecksum := binary.BigEndian.Uint32(data[versionSize:headerSize])
	jsonData := data[headerSize:]

	calculatedChecksum := crc32.ChecksumIEEE(jsonData)
	if calculatedChecksum != storedChecksum {
		return nil, ErrChecksumMismatch
	}

	// Deserialize JSON payload
	var serialized SerializedNodeBufferInfo
	if err := json.Unmarshal(jsonData, &serialized); err != nil {
		return nil, err
	}

	info := buildNodeBufferInfoFromSerialized(&serialized)
	return info, nil
}

// deserializeLegacyFormat handles data without checksums (backward compatibility)
// This ensures existing databases continue to work after the upgrade
func deserializeLegacyFormat(data []byte) (*NodeBufferInfo, error) {
	var serialized SerializedNodeBufferInfo
	if err := json.Unmarshal(data, &serialized); err != nil {
		return nil, err
	}
	info := buildNodeBufferInfoFromSerialized(&serialized)
	return info, nil
}

// buildNodeBufferInfoFromSerialized constructs NodeBufferInfo from serialized data
// and performs any necessary migrations
func buildNodeBufferInfoFromSerialized(serialized *SerializedNodeBufferInfo) *NodeBufferInfo {
	info := &NodeBufferInfo{
		LastOtherSideMultiAddress:      serialized.LastOtherSideMultiAddress,
		LibP2PState:                    serialized.LibP2PState,
		RendezvousState:                serialized.RendezvousState,
		IsOtherSideValidAccount:        serialized.IsOtherSideValidAccount,
		NoOfConnectionAttempts:         serialized.NoOfConnectionAttempts,
		LastConnectionAttempt:          serialized.LastConnectionAttempt,
		NextScheduledConnectionAttempt: serialized.NextScheduledConnectionAttempt,
		RequestOrResponse:              serialized.RequestOrResponse,
		NextScheduleRequestTime:        serialized.NextScheduleRequestTime,
		LastGoodsReceivedTime:          serialized.LastGoodsReceivedTime,
		SharedAccID:                    serialized.SharedAccID,
		SharedAccIDCreatedAt:           serialized.SharedAccIDCreatedAt,
	}

	// Migration: Try to extract SharedAccID from RequestOrResponse.Message if not directly stored
	if info.SharedAccID == 0 && info.RequestOrResponse.Message != nil {
		info.SharedAccID = extractSharedAccIDFromMessage(info.RequestOrResponse.Message)
	}

	return info
}

// extractSharedAccIDFromMessage attempts to extract SharedAccID from various message formats
func extractSharedAccIDFromMessage(message interface{}) uint64 {
	switch msg := message.(type) {
	case *types.NeuronServiceRequestMsg:
		return msg.SharedAccID
	case map[string]interface{}:
		// Handle deserialized JSON where interface{} becomes map[string]interface{}
		if a, ok := msg["a"].(float64); ok {
			return uint64(a)
		}
		if a, ok := msg["SharedAccID"].(float64); ok {
			return uint64(a)
		}
	}
	return 0
}

// PeerWrite represents a pending write operation for a peer
type PeerWrite struct {
	PeerID         peer.ID
	Info           *NodeBufferInfo
	Classification WriteClassification
}

// TopicPositionWrite represents a pending write operation for a topic position
type TopicPositionWrite struct {
	TopicKey  string
	Timestamp time.Time
}

// MetadataWrite represents a pending write operation for metadata
type MetadataWrite struct {
	Key   string
	Value string
}

// ============================================================================
// PeerInfo Cache Types - For blockchain fallback strategy
// ============================================================================

// CacheTTL constants for PeerInfo cache staleness checks
const (
	// DefaultPeerInfoCacheTTL is the default time-to-live for cached PeerInfo data
	// After this duration, blockchain queries are preferred over cached data
	DefaultPeerInfoCacheTTL = 24 * time.Hour
)

// CachedPeerInfo represents cached blockchain PeerInfo data with timestamp
// This is used for fallback when blockchain queries fail
type CachedPeerInfo struct {
	Available   bool      `json:"available"`
	PeerID      string    `json:"peer_id"`
	StdOutTopic uint64    `json:"std_out_topic"`
	StdInTopic  uint64    `json:"std_in_topic"`
	StdErrTopic uint64    `json:"std_err_topic"`
	CachedAt    time.Time `json:"cached_at"`
}

// CachedPeerList represents cached list of all registered peer addresses
// This is used for fallback when blockchain queries fail
type CachedPeerList struct {
	Addresses []string  `json:"addresses"`
	CachedAt  time.Time `json:"cached_at"`
}

// PeerInfoCacheWrite represents a pending write operation for PeerInfo cache
type PeerInfoCacheWrite struct {
	EvmAddress string
	Info       *CachedPeerInfo
}

// PeerListCacheWrite represents a pending write operation for peer list cache
type PeerListCacheWrite struct {
	List *CachedPeerList
}

// SerializeCachedPeerInfo converts CachedPeerInfo to checksummed bytes
// Format: [version:1byte][crc32:4bytes][json:Nbytes]
func SerializeCachedPeerInfo(info *CachedPeerInfo) ([]byte, error) {
	jsonData, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}

	// Build result: [version][checksum][json]
	result := make([]byte, headerSize+len(jsonData))
	result[0] = serializationVersion
	checksum := crc32.ChecksumIEEE(jsonData)
	binary.BigEndian.PutUint32(result[versionSize:headerSize], checksum)
	copy(result[headerSize:], jsonData)

	return result, nil
}

// DeserializeCachedPeerInfo converts checksummed bytes to CachedPeerInfo
// It validates the checksum to detect data corruption
func DeserializeCachedPeerInfo(data []byte) (*CachedPeerInfo, error) {
	// Minimum valid data: header + "{}" (empty JSON object)
	minSize := headerSize + 2
	if len(data) < minSize {
		// Attempt legacy format (no checksum) for backward compatibility
		return deserializeCachedPeerInfoLegacy(data)
	}

	version := data[0]

	// Check if this looks like versioned data or legacy JSON
	if version != serializationVersion {
		return deserializeCachedPeerInfoLegacy(data)
	}

	// Extract and validate checksum
	storedChecksum := binary.BigEndian.Uint32(data[versionSize:headerSize])
	jsonData := data[headerSize:]

	calculatedChecksum := crc32.ChecksumIEEE(jsonData)
	if calculatedChecksum != storedChecksum {
		return nil, ErrChecksumMismatch
	}

	// Deserialize JSON payload
	var info CachedPeerInfo
	if err := json.Unmarshal(jsonData, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// deserializeCachedPeerInfoLegacy handles data without checksums (backward compatibility)
func deserializeCachedPeerInfoLegacy(data []byte) (*CachedPeerInfo, error) {
	var info CachedPeerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SerializeCachedPeerList converts CachedPeerList to checksummed bytes
// Format: [version:1byte][crc32:4bytes][json:Nbytes]
func SerializeCachedPeerList(list *CachedPeerList) ([]byte, error) {
	jsonData, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}

	// Build result: [version][checksum][json]
	result := make([]byte, headerSize+len(jsonData))
	result[0] = serializationVersion
	checksum := crc32.ChecksumIEEE(jsonData)
	binary.BigEndian.PutUint32(result[versionSize:headerSize], checksum)
	copy(result[headerSize:], jsonData)

	return result, nil
}

// DeserializeCachedPeerList converts checksummed bytes to CachedPeerList
// It validates the checksum to detect data corruption
func DeserializeCachedPeerList(data []byte) (*CachedPeerList, error) {
	// Minimum valid data: header + "{}" (empty JSON object)
	minSize := headerSize + 2
	if len(data) < minSize {
		// Attempt legacy format (no checksum) for backward compatibility
		return deserializeCachedPeerListLegacy(data)
	}

	version := data[0]

	// Check if this looks like versioned data or legacy JSON
	if version != serializationVersion {
		return deserializeCachedPeerListLegacy(data)
	}

	// Extract and validate checksum
	storedChecksum := binary.BigEndian.Uint32(data[versionSize:headerSize])
	jsonData := data[headerSize:]

	calculatedChecksum := crc32.ChecksumIEEE(jsonData)
	if calculatedChecksum != storedChecksum {
		return nil, ErrChecksumMismatch
	}

	// Deserialize JSON payload
	var list CachedPeerList
	if err := json.Unmarshal(jsonData, &list); err != nil {
		return nil, err
	}

	return &list, nil
}

// deserializeCachedPeerListLegacy handles data without checksums (backward compatibility)
func deserializeCachedPeerListLegacy(data []byte) (*CachedPeerList, error) {
	var list CachedPeerList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// IsCacheStale checks if cached data has exceeded its TTL
func IsCacheStale(cachedAt time.Time, maxAge time.Duration) bool {
	return time.Since(cachedAt) > maxAge
}
