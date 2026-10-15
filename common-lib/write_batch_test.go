package commonlib

import (
	"bytes"
	"encoding/binary"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// fakeStream records writes. It embeds network.Stream so only the methods the
// writer calls need implementing.
type fakeStream struct {
	network.Stream
	mu      sync.Mutex
	writes  [][]byte
	resets  int
	writeFn func(p []byte) (int, error)
}

func (f *fakeStream) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeFn != nil {
		return f.writeFn(p)
	}
	f.writes = append(f.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (f *fakeStream) SetWriteDeadline(time.Time) error { return nil }

func (f *fakeStream) Reset() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
	return nil
}

func sentFrames(id peer.ID) uint64 {
	globalWriteStats.mu.Lock()
	defer globalWriteStats.mu.Unlock()
	return globalWriteStats.successfulWrites[id]
}

func droppedFramesFor(id peer.ID) uint64 {
	globalWriteStats.mu.Lock()
	defer globalWriteStats.mu.Unlock()
	return globalWriteStats.droppedFrames[id]
}

func TestWriteToAllPeersCoalescesFrames(t *testing.T) {
	bb := NewNodeBuffers()
	ids := []peer.ID{"batch-test-a", "batch-test-b"}
	streams := map[peer.ID]*fakeStream{}
	for _, id := range ids {
		s := &fakeStream{}
		streams[id] = s
		bb.Buffers[id] = &NodeBufferInfo{Writer: s, LibP2PState: Connected}
	}
	t.Cleanup(func() {
		for _, id := range ids {
			stopPeerWriteQueue(id)
			globalWriteStats.reset(id)
		}
	})

	// Seller records: a length byte, then that many bytes.
	const nFrames = 200
	var want []byte
	for i := 0; i < nFrames; i++ {
		frame := make([]byte, 57)
		frame[0] = 56
		binary.BigEndian.PutUint32(frame[1:], uint32(i))
		want = append(want, frame...)
		if errs := WriteToAllPeersParallel(bb, frame); len(errs) != 0 {
			t.Fatalf("frame %d: unexpected write results %v", i, errs)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for _, id := range ids {
		s := streams[id]
		for {
			s.mu.Lock()
			got := bytes.Join(s.writes, nil)
			nWrites := len(s.writes)
			s.mu.Unlock()
			if len(got) == len(want) {
				if !bytes.Equal(got, want) {
					t.Fatalf("peer %s: bytes arrived out of order or corrupted", id)
				}
				if nWrites > nFrames/4 {
					t.Errorf("peer %s: %d writes for %d frames, want them coalesced", id, nWrites, nFrames)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("peer %s: got %d of %d bytes", id, len(got), len(want))
			}
			time.Sleep(5 * time.Millisecond)
		}
		s.mu.Lock()
		for i, w := range s.writes {
			if len(w) > maxWriteBatchBytes+57 {
				t.Errorf("peer %s: write %d is %d bytes, over the batch cap", id, i, len(w))
			}
		}
		s.mu.Unlock()
		if got := sentFrames(id); got != nFrames {
			t.Errorf("peer %s: stats count %d frames sent, want %d", id, got, nFrames)
		}
	}
}

func TestWriteFramesTimeout(t *testing.T) {
	for _, tc := range []struct {
		name        string
		written     int
		wantErr     error
		wantResets  int
		wantState   LibP2PState
		wantDropped uint64
	}{
		{name: "nothing written drops the batch", written: 0, wantErr: FrameDropped, wantResets: 0, wantState: Connected, wantDropped: 3},
		{name: "partial write resets the stream", written: 10, wantResets: 1, wantState: Reconnecting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := peer.ID("timeout-test")
			t.Cleanup(func() { globalWriteStats.reset(id) })
			s := &fakeStream{writeFn: func(p []byte) (int, error) { return tc.written, os.ErrDeadlineExceeded }}
			bb := NewNodeBuffers()
			bb.Buffers[id] = &NodeBufferInfo{Writer: s, LibP2PState: Connected}

			err := writeFrames(s, id, bb, make([]byte, 3*57), 3)
			if tc.wantErr != nil && err != tc.wantErr {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && err == nil {
				t.Fatalf("want an error for a partial write")
			}
			if s.resets != tc.wantResets {
				t.Errorf("resets = %d, want %d", s.resets, tc.wantResets)
			}
			if st := bb.Buffers[id].LibP2PState; st != tc.wantState {
				t.Errorf("state = %s, want %s", st, tc.wantState)
			}
			if got := droppedFramesFor(id); got != tc.wantDropped {
				t.Errorf("dropped = %d, want %d", got, tc.wantDropped)
			}
		})
	}
}
