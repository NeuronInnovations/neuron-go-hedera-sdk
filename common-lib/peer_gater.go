package commonlib

import (
	"log"

	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/crypto/pb"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// Secp256k1OnlyGater drops any connection whose remote identity is not a
// secp256k1 key. Every Neuron buyer and seller derives its libp2p identity from
// its Hedera ECDSA (secp256k1) key, so their peer IDs start "16Uiu2…". Anything
// else — typically an internet scanner or stray IPFS node using libp2p's default
// Ed25519 identity ("12D3KooW…") — is not one of ours.
//
// Before this gater, such a peer got as far as identify, and the peer-identified
// logger fed its Ed25519 key to keylib's secp256k1 decompression, which
// log.Fatal'd the whole process ("invalid compressed public key length").
//
// Rejection happens in InterceptSecured, i.e. straight after the transport
// security handshake — the earliest point the remote key is known. The
// connection is closed before any stream, identify or protocol negotiation, so
// the remote learns nothing beyond the handshake itself. We only log it locally.
type Secp256k1OnlyGater struct{}

func (Secp256k1OnlyGater) InterceptPeerDial(peer.ID) bool               { return true }
func (Secp256k1OnlyGater) InterceptAddrDial(peer.ID, ma.Multiaddr) bool { return true }
func (Secp256k1OnlyGater) InterceptAccept(network.ConnMultiaddrs) bool  { return true }
func (Secp256k1OnlyGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func (Secp256k1OnlyGater) InterceptSecured(dir network.Direction, p peer.ID, addrs network.ConnMultiaddrs) bool {
	pk, err := p.ExtractPublicKey()
	if err == nil && pk.Type() == pb.KeyType_Secp256k1 {
		return true
	}
	keyType := "unknown"
	if err == nil {
		keyType = pk.Type().String()
	}
	log.Printf("[P2P GATE] dropped non-secp256k1 peer=%s key=%s dir=%s remote=%s",
		p, keyType, dir, addrs.RemoteMultiaddr())
	return false
}
