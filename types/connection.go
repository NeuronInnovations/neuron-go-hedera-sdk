package types

import (
	"context"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ConnectionState represents the state of a connection
type ConnectionState string

const (
	ConnectionLost             ConnectionState = "ConnectionLost"
	ConnectionLostFlushError   ConnectionState = "ConnectionLostFlushError"
	ConnectionLostWriteError   ConnectionState = "ConnectionLostWriteError"
	CanNotConnectUnknownReason ConnectionState = "CanNotConnectUnknownReason"
	CanNotConnectStreamError   ConnectionState = "CanNotConnectStreamError"
	Connected                  ConnectionState = "Connected"
	Connecting                 ConnectionState = "Connecting"
	Reconnecting               ConnectionState = "Reconnecting"
)

// RendezvousState represents the state of a rendezvous
type RendezvousState string

const (
	NotInitiated    RendezvousState = "NotInitiated"
	SendOK          RendezvousState = "SendOK"
	SendFail        RendezvousState = "SendFail"
	ReceivedOK      RendezvousState = "ReceivedOK"
	ReceivedFail    RendezvousState = "ReceivedFail"
	WeDoNotKnowPeer RendezvousState = "WeDoNotKnowPeer"
	HoldYourHorses  RendezvousState = "HoldYourHorses"
)

// ConnectionManager defines the interface for managing peer connections
type ConnectionManager interface {
	InitialConnect(ctx context.Context, p2pHost host.Host, addrInfo peer.AddrInfo, protocol protocol.ID) error
	IsRequestTooEarly(peerID peer.ID) (bool, error)
}
