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

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func InitialConnect(ctx context.Context, p2pHost host.Host, addrInfo peer.AddrInfo, buyerBuffers *NodeBuffers, protocol protocol.ID) error {

	// show address info
	fmt.Println("address info of initial connect", addrInfo)

	info, exists := buyerBuffers.GetBuffer(addrInfo.ID)

	if exists && info.LibP2PState == Connected {
		if p2pHost.Network().Connectedness(addrInfo.ID) == network.Connected {
			if info.StreamHandler != nil && !network.Stream.Conn(*info.StreamHandler).IsClosed() {
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
		return fmt.Errorf("%s:error connecting: %w", CanNotConnectUnknownReason, conErr)
		//continue
	}

	fmt.Println("connected, create a stream ", addrInfo.ID)

	log.Println("connect and open stream")
	// --------  stuck here

	// Create a context with a timeout for the NewStream operation
	streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, conn := range p2pHost.Network().ConnsToPeer(addrInfo.ID) {
		log.Println("Connection to %s open with muxer: %s", addrInfo.ID, conn.ConnState())
	}

	s, strErr := p2pHost.NewStream(streamCtx, addrInfo.ID, protocol)
	if strErr != nil {
		log.Printf("First attempt failed, resetting connection and retrying: %v", strErr)
		p2pHost.Network().ClosePeer(addrInfo.ID)
		time.Sleep(1 * time.Second) // Brief delay before retry
		s, strErr = p2pHost.NewStream(ctx, addrInfo.ID, protocol)
		if strErr != nil {
			log.Println("failed to create a new stream in InitialConnect. ", strErr)
			log.Println("this is what we know about the buffer:  exists:", exists, " bufferrInfo", info)
			return fmt.Errorf("%s:error connecting: %w", CanNotConnectStreamError, strErr)
		}
		//continue
	}
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
		// check if the stream we have is closed
		if bufferInfo.StreamHandler != nil && !network.Stream.Conn(*bufferInfo.StreamHandler).IsClosed() {
			fmt.Println("Stream is already connected:", peerID)
			// Mark the buffer as connected
			connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, Connected)
			return nil
		}
	}

	// Attempt to reconnect. TODO: there's a case where we don't have an address after reboot. Remember last address and merge from bufferstate.
	fmt.Println("Attempting to reconnect to peer:", peerID)
	s, err := p2pHost.NewStream(ctx, peerID, protocol)
	if err != nil {
		log.Println("Stream creation failed to", peerID, ":", err)
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

func IsRequestTooEarly(connectedBuffersOfBuyers *NodeBuffers, peerID peer.ID) (bool, error) {
	reconnectAttempts, lastAttemptTime, exists := connectedBuffersOfBuyers.GetReconnectInfo(peerID)
	if !exists {
		return true, fmt.Errorf("%s:could not find the record for the peer %s in the state map. Make an initial connection", WeDoNotKnowPeer, peerID)
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
		return true, fmt.Errorf("%s:time since last attempt %v is less than backoff duration %v", HoldYourHorses, timeSinceLastAttempt, backoffDuration)
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
) error {
	if bufferInfo.Writer == nil {
		bufferInfo.LibP2PState = ConnectionLost
		return fmt.Errorf("%s:stream handler writer is nil", ConnectionLostWriteError)
	}

	if bufferInfo.LibP2PState == Connected {
		// Short write deadline to avoid blocking too long
		bufferInfo.Writer.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))

		writeStart := time.Now()
		_, writeErr := bufferInfo.Writer.Write(data)
		writeDuration := time.Since(writeStart)

		// Log a warning if we took >10ms to return from the write call
		if writeDuration > 10*time.Millisecond {
			log.Printf("⚠️ High write latency: %v. Receiver may be slow.", writeDuration)
		}

		if writeErr != nil {
			// If we timed out, treat that as "not ready yet"
			if netErr, ok := writeErr.(net.Error); ok && netErr.Timeout() {
				log.Printf("Write skipped: Receiver not ready (timeout). Resseting stream.")
				// reset stream
				bufferInfo.Writer.Reset()
				connectedBuffersOfBuyers.RemoveBuffer(peerID)
				return fmt.Errorf("%s:error writing to stream - 1:  %w", ConnectionLostWriteError, writeErr)

			}
			// Otherwise, update state & increment reconnect attempts
			bufferInfo.Writer.Reset()

			connectedBuffersOfBuyers.UpdateBufferLibP2PState(peerID, ConnectionLost)
			connectedBuffersOfBuyers.IncrementReconnectAttempts(peerID)
			connectedBuffersOfBuyers.RemoveBuffer(peerID)
			return fmt.Errorf("%s:error writing to stream - 2:  %w", ConnectionLostWriteError, writeErr)
		}

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
