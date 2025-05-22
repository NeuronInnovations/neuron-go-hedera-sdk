package commonlib

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Package-level variable for the BoltDB instance
//var db *bbolt.DB

// Package-level variable for the NodeBuffers instance
var NodeBuffersInstance *NodeBuffers

// Initialize the database in the package init function
func StateManagerInit(buyerOrSellerFlag string, clearCacheFlag bool) {
	NodeBuffersInstance = NewNodeBuffers()

	// Dump database to JSON for debugging
	//if err := NodeBuffersInstance.dumpToJSON(fmt.Sprintf("neuron-connections-startup-%s.json", buyerOrSellerFlag)); err != nil {
	//	log.Printf("Warning: Failed to dump database to JSON: %v", err)
	//}
}

type NodeBuffers struct {
	Buffers map[peer.ID]*NodeBufferInfo
	mu      sync.RWMutex
}

// NewNodeBuffers creates a new instance of NodeBuffers
func NewNodeBuffers() *NodeBuffers {
	return &NodeBuffers{
		Buffers: make(map[peer.ID]*NodeBufferInfo),
	}
}

// LibP2PState represents the state of a buffer
type LibP2PState string

const (
	ConnectionLost             LibP2PState = "LibP2PState:ConnectionLost"
	ConnectionLostFlushError   LibP2PState = "LibP2PState:ConnectionLostFlushError"
	ConnectionLostWriteError   LibP2PState = "LibP2PState:ConnectionLostWriteError"
	CanNotConnectUnknownReason LibP2PState = "LibP2PState:CanNotConnectUnknownReason"
	CanNotConnectStreamError   LibP2PState = "LibP2PState:CanNotConnectStreamError"

	Connected    LibP2PState = "LibP2PState:Connected"
	Connecting   LibP2PState = "LibP2PState:Connecting"
	Reconnecting LibP2PState = "LibP2PState:Reconnecting"
)

// StateString returns the LibP2PState value as a string.
func (s LibP2PState) StateString() string {
	return string(s)
}

type RendezvousState string

// / print the stateString
func (s RendezvousState) StateString() string {
	return string(s)
}

const (
	NotInitiated    RendezvousState = "RendezvousState:NotInitiated"
	SendOK          RendezvousState = "RendezvousState:SendOK"
	SendFail        RendezvousState = "RendezvousState:SendFail"
	ReceivedOK      RendezvousState = "RendezvousState:ReceivedOK"
	ReceivedFail    RendezvousState = "RendezvousState:ReceivedFail"
	WeDoNotKnowPeer RendezvousState = "RendezvousState:WeDoNotKnowPeer"
	HoldYourHorses  RendezvousState = "RendezvousState:HoldYourHorses"
)

// TopicPostalEnvelope holds prepared transactions along with the destination or source topic.
type TopicPostalEnvelope struct {
	Message         interface{}    `json:"message"`
	OtherStdInTopic hedera.TopicID `json:"other_std_in_topic"`
}

// NodeBufferInfo holds runtime info related to a remote peer.
type NodeBufferInfo struct {
	Writer                         network.Stream            `json:"-"`
	StreamHandler                  *network.Stream           `json:"-"`
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
}

func (nb *NodeBuffers) AddBuffer2(peerID peer.ID, envelope types.TopicPostalEnvelope, isOtherSideValidAccount bool, rendezvousState types.RendezvousState, libP2PState types.ConnectionState) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	nb.Buffers[peerID] = &NodeBufferInfo{
		RequestOrResponse:              envelope,
		IsOtherSideValidAccount:        isOtherSideValidAccount,
		RendezvousState:                rendezvousState,
		LibP2PState:                    libP2PState,
		LastConnectionAttempt:          time.Now(),
		NoOfConnectionAttempts:         1,
		NextScheduledConnectionAttempt: time.Now().Add(time.Second * time.Duration(1<<1)),
		NextScheduleRequestTime:        time.Now(),
	}
}

// AddBuffer3 adds a new bufio.Writer for a buyerID with a specified state and a bufio.Writer
func (bb *NodeBuffers) AddBuffer3(buyerID peer.ID, streamWriter network.Stream, rendezvousState types.RendezvousState, libP2PState types.ConnectionState) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.Buffers[buyerID] = &NodeBufferInfo{
		Writer:                         streamWriter,
		RendezvousState:                rendezvousState,
		LibP2PState:                    libP2PState,
		IsOtherSideValidAccount:        true,
		NoOfConnectionAttempts:         0,
		LastConnectionAttempt:          time.Now(),
		RequestOrResponse:              types.TopicPostalEnvelope{},
		NextScheduleRequestTime:        time.Time{},
		NextScheduledConnectionAttempt: time.Time{},
	}
}

// UpdateBufferIsValidAccount updates account validity
func (bb *NodeBuffers) UpdateBufferIsValidAccount(buyerID peer.ID, isValidAccount bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.IsOtherSideValidAccount = isValidAccount
	}
}

// GetBuffer returns the NodeBufferInfo associated with a buyerID, if it exists
func (nb *NodeBuffers) GetBuffer(peerID peer.ID) (*NodeBufferInfo, bool) {
	nb.mu.RLock()
	defer nb.mu.RUnlock()
	buffer, ok := nb.Buffers[peerID]
	return buffer, ok
}

// RemoveBuffer removes a buyerID and its associated NodeBufferInfo
func (nb *NodeBuffers) RemoveBuffer(peerID peer.ID) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	delete(nb.Buffers, peerID)
}

// UpdateBufferLibP2PState updates the LibP2PState of a buffer for a given buyerID
func (nb *NodeBuffers) UpdateBufferLibP2PState(peerID peer.ID, state types.ConnectionState) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if buffer, ok := nb.Buffers[peerID]; ok {
		buffer.LibP2PState = state
		if state == types.Connected {
			buffer.NoOfConnectionAttempts = 0
		}
		buffer.LastConnectionAttempt = time.Now()
	}
}

// UpdateBufferRendezvousState updates the RendezvousState of a buffer for a given buyerID
func (nb *NodeBuffers) UpdateBufferRendezvousState(peerID peer.ID, state types.RendezvousState) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if buffer, ok := nb.Buffers[peerID]; ok {
		buffer.RendezvousState = state
	}
}

// IncrementReconnectAttempts increments the reconnection attempt count for a buffer
func (nb *NodeBuffers) IncrementReconnectAttempts(peerID peer.ID) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if buffer, ok := nb.Buffers[peerID]; ok {
		buffer.NoOfConnectionAttempts++
		buffer.LastConnectionAttempt = time.Now()
		buffer.NextScheduledConnectionAttempt = time.Now().Add(time.Second * time.Duration(1<<buffer.NoOfConnectionAttempts))
	}
}

// SetNeuronSellerRequest sets the neuron seller request message
func (bb *NodeBuffers) SetNeuronSellerRequest(buyerID peer.ID, msg types.TopicPostalEnvelope) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.RequestOrResponse = msg
	}
}

// GetReconnectInfo returns the reconnection attempt count and last attempt time for a buffer
func (nb *NodeBuffers) GetReconnectInfo(peerID peer.ID) (int, time.Time, bool) {
	nb.mu.RLock()
	defer nb.mu.RUnlock()
	if buffer, ok := nb.Buffers[peerID]; ok {
		return buffer.NoOfConnectionAttempts, buffer.LastConnectionAttempt, true
	}
	return 0, time.Time{}, false
}

// GetBufferMap returns a copy of the buffer map
func (bb *NodeBuffers) GetBufferMap() map[peer.ID]*NodeBufferInfo {
	bb.mu.RLock()
	defer bb.mu.RUnlock()
	bufferMap := make(map[peer.ID]*NodeBufferInfo)
	for k, v := range bb.Buffers {
		bufferMap[k] = v
	}
	return bufferMap
}

// SetLastOtherSideMultiAddress sets the last known multi-address for a peer
func (nb *NodeBuffers) SetLastOtherSideMultiAddress(peerID peer.ID, addr string) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if buffer, ok := nb.Buffers[peerID]; ok {
		buffer.LastOtherSideMultiAddress = addr
	}
}

// dumpToJSON dumps the buffer state to a JSON file
func (nb *NodeBuffers) dumpToJSON(filename string) error {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	// Create a map to hold the serializable data
	data := make(map[string]interface{})
	for k, v := range nb.Buffers {
		// Create a copy of the buffer info without the non-serializable fields
		bufferInfo := map[string]interface{}{
			"last_other_side_multi_address":     v.LastOtherSideMultiAddress,
			"lib_p2p_state":                     v.LibP2PState,
			"rendezvous_state":                  v.RendezvousState,
			"is_other_side_valid_account":       v.IsOtherSideValidAccount,
			"no_of_connection_attempts":         v.NoOfConnectionAttempts,
			"last_connection_attempt":           v.LastConnectionAttempt,
			"next_scheduled_connection_attempt": v.NextScheduledConnectionAttempt,
			"next_schedule_request_time":        v.NextScheduleRequestTime,
			"last_goods_received_time":          v.LastGoodsReceivedTime,
		}
		data[k.String()] = bufferInfo
	}

	// Marshal the data to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling buffer state: %w", err)
	}

	// Write the JSON data to the file
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("error writing buffer state to file: %w", err)
	}

	return nil
}

// SetLastGoodsReceivedTime sets the last time goods were received from a peer
func (nb *NodeBuffers) SetLastGoodsReceivedTime(peerID peer.ID) {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if buffer, ok := nb.Buffers[peerID]; ok {
		buffer.LastGoodsReceivedTime = time.Now()
	}
}
