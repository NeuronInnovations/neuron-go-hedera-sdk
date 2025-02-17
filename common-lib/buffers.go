package commonlib

import (
	"bufio"
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
	"go.etcd.io/bbolt"
)

// Package-level variable for the BoltDB instance
var db *bbolt.DB

// Package-level variable for the NodeBuffers instance
var NodeBuffersInstance *NodeBuffers

// Initialize the database in the package init function
func StateManagerInit(buyerOrSellerFlag string, clearCacheFlag bool) {

	// Put the BoltDB file in the OS temp directory, using buyerOrSellerFlag in the filename.
	// This allows you to reuse the same DB on subsequent runs if the temp file still exists.
	dbPath := fmt.Sprintf("%s/neuron-connections-%s.db", os.TempDir(), buyerOrSellerFlag)

	// delete the db if the clearcache flag is set
	if clearCacheFlag {
		// Delete the database file if the reset flag is provided

		if _, err := os.Stat(dbPath); err == nil {
			err = os.Remove(dbPath)
			if err != nil {
				log.Fatalf("Failed to delete existing database: %v", err)
			}
			log.Println("Existing database deleted successfully.")
		} else if !os.IsNotExist(err) {
			log.Fatalf("Error checking database file: %v", err)
		}

	}

	var err error
	db, err = bbolt.Open(dbPath, 0600, nil)

	if err != nil {
		// If database initialization fails, log the error and proceed without persistence
		log.Fatalf("Failed to open BoltDB at %s (%v). Continuing without persistence.", dbPath, err)
		db = nil // Explicitly set db to nil to indicate no persistence
	} else {
		log.Printf("BoltDB opened successfully at: %s", dbPath)

		// Initialize the database schema or buckets if needed
		err = db.Update(func(tx *bbolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("NodeBuffers"))
			return err
		})
		if err != nil {
			log.Printf("Warning: failed to initialize BoltDB buckets (%v). Continuing without persistence.", err)
			db.Close()
			db = nil
		} else {
			// Initialize the NodeBuffers instance
			NodeBuffersInstance = NewNodeBuffers()
			// Load state from the database
			if err := NodeBuffersInstance.LoadState(); err != nil {
				log.Printf("Warning: Failed to load state: %v", err)
			}

			// Loop through all buffers and set their state to ConnectionLost
			NodeBuffersInstance.mu.Lock()
			for peerID, bufferInfo := range NodeBuffersInstance.Buffers {
				bufferInfo.LibP2PState = Connecting
				bufferInfo.RendezvousState = NotInitiated
				bufferInfo.LastConnectionAttempt = time.Time{}
				bufferInfo.NextScheduleRequestTime = time.Time{}
				bufferInfo.NoOfConnectionAttempts = 0
				bufferInfo.NextScheduleRequestTime = time.Time{}
				log.Printf("Setting LibP2PState to ConnectionLost for peer: %s", peerID)

				// Persist the updated state
				if persistErr := NodeBuffersInstance.persistNodeBufferInfo(peerID); persistErr != nil {
					log.Printf("Warning: Error persisting state for peerID %s: %v", peerID, persistErr)
				}
			}
			NodeBuffersInstance.mu.Unlock()

			// Dump database to JSON for debugging
			//if err := NodeBuffersInstance.dumpToJSON(fmt.Sprintf("neuron-connections-startup-%s.json", buyerOrSellerFlag)); err != nil {
			//	log.Printf("Warning: Failed to dump database to JSON: %v", err)
			//}

		}
	}
}

// CloseDB closes the BoltDB database.
func CloseDB(buyerOrSellerFlag string) error {
	if db != nil {
		// Dump database to JSON for debugging before closing
		//if err := NodeBuffersInstance.dumpToJSON(fmt.Sprintf("neuron-connections-shutdown-%s.json", buyerOrSellerFlag)); err != nil {
		//	log.Printf("Warning: Failed to dump database to JSON on shutdown: %v", err)
		//}
		return db.Close()
	}
	return nil
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
	Writer                         *bufio.Writer       `json:"-"`
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

// persistNodeBufferInfo persists a single NodeBufferInfo to BoltDB using JSON.
func (sb *NodeBuffers) persistNodeBufferInfo(peerID peer.ID) error {
	if db == nil {
		return nil
	}

	info, exists := sb.Buffers[peerID]
	if !exists {
		return fmt.Errorf("peerID %s not found in NodeBuffers", peerID)
	}

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal NodeBufferInfo: %w", err)
	}

	key := []byte(peerID.String())

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("NodeBuffers"))
		if b == nil {
			return fmt.Errorf("NodeBuffers bucket does not exist")
		}
		return b.Put(key, data)
	})
}

// deleteNodeBufferInfo removes a NodeBufferInfo from BoltDB.
func (bb *NodeBuffers) deleteNodeBufferInfo(peerID peer.ID) error {
	if db == nil {
		return nil
	}

	key := []byte(peerID.String())

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("NodeBuffers"))
		if b == nil {
			return nil // Bucket doesn't exist; nothing to delete
		}
		if err := b.Delete(key); err != nil {
			return fmt.Errorf("failed to delete key %s: %w", peerID, err)
		}
		return nil
	})
}

// LoadState loads the persisted NodeBuffers from BoltDB.
func (sb *NodeBuffers) LoadState() error {
	if db == nil {
		return nil
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	return db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("NodeBuffers"))
		if b == nil {
			return nil // No data to load
		}

		return b.ForEach(func(k, v []byte) error {
			var info NodeBufferInfo

			if err := json.Unmarshal(v, &info); err != nil {
				return fmt.Errorf("failed to unmarshal data for key %s: %w", k, err)
			}

			peerID, err := peer.Decode(string(k))
			if err != nil {
				return fmt.Errorf("failed to decode peer ID %s: %w", k, err)
			}

			sb.Buffers[peerID] = &info
			return nil
		})
	})
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

	// Persist the updated state
	if err := sb.persistNodeBufferInfo(sellerID); err != nil {
		log.Printf("Warning: Error persisting state for sellerID %s: %v", sellerID, err)
	}
}

// AddBuffer3 adds a new bufio.Writer for a buyerID with a specified state and a bufio.Writer
func (bb *NodeBuffers) AddBuffer3(buyerID peer.ID, streamWriter *bufio.Writer, rendezvousState RendezvousState, libP2PState LibP2PState) {
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

	// Persist the updated state
	if err := bb.persistNodeBufferInfo(buyerID); err != nil {
		log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
	}
}

// UpdateBufferIsValidAccount updates account validity
func (bb *NodeBuffers) UpdateBufferIsValidAccount(buyerID peer.ID, isValidAccount bool) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.IsOtherSideValidAccount = isValidAccount

		// Persist the updated state
		if err := bb.persistNodeBufferInfo(buyerID); err != nil {
			log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
		}
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

	// Remove from BoltDB
	if err := bb.deleteNodeBufferInfo(buyerID); err != nil {
		log.Printf("Warning: Error deleting state for buyerID %s: %v", buyerID, err)
	}
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

	// Persist the updated state
	if err := bb.persistNodeBufferInfo(buyerID); err != nil {
		log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
	}
	return true
}

// UpdateBufferRendezvousState updates the RendezvousState of a buffer for a given buyerID
func (bb *NodeBuffers) UpdateBufferRendezvousState(buyerID peer.ID, state RendezvousState) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.RendezvousState = state

		// Persist the updated state
		if err := bb.persistNodeBufferInfo(buyerID); err != nil {
			log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
		}
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

		// Persist the updated state
		if err := bb.persistNodeBufferInfo(buyerID); err != nil {
			log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
		}
	}
}

// SetNeuronSellerRequest sets the neuron seller request message
func (bb *NodeBuffers) SetNeuronSellerRequest(buyerID peer.ID, msg TopicPostalEnvelope) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	info, exists := bb.Buffers[buyerID]
	if exists {
		info.RequestOrResponse = msg

		// Persist the updated state
		if err := bb.persistNodeBufferInfo(buyerID); err != nil {
			log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
		}
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

// GetBufferList returns a copy of a list of all buffers to iterate over
func (bb *NodeBuffers) GetBufferList() []*bufio.Writer {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bufferList := make([]*bufio.Writer, 0, len(bb.Buffers))
	for _, info := range bb.Buffers {
		bufferList = append(bufferList, info.Writer)
	}
	return bufferList
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

		// Persist the updated state
		if err := bb.persistNodeBufferInfo(buyerID); err != nil {
			log.Printf("Warning: Error persisting state for buyerID %s: %v", buyerID, err)
		}
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
