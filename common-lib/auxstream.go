package commonlib

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// An aux stream carries a second feed to each buyer on its own protocol, next
// to the main stream on the same libp2p connection. The feed is sequenced:
// keyframes carry the whole state, deltas apply on top of the previous frame.
// Each peer gets its own stream and writer, so a slow or failing aux stream
// never touches the main stream or the buyer's NodeBuffers state. A peer whose
// queue overflows skips ahead to a fresh keyframe instead of falling behind.
//
// Only peers that advertise the protocol (identify) get a stream, so buyers
// without a handler keep working untouched.

// AuxFrameKind says how a reader applies an aux frame.
type AuxFrameKind byte

const (
	// AuxKeyframe is self-contained: it replaces the reader's state.
	AuxKeyframe AuxFrameKind = 1
	// AuxDelta applies on top of the frame with the previous sequence number.
	AuxDelta AuxFrameKind = 2
)

// Aux frame layout, big-endian:
//
//	[len u32][kind u8][flags u8][seq u64][body]
//
// len counts the bytes after itself.
const (
	auxHeaderLen = 4 + 1 + 1 + 8
	// MaxAuxFrameLen bounds len on read, so a corrupt prefix can't make a
	// reader allocate gigabytes.
	MaxAuxFrameLen = 16 << 20
	// maxAuxPayloadLen bounds an inflated body.
	maxAuxPayloadLen = 64 << 20
)

// AuxFlagDeflate marks a body compressed with raw DEFLATE (RFC 1951).
const AuxFlagDeflate byte = 1

// AuxFrame is one decoded aux-stream frame.
type AuxFrame struct {
	Kind  AuxFrameKind
	Flags byte
	Seq   uint64
	Body  []byte // as sent; see Payload
}

// Payload returns the frame's body, inflated if it was sent compressed.
func (f AuxFrame) Payload() ([]byte, error) {
	if f.Flags&AuxFlagDeflate == 0 {
		return f.Body, nil
	}
	out, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(f.Body)), maxAuxPayloadLen+1))
	if err != nil {
		return nil, fmt.Errorf("inflate aux frame %d: %w", f.Seq, err)
	}
	if len(out) > maxAuxPayloadLen {
		return nil, fmt.Errorf("aux frame %d inflates past %d bytes", f.Seq, maxAuxPayloadLen)
	}
	return out, nil
}

// AppendAuxFrame appends an uncompressed frame to dst.
func AppendAuxFrame(dst []byte, kind AuxFrameKind, seq uint64, body []byte) []byte {
	return appendAuxFrame(dst, kind, 0, seq, body)
}

func appendAuxFrame(dst []byte, kind AuxFrameKind, flags byte, seq uint64, body []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(auxHeaderLen-4+len(body)))
	dst = append(dst, byte(kind), flags)
	dst = binary.BigEndian.AppendUint64(dst, seq)
	return append(dst, body...)
}

// AuxEncoder builds deflated frames, reusing one compressor. A producer
// encodes each frame once and publishes the same bytes to every peer. It is
// not safe for concurrent use.
type AuxEncoder struct {
	zw  *flate.Writer
	buf bytes.Buffer
}

// NewAuxEncoder returns an encoder compressing at a compress/flate level;
// flate.BestSpeed costs about a third of the default on ARMv7 sensors.
func NewAuxEncoder(level int) (*AuxEncoder, error) {
	zw, err := flate.NewWriter(nil, level)
	if err != nil {
		return nil, err
	}
	return &AuxEncoder{zw: zw}, nil
}

// Frame returns a new frame holding body, deflated.
func (e *AuxEncoder) Frame(kind AuxFrameKind, seq uint64, body []byte) []byte {
	e.buf.Reset()
	e.zw.Reset(&e.buf)
	e.zw.Write(body) // writes to a bytes.Buffer don't fail
	e.zw.Close()
	return appendAuxFrame(make([]byte, 0, auxHeaderLen+e.buf.Len()), kind, AuxFlagDeflate, seq, e.buf.Bytes())
}

// ReadAuxFrame reads one frame from r. Body is freshly allocated.
func ReadAuxFrame(r io.Reader) (AuxFrame, error) {
	var hdr [auxHeaderLen]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return AuxFrame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[0:4])
	if n < auxHeaderLen-4 || n > MaxAuxFrameLen {
		return AuxFrame{}, fmt.Errorf("aux frame length %d out of range", n)
	}
	f := AuxFrame{
		Kind:  AuxFrameKind(hdr[4]),
		Flags: hdr[5],
		Seq:   binary.BigEndian.Uint64(hdr[6:14]),
		Body:  make([]byte, n-(auxHeaderLen-4)),
	}
	if _, err := io.ReadFull(r, f.Body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return AuxFrame{}, err
	}
	return f, nil
}

// AuxKeyframeFunc returns an encoded keyframe (AppendAuxFrame output) for the
// state after the newest published frame, and that frame's seq. The producer
// must update this state before it publishes the frame, so a keyframe always
// covers every frame already handed to Publish.
type AuxKeyframeFunc func() (seq uint64, frame []byte)

const (
	auxReconcileEvery = 2 * time.Second
	auxQueueLen       = 32
	auxOpenTimeout    = 10 * time.Second
	auxWriteTimeout   = 10 * time.Second
	auxRetryMin       = 5 * time.Second
	auxRetryMax       = 5 * time.Minute
	// A stream that lived this long counts as healthy and clears its backoff.
	auxHealthyAfter = 5 * time.Minute
	auxStatsEvery   = 5 * time.Minute
)

// AuxStream fans one sequenced feed out to every Connected buyer that
// supports its protocol.
type AuxStream struct {
	host     host.Host
	buffers  *NodeBuffers
	protocol protocol.ID
	keyframe AuxKeyframeFunc

	published atomic.Bool

	mu       sync.Mutex
	peers    map[peer.ID]*auxPeer
	retryAt  map[peer.ID]time.Time
	failures map[peer.ID]int
}

type auxMsg struct {
	kind  AuxFrameKind
	seq   uint64
	frame []byte
}

type auxPeer struct {
	ch       chan auxMsg
	done     chan struct{}
	stopOnce sync.Once

	frames  atomic.Uint64
	bytes   atomic.Uint64
	resyncs atomic.Uint64
	dropped atomic.Uint64
}

func (p *auxPeer) stop() { p.stopOnce.Do(func() { close(p.done) }) }

// StartAuxStream starts serving protocol to the Connected peers in buffers.
// It runs until ctx is done.
func StartAuxStream(ctx context.Context, h host.Host, buffers *NodeBuffers, proto protocol.ID, keyframe AuxKeyframeFunc) *AuxStream {
	a := newAuxStream(h, buffers, proto, keyframe)
	go a.run(ctx)
	return a
}

func newAuxStream(h host.Host, buffers *NodeBuffers, proto protocol.ID, keyframe AuxKeyframeFunc) *AuxStream {
	return &AuxStream{
		host:     h,
		buffers:  buffers,
		protocol: proto,
		keyframe: keyframe,
		peers:    make(map[peer.ID]*auxPeer),
		retryAt:  make(map[peer.ID]time.Time),
		failures: make(map[peer.ID]int),
	}
}

// Publish queues a frame for every peer with an open aux stream. A peer whose
// queue is full misses it; its writer sees the sequence gap and resyncs from a
// keyframe.
func (a *AuxStream) Publish(kind AuxFrameKind, seq uint64, frame []byte) {
	a.published.Store(true)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.peers {
		select {
		case p.ch <- auxMsg{kind: kind, seq: seq, frame: frame}:
		default:
			p.dropped.Add(1)
		}
	}
}

// PeerCount returns how many peers have an aux stream open or opening.
func (a *AuxStream) PeerCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.peers)
}

func (a *AuxStream) run(ctx context.Context) {
	tick := time.NewTicker(auxReconcileEvery)
	defer tick.Stop()
	lastStats := time.Now()
	for {
		select {
		case <-ctx.Done():
			a.mu.Lock()
			for id, p := range a.peers {
				p.stop()
				delete(a.peers, id)
			}
			a.mu.Unlock()
			return
		case <-tick.C:
			a.reconcile(ctx)
			if time.Since(lastStats) >= auxStatsEvery {
				a.logStats()
				lastStats = time.Now()
			}
		}
	}
}

// reconcile opens a stream to each Connected peer that supports the protocol
// and has none, and stops the streams of peers that are no longer Connected.
// It never dials: (re)connecting is the main stream's job.
func (a *AuxStream) reconcile(ctx context.Context) {
	connected := a.buffers.appendConnectedPeerIDs(nil)
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	live := make(map[peer.ID]struct{}, len(connected))
	for _, id := range connected {
		live[id] = struct{}{}
		if _, ok := a.peers[id]; ok {
			continue
		}
		if !a.published.Load() || now.Before(a.retryAt[id]) || !a.supports(id) {
			continue
		}
		p := &auxPeer{ch: make(chan auxMsg, auxQueueLen), done: make(chan struct{})}
		a.peers[id] = p
		go a.runPeer(ctx, id, p)
	}
	for id, p := range a.peers {
		if _, ok := live[id]; !ok {
			p.stop()
			delete(a.peers, id)
		}
	}
}

func (a *AuxStream) supports(id peer.ID) bool {
	if a.host.Network().Connectedness(id) != network.Connected {
		return false
	}
	ps, err := a.host.Peerstore().SupportsProtocols(id, a.protocol)
	return err == nil && len(ps) > 0
}

// runPeer opens the peer's stream, sends a keyframe, then forwards frames in
// sequence until the peer is stopped or a write fails.
func (a *AuxStream) runPeer(ctx context.Context, id peer.ID, p *auxPeer) {
	openCtx, cancel := context.WithTimeout(network.WithNoDial(ctx, "aux stream"), auxOpenTimeout)
	s, err := a.host.NewStream(openCtx, id, a.protocol)
	cancel()
	if err != nil {
		a.end(id, p, time.Time{}, fmt.Errorf("open: %w", err))
		return
	}
	opened := time.Now()
	log.Printf("📎 aux %s: stream open to %s", a.protocol, id.ShortString())

	var last uint64
	write := func(seq uint64, frame []byte) error {
		_ = s.SetWriteDeadline(time.Now().Add(auxWriteTimeout))
		_, err := s.Write(frame)
		if err != nil {
			return err
		}
		last = seq
		p.frames.Add(1)
		p.bytes.Add(uint64(len(frame)))
		return nil
	}

	// Every stream starts from a keyframe. A partial write leaves the reader
	// mid-frame, so any write error ends the stream and the next one starts over.
	err = write(a.keyframe())
	for err == nil {
		select {
		case <-p.done:
			s.Reset()
			a.end(id, p, opened, nil)
			return
		case m := <-p.ch:
			switch {
			case m.seq <= last:
				// Already covered by the keyframe we sent.
			case m.kind == AuxKeyframe || m.seq == last+1:
				err = write(m.seq, m.frame)
			default:
				// A gap: frames were dropped while the queue was full. The
				// keyframe covers this frame and everything before it.
				p.resyncs.Add(1)
				err = write(a.keyframe())
			}
		}
	}
	s.Reset()
	a.end(id, p, opened, fmt.Errorf("write: %w", err))
}

// end forgets a peer's stream. A failed stream backs the peer off; a stream
// stopped because the peer left Connected does not.
func (a *AuxStream) end(id peer.ID, p *auxPeer, opened time.Time, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.peers[id] == p {
		delete(a.peers, id)
	}
	if err == nil {
		log.Printf("📎 aux %s: stream to %s closed (peer no longer connected), %d frames sent", a.protocol, id.ShortString(), p.frames.Load())
		return
	}
	if !opened.IsZero() && time.Since(opened) >= auxHealthyAfter {
		a.failures[id] = 0
	}
	delay := auxRetryMin << a.failures[id]
	if delay > auxRetryMax || delay <= 0 {
		delay = auxRetryMax
	} else {
		a.failures[id]++
	}
	a.retryAt[id] = time.Now().Add(delay)
	log.Printf("📎 aux %s: stream to %s ended after %d frames: %v (retry in %v)", a.protocol, id.ShortString(), p.frames.Load(), err, delay)
}

func (a *AuxStream) logStats() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, p := range a.peers {
		log.Printf("📎 aux %s: %s: %d frames, %d KiB sent, %d resyncs, %d dropped",
			a.protocol, id.ShortString(), p.frames.Load(), p.bytes.Load()>>10, p.resyncs.Load(), p.dropped.Load())
	}
}
