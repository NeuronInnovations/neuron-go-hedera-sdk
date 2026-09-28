package commonlib

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// Package-level variable for the NodeBuffers instance
var NodeBuffersInstance *NodeBuffers

// StateManagerInit initializes the state manager including BBolt database for shared account caching
func StateManagerInit(buyerOrSellerFlag string, clearCacheFlag bool) {
	NodeBuffersInstance = NewNodeBuffers()

	// Initialize BBolt database for shared account caching (graceful degradation if fails)
	if err := OpenSharedAccountDB(); err != nil {
		log.Printf("Warning: Failed to open shared account cache: %v", err)
		log.Printf("Continuing without persistence - new shared accounts will be created each time")
		// Continue without persistence - graceful degradation per design
	}

	// Clear cache if flag set
	if clearCacheFlag && IsSharedAccountDBOpen() {
		if err := ClearSharedAccountCache(); err != nil {
			log.Printf("Warning: Failed to clear shared account cache: %v", err)
		}
	}
}

// NodeBuffers holds per-peer connection state. All access is protected by mu.
// When a connection drops, both the app (e.g. stream read error → RecordDisconnectEvent)
// and the SDK (processSeller from recovery/60s loop) can touch the same buffer.
// Contention: many concurrent disconnects cause lock contention on mu. Avoid
// doing heavy work (Hedera/gRPC) while holding mu; processSeller yields the
// first reconnect window after LastDisconnectAt so the app's adaptive retry
// can own the first resend and we don't double-submit.
type NodeBuffers struct {
	mu                sync.Mutex
	Buffers           map[peer.ID]*NodeBufferInfo
	peerIDByEvm       map[string]peer.ID // lookup: do not use peer IDs as external keys
	peerIDByPublicKey map[string]peer.ID
}

// NewNodeBuffers creates a new instance of NodeBuffers
func NewNodeBuffers() *NodeBuffers {
	return &NodeBuffers{
		Buffers:           make(map[peer.ID]*NodeBufferInfo),
		peerIDByEvm:       make(map[string]peer.ID),
		peerIDByPublicKey: make(map[string]peer.ID),
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
	Writer                         network.Stream      `json:"-"` // TODO: only one needed
	StreamHandler                  *network.Stream     `json:"-"`
	LastOtherSideMultiAddress      string              `json:"last_other_side_multi_address"`
	LibP2PState                    LibP2PState         `json:"lib_p2p_state"`
	RendezvousState                RendezvousState     `json:"rendezvous_state"`
	IsOtherSideValidAccount        bool                `json:"is_other_side_valid_account"`
	NoOfConnectionAttempts         int                 `json:"no_of_connection_attempts"`
	RequestAttemptsSinceSuccess    int                 `json:"request_attempts_since_success"`
	SuccessfulConnections          int                 `json:"successful_connections"`
	DisconnectCount                int                 `json:"disconnect_count"`
	DisconnectScore                int                 `json:"disconnect_score"`
	LastConnectionAttempt          time.Time           `json:"last_connection_attempt"`
	FirstConnectionAttempt         time.Time           `json:"first_connection_attempt"` // when we first started trying (for give-up horizon)
	LastSuccessAt                  time.Time           `json:"last_success_at"`
	LastDisconnectAt               time.Time           `json:"last_disconnect_at"`
	NextScheduledConnectionAttempt time.Time           `json:"next_scheduled_connection_attempt"`
	RequestOrResponse              TopicPostalEnvelope `json:"request_or_response"`
	NextScheduleRequestTime        time.Time           `json:"next_schedule_request_time"`
	LastGoodsReceivedTime          time.Time           `json:"last_goods_received_time"`
	PeerPublicKey                  string              `json:"peer_public_key"` // The peer's public key from Hedera (for log correlation)
	EvmAddress                     string              `json:"evm_address"`     // The peer's EVM address when known (for lookup; do not use peer ID as key)
}

// ShortPublicKey returns the last 8 characters of the public key for logging
func (n *NodeBufferInfo) ShortPublicKey() string {
	if len(n.PeerPublicKey) >= 8 {
		return n.PeerPublicKey[len(n.PeerPublicKey)-8:]
	}
	return n.PeerPublicKey
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
	now := time.Now()
	prev := sb.Buffers[sellerID]
	successfulConnections := 0
	disconnectCount := 0
	disconnectScore := 0
	lastSuccessAt := time.Time{}
	lastDisconnectAt := time.Time{}
	lastGoodsReceivedAt := time.Time{}
	lastOtherSideMultiAddress := ""
	peerPublicKey := ""
	evmAddress := ""
	if prev != nil {
		successfulConnections = prev.SuccessfulConnections
		disconnectCount = prev.DisconnectCount
		disconnectScore = effectiveDisconnectScore(now, prev.DisconnectScore, prev.LastSuccessAt, prev.LastDisconnectAt)
		lastSuccessAt = prev.LastSuccessAt
		lastDisconnectAt = prev.LastDisconnectAt
		lastGoodsReceivedAt = prev.LastGoodsReceivedTime
		lastOtherSideMultiAddress = prev.LastOtherSideMultiAddress
		peerPublicKey = prev.PeerPublicKey
		evmAddress = prev.EvmAddress
	}
	sb.Buffers[sellerID] = &NodeBufferInfo{
		StreamHandler:                  nil,
		LastOtherSideMultiAddress:      lastOtherSideMultiAddress,
		RendezvousState:                rendezvousState,
		LibP2PState:                    libP2PState,
		IsOtherSideValidAccount:        isValidAccount,
		NoOfConnectionAttempts:         1,
		RequestAttemptsSinceSuccess:    1,
		SuccessfulConnections:          successfulConnections,
		DisconnectCount:                disconnectCount,
		DisconnectScore:                disconnectScore,
		LastConnectionAttempt:          now,
		FirstConnectionAttempt:         now,
		LastSuccessAt:                  lastSuccessAt,
		LastDisconnectAt:               lastDisconnectAt,
		NextScheduledConnectionAttempt: now.Add(computeRetryDelay(1, successfulConnections > 0, disconnectScore)),
		RequestOrResponse:              request,
		LastGoodsReceivedTime:          lastGoodsReceivedAt,
		PeerPublicKey:                  peerPublicKey,
		EvmAddress:                     evmAddress,
	}
}

// AddBuffer3 adds a new bufio.Writer for a buyerID with a specified state and a bufio.Writer
func (bb *NodeBuffers) AddBuffer3(buyerID peer.ID, streamWriter network.Stream, rendezvousState RendezvousState, libP2PState LibP2PState) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	now := time.Now()
	prev := bb.Buffers[buyerID]
	successfulConnections := 1
	disconnectCount := 0
	disconnectScore := 0
	lastDisconnectAt := time.Time{}
	lastGoodsReceivedAt := time.Time{}
	requestOrResponse := TopicPostalEnvelope{}
	lastOtherSideMultiAddress := ""
	peerPublicKey := ""
	evmAddress := ""
	if prev != nil {
		successfulConnections = prev.SuccessfulConnections + 1
		disconnectCount = prev.DisconnectCount
		disconnectScore = effectiveDisconnectScore(now, prev.DisconnectScore, prev.LastSuccessAt, prev.LastDisconnectAt)
		if disconnectScore > 0 {
			disconnectScore--
		}
		lastDisconnectAt = prev.LastDisconnectAt
		lastGoodsReceivedAt = prev.LastGoodsReceivedTime
		requestOrResponse = prev.RequestOrResponse
		lastOtherSideMultiAddress = prev.LastOtherSideMultiAddress
		peerPublicKey = prev.PeerPublicKey
		evmAddress = prev.EvmAddress
	}
	bb.Buffers[buyerID] = &NodeBufferInfo{
		Writer:                         streamWriter,
		LastOtherSideMultiAddress:      lastOtherSideMultiAddress,
		RendezvousState:                rendezvousState,
		LibP2PState:                    libP2PState,
		IsOtherSideValidAccount:        true,
		NoOfConnectionAttempts:         0,
		RequestAttemptsSinceSuccess:    0,
		SuccessfulConnections:          successfulConnections,
		DisconnectCount:                disconnectCount,
		DisconnectScore:                disconnectScore,
		LastConnectionAttempt:          now,
		FirstConnectionAttempt:         time.Time{},
		LastSuccessAt:                  now,
		LastDisconnectAt:               lastDisconnectAt,
		RequestOrResponse:              requestOrResponse,
		NextScheduleRequestTime:        time.Time{},
		NextScheduledConnectionAttempt: time.Time{},
		LastGoodsReceivedTime:          lastGoodsReceivedAt,
		PeerPublicKey:                  peerPublicKey,
		EvmAddress:                     evmAddress,
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

// RemoveBuffer removes a buyerID and its associated NodeBufferInfo, and cleans lookup maps.
func (bb *NodeBuffers) RemoveBuffer(buyerID peer.ID) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	if info, exists := bb.Buffers[buyerID]; exists {
		if info.EvmAddress != "" {
			delete(bb.peerIDByEvm, normalizeEvmLookup(info.EvmAddress))
		}
		if info.PeerPublicKey != "" {
			delete(bb.peerIDByPublicKey, normalizeLookupKey(info.PeerPublicKey))
		}
	}
	delete(bb.Buffers, buyerID)
	// Stop per-peer writer to avoid goroutine leaks on disconnect.
	stopPeerWriteQueue(buyerID)
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
		info.RequestAttemptsSinceSuccess = 0
		info.LastSuccessAt = time.Now()
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
		now := time.Now()
		if info.FirstConnectionAttempt.IsZero() {
			info.FirstConnectionAttempt = now
		}
		info.NoOfConnectionAttempts++
		info.RequestAttemptsSinceSuccess++
		info.LastConnectionAttempt = now
		info.DisconnectScore = effectiveDisconnectScore(now, info.DisconnectScore, info.LastSuccessAt, info.LastDisconnectAt)
		info.NextScheduledConnectionAttempt = now.Add(computeRetryDelay(info.RequestAttemptsSinceSuccess, info.SuccessfulConnections > 0, info.DisconnectScore))
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

// ResetReconnectSchedule clears the "first attempt" time so the next outage is treated as a new run (day 1, 10 min interval).
// Call when we achieve connection so that if we go down again later we restart the day-based schedule.
func (bb *NodeBuffers) ResetReconnectSchedule(peerID peer.ID) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[peerID]
	if exists {
		info.NoOfConnectionAttempts = 0
		info.RequestAttemptsSinceSuccess = 0
		info.FirstConnectionAttempt = time.Time{}
		info.NextScheduledConnectionAttempt = time.Time{}
	}
}

// ResetReconnectScheduleByEvm clears the reconnect schedule for the peer with the given EVM address so the next retry cycle starts from day 1.
// Use after "give up" when the user manually triggers a new connection attempt (e.g. Restart).
func (bb *NodeBuffers) ResetReconnectScheduleByEvm(evmAddress string) bool {
	pid, ok := bb.GetPeerIDByEvm(evmAddress)
	if !ok {
		return false
	}
	bb.ResetReconnectSchedule(pid)
	return true
}

// GetReconnectInfo returns the reconnection attempt count, last attempt time, and first attempt time for a buffer
func (bb *NodeBuffers) GetReconnectInfo(buyerID peer.ID) (int, time.Time, time.Time, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if !exists {
		return 0, time.Time{}, time.Time{}, false
	}
	return info.RequestAttemptsSinceSuccess, info.LastConnectionAttempt, info.FirstConnectionAttempt, true
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

func (bb *NodeBuffers) RecordDisconnectEvent(buyerID peer.ID) bool {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if !exists {
		return false
	}
	now := time.Now()
	info.DisconnectScore = effectiveDisconnectScore(now, info.DisconnectScore, info.LastSuccessAt, info.LastDisconnectAt)
	info.DisconnectCount++
	if info.DisconnectScore < 8 {
		info.DisconnectScore++
	}
	info.LastDisconnectAt = now
	return true
}

func (bb *NodeBuffers) RetryPolicySnapshot(buyerID peer.ID) (int, int, bool, time.Time, time.Time, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if !exists {
		return 0, 0, false, time.Time{}, time.Time{}, false
	}
	effectiveScore := effectiveDisconnectScore(time.Now(), info.DisconnectScore, info.LastSuccessAt, info.LastDisconnectAt)
	return info.RequestAttemptsSinceSuccess, effectiveScore, info.SuccessfulConnections > 0, info.LastConnectionAttempt, info.FirstConnectionAttempt, true
}

func effectiveDisconnectScore(now time.Time, rawScore int, lastSuccessAt, lastDisconnectAt time.Time) int {
	score := rawScore
	if score < 0 {
		score = 0
	}
	if !lastDisconnectAt.IsZero() {
		sinceDisconnect := now.Sub(lastDisconnectAt)
		switch {
		case sinceDisconnect >= 6*time.Hour:
			score = 0
		case sinceDisconnect >= 2*time.Hour:
			score -= 2
		case sinceDisconnect >= 1*time.Hour:
			score--
		}
	}
	if !lastSuccessAt.IsZero() {
		stableFor := now.Sub(lastSuccessAt)
		switch {
		case stableFor >= 2*time.Hour:
			score = 0
		case stableFor >= time.Hour:
			score -= 4
		case stableFor >= 30*time.Minute:
			score -= 2
		case stableFor >= 10*time.Minute:
			score--
		}
	}
	if score < 0 {
		return 0
	}
	return score
}

// computeRetryDelay balances recovery with gRPC/Hedera load and seller protection.
// Intervals are conservative to avoid reconnection storms: when many streams drop,
// aggressive retries would choke the system and cause more drops. disconnectScore
// decays after sustained healthy time so recovered sellers get faster retries again.
func computeRetryDelay(attemptsSinceSuccess int, hasEverConnected bool, disconnectScore int) time.Duration {
	if attemptsSinceSuccess < 1 {
		attemptsSinceSuccess = 1
	}

	var schedule []time.Duration
	switch {
	case !hasEverConnected && disconnectScore <= 3:
		schedule = []time.Duration{
			90 * time.Second,
			3 * time.Minute,
			8 * time.Minute,
			15 * time.Minute,
			30 * time.Minute,
			time.Hour,
			3 * time.Hour,
			6 * time.Hour,
		}
	case !hasEverConnected:
		schedule = []time.Duration{
			5 * time.Minute,
			10 * time.Minute,
			20 * time.Minute,
			time.Hour,
			3 * time.Hour,
			6 * time.Hour,
		}
	case disconnectScore <= 1:
		schedule = []time.Duration{
			45 * time.Second,
			2 * time.Minute,
			5 * time.Minute,
			12 * time.Minute,
			30 * time.Minute,
			time.Hour,
			3 * time.Hour,
			6 * time.Hour,
		}
	case disconnectScore <= 3:
		schedule = []time.Duration{
			2 * time.Minute,
			5 * time.Minute,
			12 * time.Minute,
			30 * time.Minute,
			time.Hour,
			3 * time.Hour,
			6 * time.Hour,
		}
	default:
		schedule = []time.Duration{
			10 * time.Minute,
			30 * time.Minute,
			time.Hour,
			3 * time.Hour,
			6 * time.Hour,
		}
	}

	idx := attemptsSinceSuccess - 1
	if idx >= len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[idx]
}

// SetPeerPublicKey stores the peer's public key and registers it for lookup (do not use peer ID as external key).
func (bb *NodeBuffers) SetPeerPublicKey(peerID peer.ID, publicKey string) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[peerID]
	if exists {
		info.PeerPublicKey = publicKey
		if publicKey != "" {
			bb.peerIDByPublicKey[normalizeLookupKey(publicKey)] = peerID
		}
	}
}

// SetPeerEvmAddress sets the peer's EVM address and registers it for lookup (do not use peer ID as external key).
func (bb *NodeBuffers) SetPeerEvmAddress(peerID peer.ID, evmAddress string) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[peerID]
	if exists {
		info.EvmAddress = evmAddress
		if evmAddress != "" {
			bb.peerIDByEvm[normalizeEvmLookup(evmAddress)] = peerID
		}
	}
}

// GetPeerIDByEvm returns the peer.ID for the given EVM address, if registered. Use this instead of using peer IDs as keys.
func (bb *NodeBuffers) GetPeerIDByEvm(evmAddress string) (peer.ID, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	pid, ok := bb.peerIDByEvm[normalizeEvmLookup(evmAddress)]
	return pid, ok
}

// GetPeerIDByPublicKey returns the peer.ID for the given public key, if registered.
func (bb *NodeBuffers) GetPeerIDByPublicKey(publicKey string) (peer.ID, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	pid, ok := bb.peerIDByPublicKey[normalizeLookupKey(publicKey)]
	return pid, ok
}

// GetEvmByPeerID returns the EVM address for the given peer ID, if set.
func (bb *NodeBuffers) GetEvmByPeerID(pid peer.ID) (string, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[pid]
	if !exists || info.EvmAddress == "" {
		return "", false
	}
	return info.EvmAddress, true
}

// GetPublicKeyByPeerID returns the public key for the given peer ID, if set.
func (bb *NodeBuffers) GetPublicKeyByPeerID(pid peer.ID) (string, bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[pid]
	if !exists || info.PeerPublicKey == "" {
		return "", false
	}
	return info.PeerPublicKey, true
}

func normalizeEvmLookup(evm string) string {
	s := strings.TrimSpace(strings.ToLower(evm))
	if strings.HasPrefix(s, "0x") {
		return s
	}
	return "0x" + s
}

func normalizeLookupKey(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

// GetPeerPublicKey returns the peer's public key (short version for logging)
func (bb *NodeBuffers) GetPeerPublicKeyShort(peerID peer.ID) string {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[peerID]
	if !exists || len(info.PeerPublicKey) < 8 {
		return ""
	}
	return info.PeerPublicKey[len(info.PeerPublicKey)-8:]
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
