package types

import (
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TopicPostalEnvelope holds prepared transactions along with the destination or source topic.
type TopicPostalEnvelope struct {
	Message         interface{}    `json:"message"`
	OtherStdInTopic hedera.TopicID `json:"other_std_in_topic"`
}

// NodeBufferInfo holds runtime info related to a remote peer.
type NodeBufferInfo struct {
	Writer                         network.Stream      `json:"-"`
	StreamHandler                  *network.Stream     `json:"-"`
	LastOtherSideMultiAddress      string              `json:"last_other_side_multi_address"`
	LibP2PState                    ConnectionState     `json:"lib_p2p_state"`
	RendezvousState                RendezvousState     `json:"rendezvous_state"`
	IsOtherSideValidAccount        bool                `json:"is_other_side_valid_account"`
	NoOfConnectionAttempts         int                 `json:"no_of_connection_attempts"`
	LastConnectionAttempt          time.Time           `json:"last_connection_attempt"`
	NextScheduledConnectionAttempt time.Time           `json:"next_scheduled_connection_attempt"`
	RequestOrResponse              TopicPostalEnvelope `json:"request_or_response"`
	NextScheduleRequestTime        time.Time           `json:"next_schedule_request_time"`
	LastGoodsReceivedTime          time.Time           `json:"last_goods_received_time"`
}

// NodeBuffers manages buffers for peer connections
type NodeBuffers struct {
	Buffers map[peer.ID]*NodeBufferInfo
}

// PeerStatusInfo contains detailed information about a peer's connection status
type PeerStatusInfo struct {
	PublicKey                      string    `json:"publicKey"`
	PeerID                         string    `json:"peerID"`
	LibP2PState                    string    `json:"libP2PState"`
	RendezvousState                string    `json:"rendezvousState"`
	IsOtherSideValidAccount        bool      `json:"isOtherSideValidAccount"`
	NoOfConnectionAttempts         int       `json:"noOfConnectionAttempts"`
	LastConnectionAttempt          time.Time `json:"lastConnectionAttempt"`
	NextScheduledConnectionAttempt time.Time `json:"nextScheduledConnectionAttempt"`
	LastGoodsReceivedTime          time.Time `json:"lastGoodsReceivedTime"`
	LastOtherSideMultiAddress      string    `json:"lastOtherSideMultiAddress"`
	ConnectionStatus               string    `json:"connectionStatus"` // "Connected", "Connecting", "Disconnected", "Error"
}
