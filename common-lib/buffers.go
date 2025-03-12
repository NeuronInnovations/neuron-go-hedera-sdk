package commonlib

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
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
	mu      sync.Mutex
	Buffers map[peer.ID]*NodeBufferInfo
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
	Writer                         network.Stream      `json:"-"` //  TODO: only one needed
	StreamHandler                  *network.Stream     `json:"-"`
	LastOtherSideMultiAddress      string              `json:"last_other_side_multi_address"`
	LibP2PState                    LibP2PState         `json:"lib_p2p_state"`
	RendezvousState                RendezvousState     `json:"rendezvous_state"`
	IsOtherSideValidAccount        bool                `json:"is_other_side_valid_account"`
	NoOfConnectionAttempts         int                 `json:"no_of_connection_attempts"`
	LastConnectionAttempt          time.Time           `json:"last_connection_attempt"`
	NextScheduledConnectionAttempt time.Time           `json:"next_scheduled_connection_attempt"`
	RequestOrResponse              TopicPostalEnvelope `json:"request_or_response"`
	NextScheduleRequestTime        time.Time           `json:"next_schedule_request_time"`
	LastGoodsReceivedTime          time.Time           `json:"last_goods_received_time"`
}

func (sb *NodeBuffers) SetStreamHandler(sellerID peer.ID, streamHandler *network.Stream) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	info, exists := sb.Buffers[sellerID]
	if exists {
		info.StreamHandler = streamHandler
		// No need to persist StreamHandler as it's not serializable
	} else {
		log.Panic(sellerID, "does not exist")
	}
}

// set the last other side multiaddress
func (sb *NodeBuffers) SetLastOtherSideMultiAddress(sellerID peer.ID, lastOtherSideMultiAddress multiaddr.Multiaddr) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	info, exists := sb.Buffers[sellerID]
	if exists {
		info.LastOtherSideMultiAddress = lastOtherSideMultiAddress.String()
	}
}

func (sb *NodeBuffers) AddBuffer2(sellerID peer.ID, request TopicPostalEnvelope, isValidAccount bool, rendezvousState RendezvousState, libP2PState LibP2PState) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.Buffers[sellerID] = &NodeBufferInfo{
		StreamHandler:           nil,
		RendezvousState:         rendezvousState,
		LibP2PState:             libP2PState,
		IsOtherSideValidAccount: isValidAccount,
		NoOfConnectionAttempts:  1,
		LastConnectionAttempt:   time.Now(),
		RequestOrResponse:       request,
	}

}

// AddBuffer3 adds a new bufio.Writer for a buyerID with a specified state and a bufio.Writer
func (bb *NodeBuffers) AddBuffer3(buyerID peer.ID, streamWriter network.Stream, rendezvousState RendezvousState, libP2PState LibP2PState) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.Buffers[buyerID] = &NodeBufferInfo{
		Writer:                         streamWriter,
		RendezvousState:                rendezvousState,
		LibP2PState:                    libP2PState,
		IsOtherSideValidAccount:        true,
		NoOfConnectionAttempts:         0,
		LastConnectionAttempt:          time.Now(),
		RequestOrResponse:              TopicPostalEnvelope{},
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
func (bb *NodeBuffers) GetBuffer(buyerID peer.ID) (NodeBufferInfo, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if !exists {
		return NodeBufferInfo{}, false
	}
	return *info, true
}

// RemoveBuffer removes a buyerID and its associated NodeBufferInfo
func (bb *NodeBuffers) RemoveBuffer(buyerID peer.ID) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	delete(bb.Buffers, buyerID)

}

// UpdateBufferLibP2PState updates the LibP2PState of a buffer for a given buyerID
func (bb *NodeBuffers) UpdateBufferLibP2PState(buyerID peer.ID, state LibP2PState) bool {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if !exists {
		return false
	}
	info.LibP2PState = state
	if state == Connected {
		info.NoOfConnectionAttempts = 0
	}
	info.LastConnectionAttempt = time.Now()

	return true
}

// UpdateBufferRendezvousState updates the RendezvousState of a buffer for a given buyerID
func (bb *NodeBuffers) UpdateBufferRendezvousState(buyerID peer.ID, state RendezvousState) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.RendezvousState = state

	}
}

// IncrementReconnectAttempts increments the reconnection attempt count for a buffer
func (bb *NodeBuffers) IncrementReconnectAttempts(buyerID peer.ID) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.NoOfConnectionAttempts += 1
		info.LastConnectionAttempt = time.Now()
		info.NextScheduledConnectionAttempt = time.Now().Add(time.Second * time.Duration(1<<info.NoOfConnectionAttempts))

	}
}

// SetNeuronSellerRequest sets the neuron seller request message
func (bb *NodeBuffers) SetNeuronSellerRequest(buyerID peer.ID, msg TopicPostalEnvelope) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.RequestOrResponse = msg

	}
}

// GetReconnectInfo returns the reconnection attempt count and last attempt time for a buffer
func (bb *NodeBuffers) GetReconnectInfo(buyerID peer.ID) (int, time.Time, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if !exists {
		return 0, time.Time{}, false
	}
	return info.NoOfConnectionAttempts, info.LastConnectionAttempt, true
}

// GetBufferMap returns a copy of the internal map of buffers and their states
func (bb *NodeBuffers) GetBufferMap() map[peer.ID]*NodeBufferInfo {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bufferMapCopy := make(map[peer.ID]*NodeBufferInfo, len(bb.Buffers))
	for k, v := range bb.Buffers {
		bufferMapCopy[k] = v
	}
	return bufferMapCopy
}

func (bb *NodeBuffers) SetLastGoodsReceivedTime(buyerID peer.ID) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.LastGoodsReceivedTime = time.Now()

	}
}

// dumpToJSON dumps the contents of the NodeBuffers to a JSON file for debugging.
func (sb *NodeBuffers) dumpToJSON(filename string) error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	// Create a map to hold the serializable version of NodeBuffers
	serializableMap := make(map[string]*NodeBufferInfo)

	for peerID, info := range sb.Buffers {
		serializableMap[peerID.String()] = info
	}

	// Marshal the map to JSON
	data, err := json.MarshalIndent(serializableMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal NodeBuffers to JSON: %w", err)
	}

	// Write to file
	return os.WriteFile(filename, data, 0644)
}
