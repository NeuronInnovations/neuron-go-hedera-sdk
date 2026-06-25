package streambuyervsseller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	validatorLib "github.com/NeuronInnovations/neuron-go-hedera-sdk/validator-lib"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"

	hedera_helper "github.com/NeuronInnovations/neuron-go-hedera-sdk/hedera"

	neuronbuffers "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"

	flags "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

func HandleSellerCase(ctx context.Context, p2pHost host.Host, protocol protocol.ID, sellerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers), sellerCaseTopicCallBack func(topicMessage hedera.TopicMessage)) {
	fmt.Println("Acting as a data seller (I'll be waiting on my topic for requests and serve them on the stream)")

	// Self-heal a wedged outbound UDP path: if we dial buyers but stay with zero
	// connected buyers for 5 min, exit so systemd restarts us on a fresh random
	// port (see the seller random-port logic in neuron-sdk.go init).
	commonlib.StartSellerDialWatchdog(5 * time.Minute)

	buyerBuffers := commonlib.NodeBuffersInstance
	if buyerBuffers == nil {
		buyerBuffers = commonlib.NewNodeBuffers()
	}

	// Keep serving buyers from the on-disk known-buyers cache: re-dial any
	// servable buyer that isn't connected (from its remembered address, no fresh
	// topic request needed) and drop buyers whose 5-day lease has expired. This is
	// the seller's Hedera-independent serve loop.
	startSellerAutonomousReconnect(ctx, p2pHost, protocol, buyerBuffers)

	// for each connected buyer send out invoices
	go func() {
		for {
			for peerID, bufferInfo := range buyerBuffers.GetBufferMap() {

				// skip if not connected
				if bufferInfo.LibP2PState != neuronbuffers.Connected || !bufferInfo.IsOtherSideValidAccount {
					continue
				}

				var requestMsgFromOtherSide commonlib.NeuronServiceRequestMsg
				switch message := bufferInfo.RequestOrResponse.Message.(type) {
				case *commonlib.NeuronServiceRequestMsg:
					// Handle the case where the Message is already a NeuronServiceRequestMsg
					fmt.Println("Message is already a NeuronServiceRequestMsg:", message)
					requestMsgFromOtherSide = *message
				case map[string]interface{}:
					// Handle the case where the Message is a map[string]interface{}
					fmt.Println("Message is a map[string]interface{}:", message)
					requestMsgFromOtherSide := commonlib.NeuronServiceRequestMsg{}
					messageBytes, err := json.Marshal(message)
					if err != nil {
						log.Printf("invoice loop: skipping buyer, cannot marshal request message: %v", err)
						continue
					}
					err = json.Unmarshal(messageBytes, &requestMsgFromOtherSide)
					if err != nil {
						log.Printf("invoice loop: skipping buyer, cannot unmarshal request message: %v", err)
						continue
					}
					fmt.Println("Successfully converted map to NeuronServiceRequestMsg:", requestMsgFromOtherSide)
				default:
					// Handle unexpected types
					log.Printf("Unexpected Message type: %T; the raw message was %s", message, message)
				}
				fmt.Println("Send invoice to: ", peerID)
				sharedAccID, err := hedera.AccountIDFromString(fmt.Sprintf("0.0.%d", requestMsgFromOtherSide.SharedAccID))

				if err != nil {
					log.Printf("invoice loop: skipping buyer %s, bad shared account id %d: %v", peerID, requestMsgFromOtherSide.SharedAccID, err)
					continue
				}

				myDeviceAccountID, err := hedera.AccountIDFromEvmAddress(0, 0, os.Getenv("hedera_evm_id"))
				if err != nil {
					log.Printf("invoice loop: skipping buyer %s, bad device evm id %q: %v", peerID, os.Getenv("hedera_evm_id"), err)
					continue
				}

				// GetDeviceParent hits the Hedera mirror REST API; a transient mirror
				// outage must not crash the seller — skip this buyer and retry next cycle.
				myParrentAccountID, err := hedera_helper.GetDeviceParent(os.Getenv("hedera_evm_id"))
				if err != nil {
					log.Printf("invoice loop: skipping buyer %s, mirror lookup of device parent failed: %v", peerID, err)
					continue
				}

				buyerStdIn := hedera.TopicID{
					Shard: 0,
					Realm: 0,
					Topic: requestMsgFromOtherSide.StdInTopic,
				}
				err2 := hedera_helper.SellerSendScheduledTransferRequest(sharedAccID, myParrentAccountID, myDeviceAccountID, buyerStdIn)

				if err2 != nil {
					log.Printf("invoice send failed for %s: %v", peerID, err2)
					continue
				}
				// Lease "real keepalive": the buyer paying invoices / topping up the
				// shared account moves its balance. Detect that and renew the buyer's
				// lease. Best-effort + mirror-dependent; during an outage we simply
				// don't renew (the seller keeps serving regardless; only ~5 days of
				// total silence gives up).
				renewLeaseOnBalanceMovement(requestMsgFromOtherSide.EthPublicKey, requestMsgFromOtherSide.SharedAccID)
				// Avoid blasting Hedera with burst writes when many buyers are connected.
				time.Sleep(600 * time.Millisecond)

			}
			time.Sleep(45 * time.Minute)
		}
	}()

	go sellerCase(ctx, p2pHost, buyerBuffers)

	go hedera_helper.ListenToTopicAndCallBack(commonlib.MyStdIn,

		func(message hedera.TopicMessage) {

			fmt.Printf("request from other side: %s ", message.Contents)

			//lastStdInTimestamp := message.ConsensusTimestamp.Format(time.RFC3339Nano)

			//commonlib.UpdateEnvVariable("last_stdin_timestamp", lastStdInTimestamp, commonlib.MyEnvFile)

			// TODO: check if the message sender exists on the network
			validatorLib.IsRequestPermitted()
			if !validatorLib.IsRequestPermitted() {
				log.Println("NACK: Ignore message as it is not permitted") // TODO: send to the other side
				return
			}

			messageType, ok := commonlib.CheckMessageType(message.Contents)
			if !ok {
				log.Println("NACK: Ignore message as it doesn't parse") // TODO: send to the other side
				fmt.Println(message.Contents)
				hedera_helper.SendSelfErrorMessage(commonlib.BadMessageError, "Error un-marshalling messa", commonlib.StopSending)
				return

			}

			switch messageType {
			case "serviceRequest":
				reqStart := time.Now().UTC()
				requestMsgFromOtherSide := new(commonlib.NeuronServiceRequestMsg)
				err := json.Unmarshal(message.Contents, &requestMsgFromOtherSide)
				if err != nil {
					log.Println("NACK: Ignore message as it doesn't parse") // TODO: send to the other side
					// TODO: send to the other side
					hedera_helper.SendSelfErrorMessage(commonlib.BadMessageError, fmt.Sprintf("Error un-marshalling message: %v from %s", message.Contents, message.TransactionID.AccountID), commonlib.StopSending)
					return
				}

				otherSideStdIn := hedera.TopicID{
					Shard: 0,
					Realm: 0,
					Topic: requestMsgFromOtherSide.StdInTopic,
				}
				log.Printf("[TRACE SELLER REQUEST] received serviceRequest tx=%s consensus=%s buyer_pub=%s shared_acc=%d",
					message.TransactionID,
					message.ConsensusTimestamp,
					requestMsgFromOtherSide.PublicKey,
					requestMsgFromOtherSide.SharedAccID,
				)

				if requestMsgFromOtherSide.Version != "0.4" {
					fmt.Println("NACK: Ignore message as it does not match the current version", requestMsgFromOtherSide.Version) // TODO: send to the other side
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.VersionError, "I am ignoring your message because it's not matching the current version", commonlib.Upgrade)
					return
				}

				buyerSharedAccountInfo, err := hedera_helper.GetAccountInfoFromNetwork(
					hedera.AccountID{
						Shard:   0,
						Realm:   0,
						Account: requestMsgFromOtherSide.SharedAccID,
					})

				if err != nil || buyerSharedAccountInfo.AccountID.IsZero() {
					fmt.Println("That buyer either doesn't exist on the hedera network or hedera struggles to fetch him:", err)
					return
				}
				log.Printf("[TRACE SELLER REQUEST] buyer account info ok tx=%s elapsed=%v account=%s",
					message.TransactionID,
					time.Since(reqStart).Round(time.Millisecond),
					buyerSharedAccountInfo.AccountID,
				)

				// TODO: check what it says in the SLA
				validatorLib.IsRequestPermitted()
				if !validatorLib.IsRequestPermitted() {
					log.Println("NACK: Ignore message as it is not match the SLA") // TODO: send to the other side
					return
				}

				if buyerSharedAccountInfo.Balance.AsTinybar() < 10_000_000 { // 0.1 HBAR threshold
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.BalanceError, "Your balance is too low, but I will serve you anyway", commonlib.DoNothing)
				}

				otherPublicKey := requestMsgFromOtherSide.PublicKey
				otherPeerID := keylib.ConvertHederaPublicKeyToPeerID(otherPublicKey)
				decryptedIpAddress, decodeErr := keylib.DecryptFromOtherside(requestMsgFromOtherSide.EncryptedIpAddress, os.Getenv("private_key"), otherPublicKey)
				if decodeErr != nil {
					log.Println("NACK: error decrypting address", decodeErr) // TODO: send to the other side
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.IpDecryptionError, "I am ignoring your message because I can't figure out who to dial", commonlib.SendFreshHederaRequest)
					return
				}
				fmt.Printf("decrypted multi address, %s\n", decryptedIpAddress)

				// The buyer advertises its whole reachable-address list (quic + tcp).
				// Collect ALL of them into one AddrInfo and let libp2p's dial ranker
				// pick: it prefers QUIC (UDP) and falls back to TCP, so a seller whose
				// UDP path to this buyer is blocked/asymmetric still connects over TCP.
				// `--force-protocol=tcp` restricts to TCP only (for testing/forcing);
				// the default ("udp") keeps all addrs with QUIC preferred.
				trimmed := strings.Trim(string(decryptedIpAddress), "[]")
				addrStrs := strings.Fields(trimmed)
				forceTCP := strings.EqualFold(strings.TrimSpace(*flags.ForceProtocolFlag), "tcp")
				pid, pidErr := peer.Decode(otherPeerID)
				if pidErr != nil {
					log.Printf("NACK: cannot decode buyer peer id %s: %v", otherPeerID, pidErr)
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.IpDecryptionError, "I cannot decode your peer id", commonlib.SendFreshHederaRequest)
					return
				}
				var maddrs []multiaddr.Multiaddr
				for _, str := range addrStrs {
					if forceTCP && !strings.Contains(str, "/tcp/") {
						continue
					}
					if m, e := multiaddr.NewMultiaddr(str); e == nil {
						maddrs = append(maddrs, m)
					}
				}
				if len(maddrs) == 0 {
					log.Printf("NACK: no dialable address for buyer %s in %q (forceTCP=%v)", otherPeerID, trimmed, forceTCP)
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.IpDecryptionError, "I found no dialable address in your request", commonlib.SendFreshHederaRequest)
					return
				}
				addrInfo := &peer.AddrInfo{ID: pid, Addrs: maddrs}

				log.Printf("[TRACE SELLER REQUEST] dialing buyer tx=%s buyer_peer=%s addrs=%v elapsed=%v",
					message.TransactionID,
					addrInfo.ID,
					addrInfo.Addrs,
					time.Since(reqStart).Round(time.Millisecond),
				)

				// Register the public key -> peer ID mapping for log correlation
				commonlib.RegisterPeerPublicKey(addrInfo.ID, otherPublicKey)

				commonlib.NoteSellerDialAttempt() // feed the seller dial watchdog
				initiationError := commonlib.InitialConnect(ctx, p2pHost, *addrInfo, buyerBuffers, protocol)
				if initiationError != nil {
					log.Printf("[TRACE SELLER REQUEST] InitialConnect failed tx=%s buyer_peer=%s elapsed=%v err=%v",
						message.TransactionID,
						addrInfo.ID,
						time.Since(reqStart).Round(time.Millisecond),
						initiationError,
					)
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.DialError, fmt.Sprintf("I tried to initialise a connection but got this error: %v", initiationError.Error()), commonlib.PunchMe)
				} else {
					log.Printf("[TRACE SELLER REQUEST] InitialConnect succeeded tx=%s buyer_peer=%s elapsed=%v",
						message.TransactionID,
						addrInfo.ID,
						time.Since(reqStart).Round(time.Millisecond),
					)
					// Persist this buyer so we can keep serving / re-dial it later
					// without a fresh topic request (survives reboots + Hedera outages).
					persistKnownBuyer(requestMsgFromOtherSide, addrInfo)
				}

				envelope := commonlib.TopicPostalEnvelope{
					OtherStdInTopic: otherSideStdIn,
					Message:         requestMsgFromOtherSide,
				}
				buyerBuffers.SetLastOtherSideMultiAddress(addrInfo.ID, addrInfo.Addrs[0])
				buyerBuffers.SetNeuronSellerRequest(addrInfo.ID, envelope)
			case "peerError": // error from buyer

				buyerError := new(commonlib.NeuronPeerErrorMsg)
				err := json.Unmarshal(message.Contents, &buyerError)
				if err != nil {
					fmt.Println("Error un marshalling message service response message", err)
					return
				}
				switch buyerError.ErrorType {
				case commonlib.ServiceError:
					// Buyer says they're not getting data - check our connection and try to reconnect
					if buyerError.PublicKey == "" {
						log.Println("ServiceError received but no public key provided")
						return
					}

					otherPeerIDStr := keylib.ConvertHederaPublicKeyToPeerID(buyerError.PublicKey)
					otherPeerID, decodeErr := peer.Decode(otherPeerIDStr)
					if decodeErr != nil {
						log.Printf("Could not decode peer ID from public key: %v", decodeErr)
						return
					}

					// Register public key for log correlation
					commonlib.RegisterPeerPublicKey(otherPeerID, buyerError.PublicKey)

					bufferInfo, exists := buyerBuffers.GetBuffer(otherPeerID)
					if !exists {
						log.Printf("Received ServiceError from unknown peer %s - they need to send a fresh service request", otherPeerID.ShortString())
						return
					}

					// Mark as needing reconnection and attempt it
					log.Printf("🔄 Buyer %s reports no goods received - checking connection and attempting reconnect", otherPeerID.ShortString())
					buyerBuffers.UpdateBufferLibP2PState(otherPeerID, commonlib.Reconnecting)

					reconnectErr := commonlib.ReconnectPeersIfNeeded(ctx, p2pHost, otherPeerID, bufferInfo, buyerBuffers, protocol)
					if reconnectErr != nil {
						log.Printf("Reconnect attempt for %s after ServiceError: %v", otherPeerID.ShortString(), reconnectErr)
					} else {
						log.Printf("✓ Reconnected to %s after ServiceError", otherPeerID.ShortString())
					}

				case commonlib.DialError:
				case commonlib.FlushError:
				case commonlib.DisconnectedError:
				case commonlib.NoKnownAddressError:
				case commonlib.HeartBeatError:
				case commonlib.BalanceError:
				case commonlib.VersionError:
				case commonlib.WriteError:
				case commonlib.StreamError:
				case commonlib.IpDecryptionError:
				default:
					fmt.Println("Unknown error type: ", buyerError.ErrorType)
					//TODO: penalize message sender.
					hedera_helper.SendSelfErrorMessage(commonlib.BadMessageError, "I received a message that I don't understand", commonlib.StopSending)
					return
				}
			default:
				fmt.Println("Forwarding message to dapp:", messageType)
				sellerCaseTopicCallBack(message)

			}

		})
}

// persistKnownBuyer records a buyer (its libp2p addresses, topic and shared
// account) so the seller can keep serving it across reboots and Hedera outages
// without waiting for a fresh topic serviceRequest. Best-effort; a closed cache
// just means we fall back to the old in-memory-only behaviour.
func persistKnownBuyer(req *commonlib.NeuronServiceRequestMsg, addrInfo *peer.AddrInfo) {
	if req == nil || addrInfo == nil || req.EthPublicKey == "" {
		return
	}
	maddrs := make([]string, 0, len(addrInfo.Addrs))
	for _, a := range addrInfo.Addrs {
		maddrs = append(maddrs, a.String())
	}
	rec := &commonlib.KnownBuyer{
		BuyerEthAddress: req.EthPublicKey,
		BuyerPublicKey:  req.PublicKey,
		Multiaddrs:      maddrs,
		BuyerStdInTopic: req.StdInTopic,
		SharedAccID:     req.SharedAccID,
		Serve:           true,
		LastSignOfLife:  time.Now(),
	}
	// Carry forward the last observed balance (and CreatedAt) so the lease-renewal
	// baseline survives a re-request.
	if prev, err := commonlib.LoadKnownBuyer(req.EthPublicKey); err == nil {
		rec.LastBalanceTiny = prev.LastBalanceTiny
		rec.CreatedAt = prev.CreatedAt
	}
	if err := commonlib.SaveKnownBuyer(rec); err != nil {
		if err != commonlib.ErrDatabaseNotOpen {
			log.Printf("could not persist known buyer %s: %v", req.EthPublicKey, err)
		}
	} else {
		log.Printf("[knownbuyers] cached buyer %s addrs=%v", req.EthPublicKey, maddrs)
	}
}

// renewLeaseOnBalanceMovement renews a buyer's lease when its shared-account
// balance has moved since we last looked — that movement is the buyer paying an
// invoice or topping up, i.e. the real economic keepalive. Best-effort and
// mirror-dependent: on any error (incl. a mirror/Hedera outage) we leave the
// lease as-is and keep serving.
func renewLeaseOnBalanceMovement(buyerEth string, sharedAccID uint64) {
	if buyerEth == "" || sharedAccID == 0 {
		return
	}
	known, err := commonlib.LoadKnownBuyer(buyerEth)
	if err != nil {
		return // not persisted yet; the connect path will persist it
	}
	acc := hedera.AccountID{Shard: 0, Realm: 0, Account: sharedAccID}
	info, err := hedera_helper.GetAccountInfoFromMirror(acc)
	if err != nil {
		return // mirror unreachable: don't renew, keep serving
	}
	bal := int64(info.Balance)
	if known.LastBalanceTiny == bal {
		return // no movement, nothing to renew
	}
	known.LastBalanceTiny = bal
	known.Serve = true
	known.LastSignOfLife = time.Now()
	if err := commonlib.SaveKnownBuyer(known); err != nil && err != commonlib.ErrDatabaseNotOpen {
		log.Printf("lease renew: could not save buyer %s: %v", buyerEth, err)
	}
}

// startSellerAutonomousReconnect launches the seller's Hedera-independent serve
// loop. Every sweep it: (1) drops buyers whose 5-day lease has expired, and
// (2) re-dials every still-leased buyer that isn't currently connected, using the
// buyer's remembered libp2p address from the known-buyers cache — so a buyer
// keeps getting data across stream breaks and Hedera/mirror outages without
// having to send a fresh topic serviceRequest. Per-buyer exponential backoff
// avoids hammering an unreachable buyer.
func startSellerAutonomousReconnect(ctx context.Context, p2pHost host.Host, protocol protocol.ID, buyerBuffers *commonlib.NodeBuffers) {
	const sweepInterval = 30 * time.Second
	const baseBackoff = 30 * time.Second
	const maxBackoff = 10 * time.Minute
	nextAttempt := make(map[string]time.Time)
	attempts := make(map[string]int)

	go func() {
		for {
			// (1) GC buyers whose lease expired (5 days with no sign of life).
			if expired, err := commonlib.GCExpiredKnownBuyers(); err == nil {
				for _, kb := range expired {
					if pid, derr := buyerPeerID(kb.BuyerPublicKey); derr == nil {
						buyerBuffers.RemoveBuffer(pid)
					}
					delete(nextAttempt, kb.BuyerEthAddress)
					delete(attempts, kb.BuyerEthAddress)
					log.Printf("known buyer %s lease expired (5d silent) — no longer serving", kb.BuyerEthAddress)
				}
			}

			// (2) Ensure every still-leased buyer has a live stream.
			servable, err := commonlib.ListServableBuyers()
			if err != nil {
				time.Sleep(sweepInterval)
				continue
			}
			now := time.Now()
			connected := make([]string, 0, len(servable))
			redialing := make([]string, 0)
			backingOff := 0
			for _, kb := range servable {
				pid, derr := buyerPeerID(kb.BuyerPublicKey)
				if derr != nil {
					continue
				}
				if isBuyerConnected(p2pHost, pid, buyerBuffers) {
					delete(attempts, kb.BuyerEthAddress) // healthy: reset backoff
					delete(nextAttempt, kb.BuyerEthAddress)
					connected = append(connected, kb.BuyerEthAddress)
					continue
				}
				if t, ok := nextAttempt[kb.BuyerEthAddress]; ok && now.Before(t) {
					backingOff++
					continue // still backing off
				}
				redialing = append(redialing, kb.BuyerEthAddress)
				if rerr := redialCachedBuyer(ctx, p2pHost, protocol, kb, pid, buyerBuffers); rerr != nil {
					n := attempts[kb.BuyerEthAddress] + 1
					attempts[kb.BuyerEthAddress] = n
					shift := uint(n - 1)
					if shift > 5 {
						shift = 5
					}
					backoff := baseBackoff << shift
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					nextAttempt[kb.BuyerEthAddress] = now.Add(backoff)
					log.Printf("autonomous re-dial of cached buyer %s failed (attempt %d, next in %s): %v", kb.BuyerEthAddress, n, backoff, rerr)
				} else {
					delete(attempts, kb.BuyerEthAddress)
					delete(nextAttempt, kb.BuyerEthAddress)
					log.Printf("autonomous re-dial of cached buyer %s succeeded", kb.BuyerEthAddress)
				}
			}
			// Heartbeat so the loop's decisions are visible even when it takes no
			// action (otherwise it is silent and we cannot tell it is alive).
			log.Printf("[knownbuyers] sweep: servable=%d connected=%d %v redialing=%d %v backingOff=%d",
				len(servable), len(connected), connected, len(redialing), redialing, backingOff)
			time.Sleep(sweepInterval)
		}
	}()
}

// buyerPeerID derives a buyer's libp2p peer ID from its hedera public key.
func buyerPeerID(buyerPublicKey string) (peer.ID, error) {
	if buyerPublicKey == "" {
		return "", fmt.Errorf("empty buyer public key")
	}
	return peer.Decode(keylib.ConvertHederaPublicKeyToPeerID(buyerPublicKey))
}

// isBuyerConnected reports whether we currently hold a live stream to the buyer.
func isBuyerConnected(p2pHost host.Host, pid peer.ID, buffers *commonlib.NodeBuffers) bool {
	if p2pHost.Network().Connectedness(pid) != network.Connected {
		return false
	}
	info, ok := buffers.GetBuffer(pid)
	if !ok || info.Writer == nil {
		return false
	}
	conn := info.Writer.Conn()
	return conn != nil && !conn.IsClosed()
}

// redialCachedBuyer reconstructs the buyer's address and invoice envelope from
// the known-buyers cache and (re-)establishes the ADS-B stream. Seeding the
// envelope first (when the buffer has none — e.g. a fresh boot) means AddBuffer3
// inside InitialConnect preserves it, so the invoice loop keeps billing a
// re-dialed/boot-restored buyer.
func redialCachedBuyer(ctx context.Context, p2pHost host.Host, protocol protocol.ID, kb commonlib.KnownBuyer, pid peer.ID, buffers *commonlib.NodeBuffers) error {
	var maddrs []multiaddr.Multiaddr
	for _, s := range kb.Multiaddrs {
		if m, e := multiaddr.NewMultiaddr(s); e == nil {
			maddrs = append(maddrs, m)
		}
	}
	if len(maddrs) == 0 {
		return fmt.Errorf("no usable cached addresses for buyer %s", kb.BuyerEthAddress)
	}

	// Seed the buffer with the reconstructed request envelope if it lacks one, so
	// the invoice loop can bill this buyer after a cache-driven (re)connect.
	if info, ok := buffers.GetBuffer(pid); !ok || info.RequestOrResponse.Message == nil {
		req := &commonlib.NeuronServiceRequestMsg{
			MessageType:  "serviceRequest",
			ServiceType:  string(commonlib.MyProtocol),
			SharedAccID:  kb.SharedAccID,
			EthPublicKey: kb.BuyerEthAddress,
			PublicKey:    kb.BuyerPublicKey,
			StdInTopic:   kb.BuyerStdInTopic,
			Version:      "0.4",
		}
		envelope := commonlib.TopicPostalEnvelope{
			OtherStdInTopic: hedera.TopicID{Shard: 0, Realm: 0, Topic: kb.BuyerStdInTopic},
			Message:         req,
		}
		buffers.AddBuffer2(pid, envelope, true, commonlib.SendOK, commonlib.Reconnecting)
		buffers.SetPeerPublicKey(pid, kb.BuyerPublicKey)
		buffers.SetPeerEvmAddress(pid, kb.BuyerEthAddress)
	}

	// Make sure libp2p knows the buyer's address before we dial.
	p2pHost.Peerstore().AddAddrs(pid, maddrs, time.Hour)
	addrInfo := peer.AddrInfo{ID: pid, Addrs: maddrs}
	return commonlib.InitialConnect(ctx, p2pHost, addrInfo, buffers, protocol)
}
