package commonlib

import (
	"encoding/json"
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

// SerializeNodeBufferInfo converts NodeBufferInfo to JSON bytes
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

	return json.Marshal(serialized)
}

// DeserializeNodeBufferInfo converts JSON bytes to NodeBufferInfo
func DeserializeNodeBufferInfo(data []byte) (*NodeBufferInfo, error) {
	var serialized SerializedNodeBufferInfo
	if err := json.Unmarshal(data, &serialized); err != nil {
		return nil, err
	}

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

	return info, nil
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
