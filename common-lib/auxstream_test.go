package commonlib

import (
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
)

const testAuxProto = protocol.ID("neuron/aux-test/0.0.1")

func TestAuxFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(AppendAuxFrame(nil, AuxKeyframe, 7, []byte(`{"a":1}`)))
	buf.Write(AppendAuxFrame(nil, AuxDelta, 8, nil))
	f, err := ReadAuxFrame(&buf)
	if err != nil || f.Kind != AuxKeyframe || f.Seq != 7 || string(f.Body) != `{"a":1}` {
		t.Fatalf("first frame = %+v, %v", f, err)
	}
	f, err = ReadAuxFrame(&buf)
	if err != nil || f.Kind != AuxDelta || f.Seq != 8 || len(f.Body) != 0 {
		t.Fatalf("second frame = %+v, %v", f, err)
	}
	if _, err = ReadAuxFrame(&buf); err != io.EOF {
		t.Fatalf("after last frame err = %v, want EOF", err)
	}

	// A truncated body is an error, not a short frame.
	whole := AppendAuxFrame(nil, AuxDelta, 9, []byte("abcdef"))
	if _, err = ReadAuxFrame(bytes.NewReader(whole[:len(whole)-2])); err != io.ErrUnexpectedEOF {
		t.Fatalf("truncated frame err = %v, want ErrUnexpectedEOF", err)
	}
	// An absurd length is refused before allocating.
	bad := []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	if _, err = ReadAuxFrame(bytes.NewReader(bad)); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

// testFeed is a producer whose state is just its seq; keyframes say "k<seq>".
type testFeed struct {
	mu  sync.Mutex
	seq uint64
}

func (f *testFeed) keyframe() (uint64, []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seq, AppendAuxFrame(nil, AuxKeyframe, f.seq, fmt.Appendf(nil, "k%d", f.seq))
}

// advance moves the state to seq and returns the delta frame for it.
func (f *testFeed) advance(seq uint64) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq = seq
	return AppendAuxFrame(nil, AuxDelta, seq, fmt.Appendf(nil, "d%d", seq))
}

// recvFrames collects the frames a buyer host reads, and the stream events.
type recvFrames struct {
	frames chan AuxFrame
	ended  chan error
}

func handleAux(h host.Host) *recvFrames {
	r := &recvFrames{frames: make(chan AuxFrame, 100), ended: make(chan error, 10)}
	h.SetStreamHandler(testAuxProto, func(s network.Stream) {
		for {
			f, err := ReadAuxFrame(s)
			if err != nil {
				r.ended <- err
				return
			}
			r.frames <- f
		}
	})
	return r
}

func (r *recvFrames) next(t *testing.T) AuxFrame {
	t.Helper()
	select {
	case f := <-r.frames:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
		return AuxFrame{}
	}
}

func expectFrame(t *testing.T, f AuxFrame, kind AuxFrameKind, seq uint64) {
	t.Helper()
	if f.Kind != kind || f.Seq != seq {
		t.Fatalf("got kind=%d seq=%d body=%q, want kind=%d seq=%d", f.Kind, f.Seq, f.Body, kind, seq)
	}
}

func waitPeers(t *testing.T, a *AuxStream, ctx context.Context, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.reconcile(ctx)
		if a.PeerCount() == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("aux peers = %d, want %d", a.PeerCount(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newAuxTestNet(t *testing.T, n int) []host.Host {
	t.Helper()
	mn, err := mocknet.FullMeshConnected(n)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mn.Close() })
	return mn.Hosts()
}

func TestAuxStreamDeliversInOrderAndResyncsOnGap(t *testing.T) {
	ctx := t.Context()
	hosts := newAuxTestNet(t, 2)
	seller, buyer := hosts[0], hosts[1]
	recv := handleAux(buyer)
	seller.Peerstore().AddProtocols(buyer.ID(), testAuxProto)

	bb := NewNodeBuffers()
	bb.Buffers[buyer.ID()] = &NodeBufferInfo{LibP2PState: Connected}

	feed := &testFeed{}
	a := newAuxStream(seller, bb, testAuxProto, feed.keyframe)

	// Nothing is opened before the producer has published anything.
	a.reconcile(ctx)
	if a.PeerCount() != 0 {
		t.Fatal("stream opened before the first publish")
	}
	a.Publish(AuxDelta, 1, feed.advance(1))
	waitPeers(t, a, ctx, 1)

	// A new stream starts with a keyframe of the current state.
	expectFrame(t, recv.next(t), AuxKeyframe, 1)
	for seq := uint64(2); seq <= 4; seq++ {
		a.Publish(AuxDelta, seq, feed.advance(seq))
		expectFrame(t, recv.next(t), AuxDelta, seq)
	}

	// Frame 5 never reaches the queue (as if the queue were full): the writer
	// sees 6 after 4 and sends a keyframe instead.
	feed.advance(5)
	a.Publish(AuxDelta, 6, feed.advance(6))
	expectFrame(t, recv.next(t), AuxKeyframe, 6)
	a.Publish(AuxDelta, 7, feed.advance(7))
	expectFrame(t, recv.next(t), AuxDelta, 7)

	// A keyframe published by the producer passes straight through.
	feed.advance(8)
	seq, kf := feed.keyframe()
	a.Publish(AuxKeyframe, seq, kf)
	expectFrame(t, recv.next(t), AuxKeyframe, 8)

	// The buyer leaving Connected closes the aux stream.
	bb.UpdateBufferLibP2PState(buyer.ID(), Reconnecting)
	a.reconcile(ctx)
	if a.PeerCount() != 0 {
		t.Fatal("aux stream kept after the buyer left Connected")
	}
	select {
	case <-recv.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("buyer's aux stream was not closed")
	}
}

func TestAuxStreamSkipsBuyersWithoutTheProtocol(t *testing.T) {
	ctx := t.Context()
	hosts := newAuxTestNet(t, 2)
	seller, oldBuyer := hosts[0], hosts[1]

	bb := NewNodeBuffers()
	bb.Buffers[oldBuyer.ID()] = &NodeBufferInfo{LibP2PState: Connected}
	feed := &testFeed{}
	a := newAuxStream(seller, bb, testAuxProto, feed.keyframe)
	a.Publish(AuxDelta, 1, feed.advance(1))
	a.reconcile(ctx)
	if a.PeerCount() != 0 {
		t.Fatal("aux stream opened to a buyer that doesn't advertise the protocol")
	}
}

func TestAuxStreamFailureLeavesBuyerStateAlone(t *testing.T) {
	ctx := t.Context()
	hosts := newAuxTestNet(t, 2)
	seller, buyer := hosts[0], hosts[1]
	// The buyer claims the protocol but resets every aux stream.
	buyer.SetStreamHandler(testAuxProto, func(s network.Stream) { s.Reset() })
	seller.Peerstore().AddProtocols(buyer.ID(), testAuxProto)

	bb := NewNodeBuffers()
	bb.Buffers[buyer.ID()] = &NodeBufferInfo{LibP2PState: Connected}
	feed := &testFeed{}
	a := newAuxStream(seller, bb, testAuxProto, feed.keyframe)
	a.Publish(AuxDelta, 1, feed.advance(1))
	waitPeers(t, a, ctx, 1)

	// Keep publishing until the writer hits the reset and gives up.
	deadline := time.Now().Add(5 * time.Second)
	for seq := uint64(2); a.PeerCount() != 0; seq++ {
		if time.Now().After(deadline) {
			t.Fatal("aux stream never failed against a resetting buyer")
		}
		a.Publish(AuxDelta, seq, feed.advance(seq))
		time.Sleep(20 * time.Millisecond)
	}

	info, _ := bb.GetBuffer(buyer.ID())
	if info.LibP2PState != Connected || info.DisconnectCount != 0 {
		t.Fatalf("aux failure touched the buyer: state=%s disconnects=%d", info.LibP2PState, info.DisconnectCount)
	}
	// The failure backs the peer off, so the next reconcile doesn't reopen.
	a.reconcile(ctx)
	if a.PeerCount() != 0 {
		t.Fatal("reopened a failed aux stream without backing off")
	}
}

func TestAuxEncoderDeflateRoundTrip(t *testing.T) {
	e, err := NewAuxEncoder(flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte(`{"hex":"ABCDEF","alt":3000},`), 200)
	// Twice, to cover reusing the compressor.
	for seq := uint64(1); seq <= 2; seq++ {
		frame := e.Frame(AuxDelta, seq, body)
		if len(frame) >= len(body)/4 {
			t.Fatalf("frame is %d bytes for a %d-byte repetitive body", len(frame), len(body))
		}
		f, err := ReadAuxFrame(bytes.NewReader(frame))
		if err != nil || f.Kind != AuxDelta || f.Seq != seq || f.Flags != AuxFlagDeflate {
			t.Fatalf("frame = %+v, %v", f, err)
		}
		p, err := f.Payload()
		if err != nil || !bytes.Equal(p, body) {
			t.Fatalf("payload mismatch: %v", err)
		}
	}
	// An uncompressed frame's payload is its body.
	f, _ := ReadAuxFrame(bytes.NewReader(AppendAuxFrame(nil, AuxKeyframe, 3, []byte("plain"))))
	if p, err := f.Payload(); err != nil || string(p) != "plain" {
		t.Fatalf("plain payload = %q, %v", p, err)
	}
	// Corrupt DEFLATE data is an error.
	if _, err := (AuxFrame{Flags: AuxFlagDeflate, Body: []byte{0xff, 0x00, 0x12}}).Payload(); err == nil {
		t.Fatal("corrupt deflate body accepted")
	}
}
