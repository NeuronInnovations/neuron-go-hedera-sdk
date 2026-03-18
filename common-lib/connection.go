package commonlib

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	_ "net/http/pprof"

	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func InitialConnect(ctx context.Context, p2pHost host.Host, addrInfo peer.AddrInfo, buyerBuffers *NodeBuffers, protocol protocol.ID) error {
	start := time.Now()

	// show address info
	fmt.Println("address info of initial connect", addrInfo)

	info, exists := buyerBuffers.GetBuffer(addrInfo.ID)

	if exists && info.LibP2PState == Connected {
		if p2pHost.Network().Connectedness(addrInfo.ID) == network.Connected {
			// Check if Writer stream is valid (Writer is set by AddBuffer3, not StreamHandler)
			if info.Writer != nil {
				conn := info.Writer.Conn()
				if conn != nil && !conn.IsClosed() {
					fmt.Printf("😍😍 Thanks, we're good, connected and pumping %s -> ! 😍😍\n", addrInfo.ID)
					return nil
				}
			}
		}
		log.Println("the buffer is there but the state is not connected, we will try to reconnect")
	}

	// now there are two cases to test.
	// a buffer is there but state is not connected, or it is not there.

	log.Printf("[TRACE SELLER CONNECT] start peer=%s addrs=%v connectedness=%s", addrInfo.ID, addrInfo.Addrs, p2pHost.Network().Connectedness(addrInfo.ID))
	conErr := HolePunchConnectIfNotConnected(ctx, p2pHost, addrInfo, true)
	//conErr := p2pHost.Connect(ctx, *pid)
	if conErr != nil {
		log.Println(conErr)
		log.Printf("[TRACE SELLER CONNECT] connect failed peer=%s elapsed=%v err=%v", addrInfo.ID, time.Since(start).Round(time.Millisecond), conErr)
		return fmt.Errorf("%s:error connecting: %w", CanNotConnectUnknownReason, conErr)
		//continue
	}
	log.Printf("[TRACE SELLER CONNECT] connect ok peer=%s elapsed=%v conn_count=%d", addrInfo.ID, time.Since(start).Round(time.Millisecond), len(p2pHost.Network().ConnsToPeer(addrInfo.ID)))

	fmt.Println("connected, create a stream ", addrInfo.ID)

	log.Println("connect and open stream")
	// --------  stuck here

	// Create a context with a timeout for the NewStream operation
	streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, conn := range p2pHost.Network().ConnsToPeer(addrInfo.ID) {
		log.Printf("Connection to %s open with muxer: %v", addrInfo.ID, conn.ConnState())
	}

	log.Printf("[TRACE SELLER CONNECT] NewStream first attempt peer=%s elapsed=%v", addrInfo.ID, time.Since(start).Round(time.Millisecond))
	s, strErr := p2pHost.NewStream(streamCtx, addrInfo.ID, protocol)
	if strErr != nil {
		log.Printf("First attempt failed, resetting connection and retrying: %v", strErr)
		log.Printf("[TRACE SELLER CONNECT] NewStream first attempt failed peer=%s elapsed=%v err=%v", addrInfo.ID, time.Since(start).Round(time.Millisecond), strErr)
		p2pHost.Network().ClosePeer(addrInfo.ID)
		time.Sleep(1 * time.Second) // Brief delay before retry
		log.Printf("[TRACE SELLER CONNECT] NewStream retry peer=%s elapsed=%v", addrInfo.ID, time.Since(start).Round(time.Millisecond))
		s, strErr = p2pHost.NewStream(ctx, addrInfo.ID, protocol)
		if strErr != nil {
			log.Println("failed to create a new stream in InitialConnect. ", strErr)
			log.Println("this is what we know about the buffer:  exists:", exists, " bufferrInfo", info)
			log.Printf("[TRACE SELLER CONNECT] NewStream retry failed peer=%s elapsed=%v err=%v", addrInfo.ID, time.Since(start).Round(time.Millisecond), strErr)
			return fmt.Errorf("%s:error connecting: %w", CanNotConnectStreamError, strErr)
		}
		//continue
	}
	log.Printf("[TRACE SELLER CONNECT] NewStream ok peer=%s elapsed=%v stream_id=%s", addrInfo.ID, time.Since(start).Round(time.Millisecond), s.ID())
	fmt.Printf("😍😍 Stream connected and pumping %s -> %s ! 😍😍\n", addrInfo, p2pHost.ID())
	//streamWriter := bufio.NewWriterSize(s, 100)
	//streamWriter := bufio.NewWriter(s)

	// start pass the map to the jetvision.
	buyerBuffers.AddBuffer3(addrInfo.ID, s, SendOK, Connected)

	return nil

}

// ReconnectPeersIfNeeded attempts to re-establish a connection to a peer if its current state
// indicates it is not connected. This function is specifically utilized by the SELLER role.
//
// **Interaction with InitialConnect**:
// While `InitialConnect` is responsible for establishing a connection with a peer, including setting
// up a stream and writing data, `ReconnectPeersIfNeeded` focuses on recovering connections that were
// previously active but have transitioned to a disconnected state. This function assumes that the peer's
// address is already in the address book, making it effective for scenarios where the peer has restarted
// or temporarily gone offline.
//
// **Behavior**:
//   - If the peer's state is explicitly marked as `ConnectionLost`, it will not attempt reconnection, as
//     the system expects the peer to initiate a new request. In such cases, the buffer is removed, and an
//     error is returned.
//   - If sufficient backoff time has not passed since the last reconnect attempt, the function will skip
//     the reconnection to prevent excessive retries, as determined by `IsRequestTooEarly`.
//   - If the peer is already connected and its stream is valid, the state is updated to `Connected`.
//   - If reconnection is deemed necessary and viable, the function attempts to create a new stream to
//     the peer using the `NewStream` method of the LibP2P host.
//
// **Outcome**:
//   - Upon successful reconnection, the function updates the buffers with a new stream writer and sets
//     the peer's state to `Connected`.
//   - If reconnection fails, it increments the retry attempt counter and respects the backoff logic
//     defined in `IsRequestTooEarly`.
//
// This function complements the `InitialConnect` logic by ensuring resiliency in maintaining peer
// connections, particularly for long-lived seller nodes communicating with buyers.
func ReconnectPeersIfNeeded(ctx context.Context, p2pHost host.Host, peerID peer.ID, bufferInfo NodeBufferInfo, connectedBuffersOfBuyers *NodeBuffers, protocol protocol.ID) error {
	if bufferInfo.LibP2PState == Connected {
		// check if the adress book truly has a connection otherwise the state is not valid.
		if p2pHost.Network().Connectedness(peerID) == network.Connected {
			return nil
		} else {
			connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, Reconnecting)

		}
	}
	if bufferInfo.LibP2PState == ConnectionLost {
		// remove the buffer, even if this means you loose the shared account id or last IP address.
		connectedBuffersOfBuyers.RemoveBuffer(peerID)
		return fmt.Errorf("%s:we will not try to connect to %s, he is explicitly disconnected and expect him to issue a new request", bufferInfo.LibP2PState, peerID)
	}

	tooEarly, tooEarlyError := IsRequestTooEarly(connectedBuffersOfBuyers, peerID)
	if tooEarly {
		return tooEarlyError
	}

	// check if we're connected in the meantime
	if p2pHost.Network().Connectedness(peerID) == network.Connected {
		fmt.Println("Peer is already connected:", peerID)
		// check if the stream we have is valid (use Writer, not StreamHandler)
		if bufferInfo.Writer != nil {
			conn := bufferInfo.Writer.Conn()
			if conn != nil && !conn.IsClosed() {
				fmt.Println("Stream is already connected:", peerID)
				// Mark the buffer as connected
				connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, Connected)
				return nil
			}
		}
	}

	// Attempt to reconnect. TODO: there's a case where we don't have an address after reboot. Remember last address and merge from bufferstate.
	fmt.Println("Attempting to reconnect to peer:", peerID)
	s, err := p2pHost.NewStream(ctx, peerID, protocol)
	if err != nil {
		log.Println("Stream creation failed to", peerID, ":", err)
		connectedBuffersOfBuyers.RecordDisconnectEvent(peerID)
		connectedBuffersOfBuyers.IncrementReconnectAttempts(peerID)

		if bufferInfo.NoOfConnectionAttempts > 20 {
			connectedBuffersOfBuyers.RemoveBuffer(peerID)
		}
		return fmt.Errorf("%s:stream creation failed: %w", CanNotConnectStreamError, err)
	}

	// Successfully reconnected
	fmt.Printf("😍 -> Stream reconnected to %s\n", peerID)
	//streamWriter := bufio.NewWriterSize(s, 100)
	//streamWriter := bufio.NewWriter(s)

	connectedBuffersOfBuyers.AddBuffer3(peerID, s, ReceivedOK, Connected)
	return nil
}

// ErrGiveUpReconnect is returned by IsRequestTooEarly when we've been retrying for over 5 days and should stop.
var ErrGiveUpReconnect = fmt.Errorf("give up reconnect after 5 days")

// IsRequestTooEarly decides if we should wait before re-sending a service request.
// New peers get aggressive early retries; unstable peers back off until they normalize.
func IsRequestTooEarly(connectedBuffersOfBuyers *NodeBuffers, peerID peer.ID) (bool, error) {
	attemptsSinceSuccess, disconnectScore, hasEverConnected, lastAttemptTime, firstAttemptTime, exists := connectedBuffersOfBuyers.RetryPolicySnapshot(peerID)
	if !exists {
		return true, fmt.Errorf("%s:could not find the record for the peer %s in the state map. Make an initial connection", WeDoNotKnowPeer, peerID)
	}
	if firstAttemptTime.IsZero() {
		// Legacy buffer without FirstConnectionAttempt; treat as "just started".
		firstAttemptTime = lastAttemptTime
	}

	elapsed := time.Since(firstAttemptTime)

	// Give up after 5 days
	if elapsed > 5*24*time.Hour {
		return true, ErrGiveUpReconnect
	}

	interval := computeRetryDelay(attemptsSinceSuccess, hasEverConnected, disconnectScore)

	timeSinceLastAttempt := time.Since(lastAttemptTime)
	if timeSinceLastAttempt < interval {
		return true, fmt.Errorf("%s:time since last attempt %v is less than interval %v (attempts=%d ever_connected=%v disconnect_score=%d)", HoldYourHorses, timeSinceLastAttempt, interval, attemptsSinceSuccess, hasEverConnected, disconnectScore)
	}
	return false, nil
}

// connection.go (commonlib)
// TODO: rate limiter needs to come from a parameter so that it belongs to the seller thread
//var writeLimiter = rate.NewLimiter(rate.Limit(1000), 200) // 1000 writes/sec, burst=200

// FrameDropped is a sentinel error indicating the frame was dropped due to backpressure.
// This is not a connection error - the stream remains valid.
var FrameDropped = fmt.Errorf("frame dropped due to backpressure")

// RemoteClosed is a sentinel error indicating the remote peer closed the connection gracefully.
// This is expected behavior and not a critical error.
var RemoteClosed = fmt.Errorf("remote peer closed connection")

// WriteStats tracks write statistics per peer for debugging
type WriteStats struct {
	mu               sync.Mutex
	droppedFrames    map[peer.ID]uint64
	successfulWrites map[peer.ID]uint64
	lastLogTime      map[peer.ID]time.Time
	lastSuccessful   map[peer.ID]uint64 // Track last logged successful count for rate calculation
	peerPublicKeys   map[peer.ID]string // Short public key for log correlation
}

var globalWriteStats = &WriteStats{
	droppedFrames:    make(map[peer.ID]uint64),
	successfulWrites: make(map[peer.ID]uint64),
	lastLogTime:      make(map[peer.ID]time.Time),
	lastSuccessful:   make(map[peer.ID]uint64),
	peerPublicKeys:   make(map[peer.ID]string),
}

type WriteQueuePressure struct {
	ActiveQueues       int
	QueuedFrames       int
	MaxQueueDepth      int
	EnqueuedFrames     uint64
	DroppedFrames      uint64
	WriteTimeouts      uint64
	RemoteClosedWrites uint64
	WriteErrors        uint64
	SuccessfulWrites   uint64
}

var globalWritePressure = struct {
	enqueuedFrames     uint64
	droppedFrames      uint64
	writeTimeouts      uint64
	remoteClosedWrites uint64
	writeErrors        uint64
	successfulWrites   uint64
}{}

// Per-peer write queues to avoid goroutine explosion under high frame rates.
// Bounded queues drop frames instead of adding latency.
const peerWriteQueueSize = 300

type peerWriteQueue struct {
	ch   chan []byte
	done chan struct{}
}

var peerWriteQueues = struct {
	mu sync.Mutex
	m  map[peer.ID]*peerWriteQueue
}{
	m: make(map[peer.ID]*peerWriteQueue),
}

func ensurePeerWriteQueue(peerID peer.ID, buffers *NodeBuffers) *peerWriteQueue {
	peerWriteQueues.mu.Lock()
	defer peerWriteQueues.mu.Unlock()
	if q, ok := peerWriteQueues.m[peerID]; ok {
		return q
	}
	q := &peerWriteQueue{ch: make(chan []byte, peerWriteQueueSize), done: make(chan struct{})}
	peerWriteQueues.m[peerID] = q
	go func() {
		for {
			select {
			case data := <-q.ch:
				info, exists := buffers.GetBuffer(peerID)
				if !exists {
					continue
				}
				if info.LibP2PState != Connected {
					continue
				}
				_ = WriteAndFlushBuffer(info, peerID, buffers, data)
			case <-q.done:
				return
			}
		}
	}()
	return q
}

func stopPeerWriteQueue(peerID peer.ID) {
	peerWriteQueues.mu.Lock()
	q, ok := peerWriteQueues.m[peerID]
	if ok {
		delete(peerWriteQueues.m, peerID)
	}
	peerWriteQueues.mu.Unlock()
	if ok {
		close(q.done)
	}
}

func GetWriteQueuePressure() WriteQueuePressure {
	peerWriteQueues.mu.Lock()
	activeQueues := len(peerWriteQueues.m)
	queuedFrames := 0
	maxQueueDepth := 0
	for _, q := range peerWriteQueues.m {
		depth := len(q.ch)
		queuedFrames += depth
		if depth > maxQueueDepth {
			maxQueueDepth = depth
		}
	}
	peerWriteQueues.mu.Unlock()

	return WriteQueuePressure{
		ActiveQueues:       activeQueues,
		QueuedFrames:       queuedFrames,
		MaxQueueDepth:      maxQueueDepth,
		EnqueuedFrames:     atomic.LoadUint64(&globalWritePressure.enqueuedFrames),
		DroppedFrames:      atomic.LoadUint64(&globalWritePressure.droppedFrames),
		WriteTimeouts:      atomic.LoadUint64(&globalWritePressure.writeTimeouts),
		RemoteClosedWrites: atomic.LoadUint64(&globalWritePressure.remoteClosedWrites),
		WriteErrors:        atomic.LoadUint64(&globalWritePressure.writeErrors),
		SuccessfulWrites:   atomic.LoadUint64(&globalWritePressure.successfulWrites),
	}
}

// SetPeerPublicKey stores the short public key for a peer (for log correlation)
func (ws *WriteStats) SetPeerPublicKey(peerID peer.ID, publicKey string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	// Store last 8 chars of public key
	if len(publicKey) >= 8 {
		ws.peerPublicKeys[peerID] = publicKey[len(publicKey)-8:]
	} else {
		ws.peerPublicKeys[peerID] = publicKey
	}
}

// RegisterPeerPublicKey is the public API to associate a public key with a peer ID
// Call this when processing a service request to enable log correlation
func RegisterPeerPublicKey(peerID peer.ID, publicKey string) {
	globalWriteStats.SetPeerPublicKey(peerID, publicKey)
}

// peerLabel returns a string combining peer ID and public key for logging
func (ws *WriteStats) peerLabel(peerID peer.ID) string {
	shortPK := ws.peerPublicKeys[peerID]
	if shortPK != "" {
		return fmt.Sprintf("%s (pk:...%s)", peerID.ShortString(), shortPK)
	}
	return peerID.ShortString()
}

// logStats logs statistics if enough time has passed (call with lock held)
func (ws *WriteStats) logStats(peerID peer.ID) {
	if time.Since(ws.lastLogTime[peerID]) > 30*time.Second {
		dropped := ws.droppedFrames[peerID]
		successful := ws.successfulWrites[peerID]
		lastSuccessful := ws.lastSuccessful[peerID]

		// Calculate frames per second since last log
		elapsed := time.Since(ws.lastLogTime[peerID]).Seconds()
		fps := float64(successful-lastSuccessful) / elapsed

		var dropRate float64
		if dropped+successful > 0 {
			dropRate = float64(dropped) / float64(dropped+successful) * 100
		}

		label := ws.peerLabel(peerID)
		if dropped > 0 {
			log.Printf("📊 %s: %d sent, %d dropped (%.1f%% drop), %.0f fps",
				label, successful, dropped, dropRate, fps)
		} else {
			log.Printf("📊 %s: %d sent, 0 dropped, %.0f fps ✓",
				label, successful, fps)
		}

		ws.lastLogTime[peerID] = time.Now()
		ws.lastSuccessful[peerID] = successful
	}
}

func (ws *WriteStats) recordDrop(peerID peer.ID) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.droppedFrames[peerID]++
	ws.logStats(peerID)
}

func (ws *WriteStats) recordSuccess(peerID peer.ID) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.successfulWrites[peerID]++
	ws.logStats(peerID)
}

func (ws *WriteStats) reset(peerID peer.ID) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.droppedFrames, peerID)
	delete(ws.successfulWrites, peerID)
	delete(ws.lastLogTime, peerID)
	delete(ws.lastSuccessful, peerID)
	delete(ws.peerPublicKeys, peerID)
}

func WriteAndFlushBuffer(
	bufferInfo NodeBufferInfo,
	peerID peer.ID,
	connectedBuffersOfBuyers *NodeBuffers,
	data []byte,
) error {
	if bufferInfo.Writer == nil {
		bufferInfo.LibP2PState = ConnectionLost
		return fmt.Errorf("%s:stream handler is nil", ConnectionLostWriteError)
	}

	if bufferInfo.LibP2PState == Connected {
		// Use a reasonable deadline that allows for some network jitter
		// but doesn't block forever. With QUIC, the protocol handles
		// congestion control - a timeout means the send buffer is full.
		bufferInfo.Writer.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))

		_, writeErr := bufferInfo.Writer.Write(data)

		// Clear the deadline for future writes
		bufferInfo.Writer.SetWriteDeadline(time.Time{})

		if writeErr != nil {
			errStr := writeErr.Error()

			// If we timed out, just drop this frame - don't reset the stream.
			// QUIC is handling congestion control; the buffer will drain.
			if netErr, ok := writeErr.(net.Error); ok && netErr.Timeout() {
				atomic.AddUint64(&globalWritePressure.writeTimeouts, 1)
				atomic.AddUint64(&globalWritePressure.droppedFrames, 1)
				globalWriteStats.recordDrop(peerID)
				return FrameDropped
			}

			// Check for graceful remote close (QUIC Application error 0x0)
			// This means the remote peer closed the connection intentionally.
			// Keep the buffer so we can try to reconnect - don't remove it.
			if strings.Contains(errStr, "Application error 0x0") {
				atomic.AddUint64(&globalWritePressure.remoteClosedWrites, 1)
				log.Printf("Remote peer %s closed connection gracefully - will attempt reconnection", peerID.ShortString())
				bufferInfo.Writer.Reset()
				connectedBuffersOfBuyers.RecordDisconnectEvent(peerID)
				connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, Reconnecting)
				// Don't remove buffer - keep it for reconnection attempts
				globalWriteStats.reset(peerID)
				return RemoteClosed
			}

			// Check for Application error 0x1 (remote rejection - often means duplicate stream)
			// The remote peer rejected our stream, possibly because one already exists.
			// Keep the buffer so we can try to reconnect later.
			if strings.Contains(errStr, "Application error 0x1") {
				atomic.AddUint64(&globalWritePressure.remoteClosedWrites, 1)
				log.Printf("Remote peer %s rejected stream (0x1) - possibly duplicate, will retry later", peerID.ShortString())
				bufferInfo.Writer.Reset()
				connectedBuffersOfBuyers.RecordDisconnectEvent(peerID)
				connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, Reconnecting)
				globalWriteStats.reset(peerID)
				return RemoteClosed
			}

			// For actual connection errors (broken pipe, reset, etc.), reset the stream
			atomic.AddUint64(&globalWritePressure.writeErrors, 1)
			log.Printf("Write error to %s: %v - resetting stream", peerID, writeErr)
			bufferInfo.Writer.Reset()
			connectedBuffersOfBuyers.RecordDisconnectEvent(peerID)
			connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, ConnectionLost)
			connectedBuffersOfBuyers.IncrementReconnectAttempts(peerID)
			connectedBuffersOfBuyers.RemoveBuffer(peerID)
			globalWriteStats.reset(peerID)
			return fmt.Errorf("%s:error writing to stream: %w", ConnectionLostWriteError, writeErr)
		}

		globalWriteStats.recordSuccess(peerID)
		atomic.AddUint64(&globalWritePressure.successfulWrites, 1)
		return nil
	}
	return fmt.Errorf("%s:buffer is not Connected %v", bufferInfo.LibP2PState, peerID)
}

func HolePunchConnectIfNotConnected(ctx context.Context, p2pHost host.Host, pi peer.AddrInfo, isClient bool) error {
	//if p2pHost.Network().Connectedness(pi.ID) != network.Connected {
	holePunchCtx := network.WithSimultaneousConnect(ctx, isClient, "hole-punching")
	forceDirectConnCtx := network.WithForceDirectDial(holePunchCtx, "hole-punching")
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, time.Second*30)
	defer cancel()
	if err := p2pHost.Connect(dialCtx, pi); err != nil {
		log.Println("hole punch failed to connect to ", pi.ID)
		return err
	}
	log.Println("hole punch connected to ", pi.ID)
	return nil
}

// PeerWriteResult holds the result of a write operation to a single peer
type PeerWriteResult struct {
	PeerID peer.ID
	Error  error
}

// WriteToAllPeersParallel writes data to all connected peers concurrently.
// This avoids the latency issue where a slow peer blocks writes to other peers.
// With QUIC, each write goes into the protocol's send buffer, so parallel writes
// are safe and efficient.
//
// Only writes to peers in Connected state - peers in Reconnecting or other states are skipped.
// Returns a slice of PeerWriteResult for any failures (successful writes are not included).
func WriteToAllPeersParallel(buffers *NodeBuffers, data []byte) []PeerWriteResult {
	bufferMap := buffers.GetBufferMap()
	if len(bufferMap) == 0 {
		return nil
	}

	// Enqueue writes per peer to avoid goroutine explosion; drop if queue full.
	var errors []PeerWriteResult
	for peerID, bufferInfo := range bufferMap {
		if bufferInfo.LibP2PState != Connected {
			continue
		}
		q := ensurePeerWriteQueue(peerID, buffers)
		select {
		case q.ch <- data:
			atomic.AddUint64(&globalWritePressure.enqueuedFrames, 1)
			// enqueued
		default:
			// Queue full: drop one old frame and try to enqueue the newest.
			select {
			case <-q.ch:
			default:
			}
			select {
			case q.ch <- data:
				atomic.AddUint64(&globalWritePressure.enqueuedFrames, 1)
				atomic.AddUint64(&globalWritePressure.droppedFrames, 1)
				// enqueued after dropping one
			default:
				// Still full - drop newest to keep latency low
				atomic.AddUint64(&globalWritePressure.droppedFrames, 1)
				globalWriteStats.recordDrop(peerID)
				errors = append(errors, PeerWriteResult{PeerID: peerID, Error: FrameDropped})
			}
		}
	}

	return errors
}
