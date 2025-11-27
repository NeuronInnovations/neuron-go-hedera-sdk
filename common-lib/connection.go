package commonlib

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"

	_ "net/http/pprof"

	//commonlib "neuron/sdk/common-lib"
	//commonlib "neuron/sdk/common-lib"

	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// GetStreamHandler finds and returns the stream for a given peer and protocol ID
func GetStreamHandler(p2pHost host.Host, peerID peer.ID, protocolID protocol.ID) (*network.Stream, error) {
	// Get all connections to the peer
	conns := p2pHost.Network().ConnsToPeer(peerID)
	if len(conns) == 0 {
		return nil, fmt.Errorf("no connections found for peer %s", peerID)
	}

	// Check each connection for a matching stream
	for _, conn := range conns {
		for _, stream := range conn.GetStreams() {
			if stream.Protocol() == protocolID {
				log.Println("found stream for protocol", protocolID, "with peer", peerID, "stream id", stream.ID())
				return &stream, nil
			}
		}
	}

	return nil, fmt.Errorf("no stream found for protocol %s with peer %s", protocolID, peerID)
}

// Note: Only the seller can perform the initial connection. The buyer initiates the request
// but the seller must establish the actual connection and stream.
func InitialConnect(ctx context.Context, p2pHost host.Host, addrInfo peer.AddrInfo, buyerBuffers *NodeBuffers, protocol protocol.ID) error {

	// show address info
	fmt.Println("address info of initial connect", addrInfo)

	info, exists := buyerBuffers.GetBuffer(addrInfo.ID)

	if exists && info.LibP2PState == types.Connected {
		if p2pHost.Network().Connectedness(addrInfo.ID) == network.Connected {

			stream, err := GetStreamHandler(p2pHost, addrInfo.ID, protocol)
			if err != nil {
				log.Println("error getting stream", err)
				return fmt.Errorf("%s:error getting stream: %w", types.CanNotConnectStreamError, err)
			}

			if stream != nil && !network.Stream.Conn(*stream).IsClosed() {
				fmt.Printf("😍😍 Thanks, we're good, connected and pumping %s -> ! 😍😍\n", addrInfo.ID)
				return nil
			}
		}
		log.Println("the buffer is there but the state is not connected, we will try to reconnect")
	}

	// now there are two cases to test.
	// a buffer is there but state is not connected, or it is not there.

	conErr := HolePunchConnectIfNotConnected(ctx, p2pHost, addrInfo, true)
	//conErr := p2pHost.Connect(ctx, *pid)
	if conErr != nil {
		log.Println(conErr)
		return fmt.Errorf("%s:error connecting: %w", types.CanNotConnectUnknownReason, conErr)
		//continue
	}

	fmt.Println("connected, create a stream ", addrInfo.ID)

	log.Println("connect and open stream")
	// --------  stuck here

	// Create a context with a timeout for the NewStream operation
	//streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	//defer cancel()

	for _, conn := range p2pHost.Network().ConnsToPeer(addrInfo.ID) {
		log.Printf("Connection to %s open with muxer: %v", addrInfo.ID, conn.ConnState())
	}

	newStream, strErr := p2pHost.NewStream(ctx, addrInfo.ID, protocol)
	if strErr != nil {
		log.Printf("First attempt failed, resetting connection and retrying: %v", strErr)
		p2pHost.Network().ClosePeer(addrInfo.ID)
		time.Sleep(1 * time.Second) // Brief delay before retry
		newStream, strErr = p2pHost.NewStream(ctx, addrInfo.ID, protocol)
		if strErr != nil {
			log.Println("failed to create a new stream in InitialConnect. ", strErr)
			log.Println("this is what we know about the buffer:  exists:", exists, " bufferrInfo", info)
			return fmt.Errorf("%s:error connecting: %w", types.CanNotConnectStreamError, strErr)
		}
		//continue
	}
	fmt.Printf("😍😍 Stream connected and pumping %s -> %s ! 😍😍 %v\n", addrInfo, p2pHost.ID(), newStream)
	//streamWriter := bufio.NewWriterSize(s, 100)
	//streamWriter := bufio.NewWriter(s)

	newStream.Write(nil)
	buyerBuffers.AddBuffer3(addrInfo.ID, types.SendOK, types.Connected)

	return nil

}

func IsRequestTooEarly(connectedBuffersOfBuyers *NodeBuffers, peerID peer.ID) (bool, error) {
	reconnectAttempts, lastAttemptTime, exists := connectedBuffersOfBuyers.GetReconnectInfo(peerID)
	if !exists {
		return true, fmt.Errorf("%s:could not find the record for the peer %s in the state map. Make an initial connection", types.WeDoNotKnowPeer, peerID)
	}

	// Start with an initial backoff of 30 seconds
	initialBackoff := time.Second * 30
	backoffDuration := initialBackoff * time.Duration(math.Pow(2, float64(reconnectAttempts)))

	// Cap the backoff duration at 12 hours
	maxBackoff := time.Hour * 12
	if backoffDuration > maxBackoff {
		backoffDuration = maxBackoff
	}

	timeSinceLastAttempt := time.Since(lastAttemptTime)

	if timeSinceLastAttempt < backoffDuration {
		return true, fmt.Errorf("%s:time since last attempt %v is less than backoff duration %v", types.HoldYourHorses, timeSinceLastAttempt, backoffDuration)
	}

	return false, nil
}

// connection.go (commonlib)
// TODO: reate limiter needs to come from a parameter so hat it belongs to the seller thread
//var writeLimiter = rate.NewLimiter(rate.Limit(1000), 200) // 1000 writes/sec, burst=200

func WriteAndFlushBuffer(
	bufferInfo NodeBufferInfo,
	peerID peer.ID,
	connectedBuffersOfBuyers *NodeBuffers,
	data []byte,
	p2pHost host.Host,
	protocol protocol.ID,
) error {

	// Check if peer is connected
	activeConns := p2pHost.Network().ConnsToPeer(peerID)
	if len(activeConns) > 0 {
		bufferInfo.LibP2PState = types.Connected
	} else {
		bufferInfo.LibP2PState = types.ConnectionLost
		return fmt.Errorf("%s:peer is not connected", types.ConnectionLostWriteError)
	}
	stream, err := GetStreamHandler(p2pHost, peerID, protocol)
	if err != nil {
		log.Println("error getting stream", err)
		return fmt.Errorf("%s:error getting stream: %w", types.CanNotConnectStreamError, err)
	}
	if stream == nil {
		bufferInfo.LibP2PState = types.ConnectionLost
		return fmt.Errorf("%s:stream handler is nil", types.ConnectionLostWriteError)

	}

	// Short write deadline to avoid blocking too long
	//stream.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))

	//writeStart := time.Now()
	writer := *stream
	bytesWritten, writeErr := writer.Write(data)
	log.Println("bytes written", bytesWritten)

	if writeErr != nil {
		// If we timed out, treat that as "not ready yet"
		if netErr, ok := writeErr.(net.Error); ok && netErr.Timeout() {
			log.Printf("Write skipped: Receiver not ready (timeout). Resseting stream.")
			// reset stream
			//stream.Reset()
			connectedBuffersOfBuyers.RemoveBuffer(peerID)
			return fmt.Errorf("%s:error writing to stream - 1:  %w", types.ConnectionLostWriteError, writeErr)

		}
		// Otherwise, update state & increment reconnect attempts
		//stream.Reset()

		connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, types.ConnectionLost)
		connectedBuffersOfBuyers.IncrementReconnectAttempts(peerID)
		connectedBuffersOfBuyers.RemoveBuffer(peerID)
		return fmt.Errorf("%s:error writing to stream - 2:  %w", types.ConnectionLostWriteError, writeErr)
	}

	return nil

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

// AttemptReconnectFromCache attempts to reconnect to a peer using cached IP addresses from BBolt.
// This function enables Hedera-free reconnection by using persisted connection data.
// It tries the last known IP address with 3 retries and exponential backoff (1s, 2s, 4s).
// Returns nil on success, error on failure after all retries exhausted.
func AttemptReconnectFromCache(ctx context.Context, p2pHost host.Host, peerID peer.ID,
	buffers *NodeBuffers, protocolID protocol.ID) error {

	if buffers == nil {
		return fmt.Errorf("buffers is nil, cannot attempt cached reconnection")
	}

	// Get cached buffer info for the peer
	bufferInfo, exists := buffers.GetBuffer(peerID)
	if !exists {
		return fmt.Errorf("no cached buffer found for peer %s", peerID.ShortString())
	}

	// Check if we have a cached address
	cachedAddr := bufferInfo.LastOtherSideMultiAddress
	if cachedAddr == "" {
		return fmt.Errorf("no cached address for peer %s", peerID.ShortString())
	}

	log.Printf("🔄 Attempting cached reconnection to peer %s using address: %s", peerID.ShortString(), cachedAddr)

	// Update state to indicate we're attempting cached reconnection
	buffers.UpdateBufferLibP2PState(peerID, types.ReconnectingFromCache)
	buffers.IncrementCacheReconnectAttempts(peerID)

	// Parse the cached multiaddress
	maddr, err := parseMultiaddr(cachedAddr)
	if err != nil {
		log.Printf("Failed to parse cached multiaddress %s: %v", cachedAddr, err)
		return fmt.Errorf("invalid cached address: %w", err)
	}

	// Create AddrInfo for connection attempt
	addrInfo := peer.AddrInfo{
		ID:    peerID,
		Addrs: maddr,
	}

	// Retry loop with exponential backoff (1s, 2s, 4s)
	maxRetries := 3
	baseBackoff := time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("🔌 Cache reconnection attempt %d/%d for peer %s", attempt, maxRetries, peerID.ShortString())

		// Create timeout context for this attempt
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		// Attempt connection
		err := p2pHost.Connect(connectCtx, addrInfo)
		cancel()

		if err == nil {
			// Connection successful!
			log.Printf("✅ Cache reconnection SUCCESS on attempt %d for peer %s", attempt, peerID.ShortString())

			// Verify connection is actually established
			if p2pHost.Network().Connectedness(peerID) == network.Connected {
				buffers.UpdateBufferLibP2PState(peerID, types.Connected)
				buffers.ResetCacheReconnectAttempts(peerID)

				// Try to open a stream to verify full connectivity
				streamCtx, streamCancel := context.WithTimeout(ctx, 5*time.Second)
				stream, streamErr := p2pHost.NewStream(streamCtx, peerID, protocolID)
				streamCancel()

				if streamErr == nil {
					log.Printf("✅ Stream opened successfully after cache reconnection to %s", peerID.ShortString())
					stream.Close() // Close the test stream, caller will open their own
					return nil
				}
				log.Printf("⚠️ Connected but stream failed for %s: %v", peerID.ShortString(), streamErr)
				// Still consider this a success - connection is established
				return nil
			}
		}

		log.Printf("⚠️ Cache reconnection attempt %d/%d failed for peer %s: %v", attempt, maxRetries, peerID.ShortString(), err)

		// If not the last attempt, wait with exponential backoff
		if attempt < maxRetries {
			backoffDuration := baseBackoff * time.Duration(1<<(attempt-1)) // 1s, 2s, 4s
			log.Printf("⏳ Waiting %v before next cache reconnection attempt", backoffDuration)
			time.Sleep(backoffDuration)
		}
	}

	// All retries exhausted
	log.Printf("❌ Cache reconnection FAILED for peer %s after %d attempts", peerID.ShortString(), maxRetries)
	buffers.UpdateBufferLibP2PState(peerID, types.ConnectionLost)
	return fmt.Errorf("cache reconnection failed after %d attempts", maxRetries)
}

// parseMultiaddr parses a cached multiaddress string which may contain multiple addresses
// separated by spaces or be a single address
func parseMultiaddr(addrStr string) ([]multiaddr.Multiaddr, error) {
	var addrs []multiaddr.Multiaddr

	// Handle space-separated addresses (e.g., from Fields split)
	parts := splitAddresses(addrStr)

	for _, part := range parts {
		if part == "" {
			continue
		}
		addr, err := multiaddr.NewMultiaddr(part)
		if err != nil {
			log.Printf("Skipping invalid multiaddr %s: %v", part, err)
			continue
		}
		addrs = append(addrs, addr)
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("no valid multiaddresses found in: %s", addrStr)
	}

	return addrs, nil
}

// splitAddresses splits a string that may contain multiple multiaddresses
func splitAddresses(s string) []string {
	// Handle different formats:
	// 1. Single address: "/ip4/1.2.3.4/udp/1234/quic-v1"
	// 2. Space-separated: "/ip4/1.2.3.4/udp/1234 /ip4/5.6.7.8/udp/5678"
	// 3. Already split

	var result []string
	current := ""

	for i := 0; i < len(s); i++ {
		if s[i] == '/' && current != "" && (i == 0 || s[i-1] == ' ') {
			// New address starting
			if current != "" {
				result = append(result, current)
			}
			current = "/"
		} else if s[i] == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(s[i])
		}
	}

	if current != "" {
		result = append(result, current)
	}

	// If no splits were made, return the original
	if len(result) == 0 {
		return []string{s}
	}

	return result
}
