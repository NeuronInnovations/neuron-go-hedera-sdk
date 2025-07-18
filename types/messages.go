// messages.go defines the messages that are published to Hedera topics, enabling peers to communicate
// in a publicly visible manner. The messages are designed to facilitate decentralized coordination
// while ensuring transparency, as validators and other network participants can inspect the content
// to understand the intent and context of the messages.
//
// These public messages are distinct from the private communications that occur directly between peers
// via P2P streams. While P2P streams are used for direct, low-latency interactions (e.g., data transfer
// or rapid acknowledgments), Hedera topic messages serve as a more transparent and auditable mechanism
// for broadcasting high-level operations or coordinating state transitions.
//
// Currently, the messages defined in this file are specific to the BuyerVsSeller protocol, which governs
// interactions between buyers and sellers in the network. These include heartbeats, service requests,
// schedule sign requests, and various error messages that peers may send to each other or to themselves.
//
// **Future Considerations**:
// As the SDK evolves to support multiple protocols, each protocol will require its own tailored set of
// messages. These messages will need to be added and extended in a modular way to ensure that they align
// with the unique requirements of each protocol, while still adhering to the transparency and audibility
// principles that Hedera topic communications are designed to provide.
package types

import (
	"encoding/json"
	"errors"
	"fmt"
)

// EnvironmentVarLocation represents a location in the environment

type EnvironmentVarLocation struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	Altitude  float64 `json:"alt"`
	GPSFix    string  `json:"gpsfix"`
}

// NeuronMessage interface for all messages
type NeuronMessage interface {
	GetMessageType() string
	GetVersion() string
}

// MessageType represents different types of messages
type MessageType string

const (
	HeartbeatMessage MessageType = "NeuronHeartBeatMsg"
	// Add other message types as needed
)

type NeuronHeartBeatMsg struct {
	MessageType        string                 `json:"messageType"`
	Location           EnvironmentVarLocation `json:"location"`
	NatDeviceType      string                 `json:"natDeviceType"`
	NatReachability    bool                   `json:"natReachability"`
	Version            string                 `json:"version"`
	BuyerOrSeller      string                 `json:"buyerOrSeller"`
	ConnectedPeersAbrv []string               `json:"connectedPeers"`
}

func (m *NeuronHeartBeatMsg) GetMessageType() string {
	return m.MessageType
}

type NeuronServiceRequestMsg struct {
	MessageType string `json:"messageType"`

	EncryptedIpAddress []byte `json:"i"` // the buyers IP address if it is reachable
	StdInTopic         uint64 `json:"o"` // this is the buyer;s stdin topic for seller to send schedule requests into or error messages
	EthPublicKey       string `json:"e"` // the ethereum id of the buyer's hedera account
	PublicKey          string `json:"k"` // the hedera public key of the buyer (used in public key encryption and verification)

	ServiceType string `json:"t"`
	SlaAgreed   uint64 `json:"s"`
	SharedAccID uint64 `json:"a"` // the buyer creates this shared account and deposits money into it
	Version     string `json:"v"`
}

func (m *NeuronServiceRequestMsg) GetMessageType() string {
	return m.MessageType
}
func (m *NeuronServiceRequestMsg) GetVersion() string {
	return m.Version
}

type NeuronScheduleSignRequestMsg struct { // this is something the seller sends to the buyer; it's like an invoice.
	MessageType string `json:"messageType"`
	ScheduleID  uint64 `json:"c"`
	SharedAccID uint64 `json:"a"`
	Version     string `json:"v"`
}

func (m *NeuronScheduleSignRequestMsg) GetMessageType() string {
	return m.MessageType
}
func (m *NeuronScheduleSignRequestMsg) GetVersion() string {
	return m.Version
}

// Hole Punching Message Types

// NeuronPunchMeRequestMsg is sent by the seller to the buyer when direct connection fails
// and hole punching is required. It contains the seller's encrypted IP address and timing
// information for coordinated hole punching.
type NeuronPunchMeRequestMsg struct {
	MessageType        string `json:"messageType"`
	EncryptedIpAddress []byte `json:"i"` // Seller's IP encrypted with buyer's public key
	StdInTopic         uint64 `json:"o"` // Buyer's stdin topic
	PublicKey          string `json:"k"` // Seller's public key
	HederaTimestamp    string `json:"t"` // Timestamp from the HCS message for synchronization
	PunchDelay         int    `json:"d"` // Delay in seconds after consensus (default: 10)
	Version            string `json:"v"`
}

func (m *NeuronPunchMeRequestMsg) GetMessageType() string {
	return m.MessageType
}

func (m *NeuronPunchMeRequestMsg) GetVersion() string {
	return m.Version
}

// NeuronPunchMeAcknowledgmentMsg is sent by the buyer to acknowledge receipt of a
// punchMeRequest and confirm participation in the hole punching protocol.
type NeuronPunchMeAcknowledgmentMsg struct {
	MessageType     string `json:"messageType"`
	RequestTopic    uint64 `json:"r"` // Topic where punchMeRequest was received
	PublicKey       string `json:"k"` // Buyer's public key
	HederaTimestamp string `json:"t"` // Same timestamp from punchMeRequest for verification
	Version         string `json:"v"`
}

func (m *NeuronPunchMeAcknowledgmentMsg) GetMessageType() string {
	return m.MessageType
}

func (m *NeuronPunchMeAcknowledgmentMsg) GetVersion() string {
	return m.Version
}

// ErrorType represents different types of errors
type ErrorType string

const (
	TooEarlyDialError   ErrorType = "TooEarlyDialError"
	DialError           ErrorType = "DialError"
	FlushError          ErrorType = "FlushError"
	WriteError          ErrorType = "WriteError"
	StreamError         ErrorType = "StreamError"
	DisconnectedError   ErrorType = "DisconnectedError"
	NoKnownAddressError ErrorType = "NoKnownAddressError"
	VersionError        ErrorType = "VersionError"
	BalanceError        ErrorType = "BalanceError"
	IpDecryptionError   ErrorType = "IpDecryptionError"
	HeartBeatError      ErrorType = "HeartBeatError"
	ServiceError        ErrorType = "ServiceError"
	BadMessageError     ErrorType = "BadMessageError"
	ExplorerReachError  ErrorType = "ExplorerReachError"
)

// RecoverAction represents different recovery actions
type RecoverAction string

const (
	DoNothing RecoverAction = "DoNothing"
	// errors to report to others
	SendFreshHederaRequest RecoverAction = "SendFreshHederaRequest"
	CheckYourTopic         RecoverAction = "CheckYourTopic"
	PunchMe                RecoverAction = "PunchMe"
	TopUp                  RecoverAction = "TopUp"
	Upgrade                RecoverAction = "Upgrade"
	StopSending            RecoverAction = "StopSending"

	// self errors
	RebootMe   RecoverAction = "RebootMe"
	ShutMeDown RecoverAction = "ShutMeDown"
)

type NeuronPeerErrorMsg struct { // this is an error message from a peer to a peer
	MessageType string `json:"messageType"`

	EncryptedIpAddress []byte `json:"i"` // the sender's IP address if it is reachable
	StdInTopic         uint64 `json:"o"` // this is the sender's stdin topic for the other-side to  send requests into
	EthPublicKey       string `json:"e"` // the sender's ethereum id (pubkey)
	PublicKey          string `json:"k"` // the sender's Public Key that is used to sign txs

	ErrorType     ErrorType     `json:"errorType"`
	ErrorMessage  string        `json:"errorMessage"`
	RecoverAction RecoverAction `json:"recoverAction"`
	Version       string        `json:"v"`
}

type NeuronSelfErrorMsg struct { // this is an error for a peer to report errors in his own stdErr
	MessageType   string        `json:"messageType"`
	StdInTopic    uint64        `json:"o"` // this is my stdin topic in case someone wants to reply w.r.t self error.
	ErrorType     ErrorType     `json:"errorType"`
	ErrorMessage  string        `json:"errorMessage"`
	RecoverAction RecoverAction `json:"recoverAction"`
	Version       string        `json:"v"`
}

func (m *NeuronPeerErrorMsg) GetMessageType() string {
	return m.MessageType
}
func (m *NeuronPeerErrorMsg) GetVersion() string {
	return m.Version
}

func CheckMessageType(jsonData []byte) (string, bool) {
	var message map[string]interface{}
	err := json.Unmarshal(jsonData, &message)
	if err != nil {
		fmt.Printf("Error unmarshalling JSON while checking for messageType: %v\n", err)
		return "", false
	}
	if messageType, ok := message["messageType"].(string); ok {
		return messageType, true
	}
	return "", false
}

// UnmarshalNeuronMessage function unmarshals JSON data into the correct NeuronMessage type based on the "messageType" field
func UnmarshalNeuronMessage(jsonData []byte) (NeuronMessage, error) {
	// Get the message type from the JSON data
	messageType, ok := CheckMessageType(jsonData)
	if !ok {
		return nil, fmt.Errorf("unable to determine message type")
	}

	// Depending on the messageType, unmarshal into the appropriate struct
	switch messageType {
	case "NeuronServiceRequestMsg":
		var msg NeuronServiceRequestMsg
		err := json.Unmarshal(jsonData, &msg)
		if err != nil {
			return nil, fmt.Errorf("error un-marshalling NeuronServiceRequestMsg: %v", err)
		}
		return &msg, nil

	case "NeuronScheduleSignRequestMsg":
		var msg NeuronScheduleSignRequestMsg
		err := json.Unmarshal(jsonData, &msg)
		if err != nil {
			return nil, fmt.Errorf("error un-marshalling NeuronScheduleSignRequestMsg: %v", err)
		}
		return &msg, nil

	case "NeuronPeerErrorMsg":
		var msg NeuronPeerErrorMsg
		err := json.Unmarshal(jsonData, &msg)
		if err != nil {
			return nil, fmt.Errorf("error un-marshalling NeuronPeerErrorMsg: %v", err)
		}
		return &msg, nil

	case "punchMeRequest":
		var msg NeuronPunchMeRequestMsg
		err := json.Unmarshal(jsonData, &msg)
		if err != nil {
			return nil, fmt.Errorf("error un-marshalling NeuronPunchMeRequestMsg: %v", err)
		}
		return &msg, nil

	case "punchMeAcknowledgment":
		var msg NeuronPunchMeAcknowledgmentMsg
		err := json.Unmarshal(jsonData, &msg)
		if err != nil {
			return nil, fmt.Errorf("error un-marshalling NeuronPunchMeAcknowledgmentMsg: %v", err)
		}
		return &msg, nil

	default:
		return nil, fmt.Errorf("unknown message type: %s", messageType)
	}
}

func FullErrorTrace(err error) string {
	var trace string
	for i := 0; err != nil; i++ {
		trace += fmt.Sprintf("%d: %v\n", i, err)
		err = errors.Unwrap(err)
	}
	return trace
}
