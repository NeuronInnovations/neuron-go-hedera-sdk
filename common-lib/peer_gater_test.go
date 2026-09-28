package commonlib

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newTestHost(t *testing.T, keyType int, listen string, gated bool) host.Host {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPairWithReader(keyType, -1, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	opts := []libp2p.Option{libp2p.Identity(priv), libp2p.ListenAddrStrings(listen)}
	if gated {
		opts = append(opts, libp2p.ConnectionGater(Secp256k1OnlyGater{}))
	}
	h, err := libp2p.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

// A stranger with an Ed25519 identity used to crash the buyer via keylib's
// log.Fatal on identify. With the gater it must be dropped, over both
// transports, and never show up as a connected peer or in the peerstore.
func TestSecp256k1OnlyGater(t *testing.T) {
	for _, tr := range []struct{ name, listen string }{
		{"tcp", "/ip4/127.0.0.1/tcp/0"},
		{"quic", "/ip4/127.0.0.1/udp/0/quic-v1"},
	} {
		t.Run(tr.name, func(t *testing.T) {
			buyer := newTestHost(t, crypto.Secp256k1, tr.listen, true)
			target := peer.AddrInfo{ID: buyer.ID(), Addrs: buyer.Addrs()}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			stranger := newTestHost(t, crypto.Ed25519, tr.listen, false)
			if err := stranger.Connect(ctx, target); err == nil {
				// The handshake can complete on the dialer's side before the
				// listener's gater closes the conn; it must not survive.
				time.Sleep(500 * time.Millisecond)
			}
			if n := len(buyer.Network().ConnsToPeer(stranger.ID())); n != 0 {
				t.Fatalf("ed25519 peer %s has %d conns to the gated host", stranger.ID(), n)
			}
			// Identify never ran in either direction: the stranger learned none
			// of our protocols, and we never recorded any of theirs.
			if ps, _ := stranger.Peerstore().GetProtocols(buyer.ID()); len(ps) != 0 {
				t.Fatalf("stranger learned our protocols: %v", ps)
			}
			if ps, _ := buyer.Peerstore().GetProtocols(stranger.ID()); len(ps) != 0 {
				t.Fatalf("gated host identified the stranger: %v", ps)
			}

			seller := newTestHost(t, crypto.Secp256k1, tr.listen, false)
			if err := seller.Connect(ctx, target); err != nil {
				t.Fatalf("secp256k1 peer rejected: %v", err)
			}
			if buyer.Network().Connectedness(seller.ID()) != network.Connected {
				t.Fatalf("secp256k1 peer not connected")
			}
		})
	}
}
