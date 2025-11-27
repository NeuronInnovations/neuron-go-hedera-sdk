package streambuyervsseller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	flags "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	neuronbuffers "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	hedera_helper "github.com/NeuronInnovations/neuron-go-hedera-sdk/hedera"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	validatorLib "github.com/NeuronInnovations/neuron-go-hedera-sdk/validator-lib"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func HandleSellerCase(ctx context.Context, p2pHost host.Host, protocol protocol.ID, sellerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers), sellerCaseTopicCallBack func(topicMessage hedera.TopicMessage)) {
	fmt.Println("Acting as a data seller (I'll be waiting on my topic for requests and serve them on the stream)")

	buyerBuffers := commonlib.NodeBuffersInstance
	if buyerBuffers == nil {
		buyerBuffers = commonlib.NewNodeBuffers()
	}

	// IMPORTANT: Try BBolt-first reconnection BEFORE listening to Hedera topics
	// This prioritizes cached IPs and SharedAccIDs to avoid unnecessary blockchain queries
	if commonlib.GlobalStateManager != nil && !commonlib.GlobalStateManager.IsInDegradedMode() {
		log.Println("🚀 [Seller] Starting BBolt-first reconnection (prioritizing cached buyer IPs over Hedera)")
		sellerTryReconnectFromBBolt(ctx, p2pHost, buyerBuffers, protocol)
		// Give BBolt reconnection attempts a moment to establish connections
		time.Sleep(2 * time.Second)
	} else {
		log.Println("⚠️ [Seller] BBolt unavailable or in degraded mode, will rely on Hedera for all connections")
	}

	/*
		// Periodically check for active streams and update buffer writers
		go func() {
			for {
				time.Sleep(5 * time.Second) // Check every 5 seconds
				// Get all active connections
				for peerID, bufferInfo := range buyerBuffers.GetBufferMap() {
					// Skip if already connected and has a valid stream
					if bufferInfo.LibP2PState == types.Connected &&
						bufferInfo.StreamHandler != nil &&
						!network.Stream.Conn(*bufferInfo.StreamHandler).IsClosed() {
						continue
					}

					// Check active connections for this peer
					activeConns := p2pHost.Network().ConnsToPeer(peerID)
					for _, conn := range activeConns {
						for _, stream := range conn.GetStreams() {
							if stream.Protocol() == protocol {
								// Found a matching stream, update the buffer
								if existingBuffer, exists := buyerBuffers.GetBuffer(peerID); exists {
									existingBuffer.Writer = stream
									existingBuffer.StreamHandler = &stream
									existingBuffer.LibP2PState = types.Connected
									log.Printf("Updated stream handler for peer: %s", peerID)
								} else {
									log.Println("the stream protocol is not the same as the protocol we are listening to", stream.Protocol(), protocol)
								}
								break
							}
						}
					}
				}
			}
		}()
	*/
	// for each connected buyer send out invoices
	go func() {
		for {
			for peerID, bufferInfo := range buyerBuffers.GetBufferMap() {

				// skip if not connected
				if bufferInfo.LibP2PState != types.Connected || !bufferInfo.IsOtherSideValidAccount {
					continue
				}

				var requestMsgFromOtherSide types.NeuronServiceRequestMsg
				var parseError error
				switch message := bufferInfo.RequestOrResponse.Message.(type) {
				case *types.NeuronServiceRequestMsg:
					// Convert types.NeuronServiceRequestMsg to commonlib.NeuronServiceRequestMsg
					msgBytes, _ := json.Marshal(message)
					json.Unmarshal(msgBytes, &requestMsgFromOtherSide)
				case map[string]interface{}:
					// Handle the case where the Message is a map[string]interface{}
					fmt.Println("Message is a map[string]interface{}:", message)
					messageBytes, err := json.Marshal(message)
					if err != nil {
						log.Printf("⚠️ Error marshaling message for peer %s: %v", peerID, err)
						parseError = err
					} else {
						err = json.Unmarshal(messageBytes, &requestMsgFromOtherSide)
						if err != nil {
							log.Printf("⚠️ Error unmarshaling message to NeuronServiceRequestMsg for peer %s: %v", peerID, err)
							parseError = err
						} else {
							fmt.Println("Successfully converted map to NeuronServiceRequestMsg:", requestMsgFromOtherSide)
						}
					}
				default:
					// Handle unexpected types
					log.Printf("⚠️ Unexpected Message type for peer %s: %T; the raw message was %s", peerID, message, message)
					continue
				}

				// Skip if parsing failed
				if parseError != nil {
					continue
				}

				// Validate SharedAccID before using
				if requestMsgFromOtherSide.SharedAccID == 0 {
					log.Printf("⚠️ Invalid SharedAccID (0) for peer %s, skipping invoice", peerID)
					continue
				}

				fmt.Println("Send invoice to: ", peerID)
				sharedAccID, err := hedera.AccountIDFromString(fmt.Sprintf("0.0.%d", requestMsgFromOtherSide.SharedAccID))

				if err != nil {
					log.Printf("⚠️ Invalid SharedAccID format for peer %s: %v", peerID, err)
					continue
				}

				// Validate SharedAccID exists on network before sending invoice
				isValid, validErr := hedera_helper.ValidateSharedAccount(requestMsgFromOtherSide.SharedAccID, 1)
				if !isValid {
					log.Printf("⚠️ SharedAccID %d for peer %s is invalid on network: %v", requestMsgFromOtherSide.SharedAccID, peerID, validErr)
					log.Printf("🔄 Marking peer %s as invalid account - buyer needs to create new shared account", peerID)
					buyerBuffers.UpdateBufferIsValidAccount(peerID, false)
					continue
				}

				myDeviceAccountID, err := hedera.AccountIDFromEvmAddress(0, 0, os.Getenv("hedera_evm_id"))
				if err != nil {
					log.Printf("⚠️ Error getting device account ID: %v", err)
					continue
				}

				myParrentAccountID, err := hedera_helper.GetDeviceParent(os.Getenv("hedera_evm_id"))
				if err != nil {
					log.Printf("⚠️ Error getting parent account ID: %v", err)
					continue
				}

				buyerStdIn := hedera.TopicID{
					Shard: 0,
					Realm: 0,
					Topic: requestMsgFromOtherSide.StdInTopic,
				}
				err2 := hedera_helper.SellerSendScheduledTransferRequest(sharedAccID, myParrentAccountID, myDeviceAccountID, buyerStdIn)

				if err2 != nil {
					log.Printf("⚠️ Error sending scheduled transfer request to peer %s: %v", peerID, err2)
					// Check if it's an account-related error
					if strings.Contains(err2.Error(), "INVALID_ACCOUNT_ID") || strings.Contains(err2.Error(), "ACCOUNT_DELETED") {
						log.Printf("🔄 SharedAccID %d appears invalid, marking peer %s as invalid account", requestMsgFromOtherSide.SharedAccID, peerID)
						buyerBuffers.UpdateBufferIsValidAccount(peerID, false)
					}
					continue
				}

				log.Printf("✅ Successfully sent invoice to peer %s using SharedAccID %d", peerID, requestMsgFromOtherSide.SharedAccID)
			}
			time.Sleep(60 * time.Minute)
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

			messageType, ok := types.CheckMessageType(message.Contents)
			if !ok {
				log.Println("NACK: Ignore message as it doesn't parse") // TODO: send to the other side
				fmt.Println(message.Contents)
				hedera_helper.SendSelfErrorMessage(types.BadMessageError, "Error un-marshalling messa", types.StopSending)
				return
			}

			switch messageType {
			case "serviceRequest":
				requestMsgFromOtherSide := new(types.NeuronServiceRequestMsg)
				err := json.Unmarshal(message.Contents, &requestMsgFromOtherSide)
				if err != nil {
					log.Println("NACK: Ignore message as it doesn't parse") // TODO: send to the other side
					// TODO: send to the other side
					hedera_helper.SendSelfErrorMessage(types.BadMessageError, fmt.Sprintf("Error un-marshalling message: %v from %s", message.Contents, message.TransactionID.AccountID), types.StopSending)
					return
				}

				otherSideStdIn := hedera.TopicID{
					Shard: 0,
					Realm: 0,
					Topic: requestMsgFromOtherSide.StdInTopic,
				}

				if requestMsgFromOtherSide.Version != "0.4" {
					fmt.Println("NACK: Ignore message as it does not match the current version", requestMsgFromOtherSide.Version) // TODO: send to the other side
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.VersionError, "I am ignoring your message because it's not matching the current version", types.Upgrade)
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

				// TODO: check what it says in the SLA
				validatorLib.IsRequestPermitted()
				if !validatorLib.IsRequestPermitted() {
					log.Println("NACK: Ignore message as it is not match the SLA") // TODO: send to the other side
					return
				}

				if buyerSharedAccountInfo.Balance.AsTinybar() < 100 {
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.BalanceError, "Your balance is too low, but I will serve you anyway", types.DoNothing)
				}

				otherPublicKey := requestMsgFromOtherSide.PublicKey
				// Convert other peer's public key to peer ID
				otherPeerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(otherPublicKey)
				if err != nil {
					log.Printf("Error converting other peer's public key to peer ID: %v", err)
					return
				}
				otherPeerID, err := peer.Decode(otherPeerIDStr)
				if err != nil {
					log.Printf("Error decoding other peer's ID: %v", err)
					return
				}
				decryptedIpAddress, decodeErr := keylib.DecryptFromOtherside(requestMsgFromOtherSide.EncryptedIpAddress, os.Getenv("private_key"), otherPublicKey)
				if decodeErr != nil {
					log.Println("NACK: error decrypting address", decodeErr) // TODO: send to the other side
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.IpDecryptionError, "I am ignoring your message because I can't figure out who to dial", types.SendFreshHederaRequest)
					return
				}
				fmt.Printf("decrypted multi address, %s\n", decryptedIpAddress)

				trimmed := strings.Trim(string(decryptedIpAddress), "[]")
				addrStrs := strings.Fields(trimmed)
				var pidStr string
				for _, str := range addrStrs {
					if strings.Contains(str, *flags.ForceProtocolFlag) {
						pidStr = fmt.Sprintf("%s/p2p/%s", str, otherPeerID)
						break
					}
				}
				addrInfo, decodeErr := peer.AddrInfoFromString(pidStr)

				if decodeErr != nil {
					log.Printf("⚠️ Error decoding peer address info from %s: %v", pidStr, decodeErr)
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.IpDecryptionError, fmt.Sprintf("Invalid peer address: %v", decodeErr), types.SendFreshHederaRequest)
					return
				}
				initiationError := commonlib.InitialConnect(ctx, p2pHost, *addrInfo, buyerBuffers, protocol)
				if initiationError != nil {
					// Enhanced error handling: Send punchMeRequest instead of just error
					log.Printf("Direct connection failed for peer %s, initiating hole punching: %v", otherPeerID, initiationError)

					// Send punchMeRequest to initiate hole punching
					punchMeEnvelope, err := preparePunchMeRequest(otherPeerID, otherSideStdIn, p2pHost, message.ConsensusTimestamp)
					if err != nil {
						log.Printf("Failed to prepare punchMeRequest for peer %s: %v", otherPeerID, err)
						// Fallback to original error message
						hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.DialError, fmt.Sprintf("I tried to initialise a connection but got this error: %v", initiationError.Error()), types.PunchMe)
					} else {
						// Send the punchMeRequest
						if err := hedera_helper.SendTransactionEnvelope(punchMeEnvelope); err != nil {
							log.Printf("Failed to send punchMeRequest for peer %s: %v", otherPeerID, err)
							// Fallback to original error message
							hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.DialError, fmt.Sprintf("I tried to initialise a connection but got this error: %v", initiationError.Error()), types.PunchMe)
						} else {
							log.Printf("Successfully sent punchMeRequest to peer %s for hole punching", otherPeerID)
							// Update buffer state to indicate hole punching is in progress
							buyerBuffers.UpdateBufferLibP2PState(otherPeerID, types.HolePunchingScheduled)
						}
					}
				}

				envelope := commonlib.TopicPostalEnvelope{
					OtherStdInTopic: otherSideStdIn,
					Message:         requestMsgFromOtherSide,
				}
				buyerBuffers.SetLastOtherSideMultiAddress(addrInfo.ID, addrInfo.Addrs[0].String())
				buyerBuffers.SetNeuronSellerRequest(addrInfo.ID, types.TopicPostalEnvelope(envelope))

			case "punchMeAcknowledgment":
				// Handle acknowledgment from buyer for hole punching
				handlePunchMeAcknowledgment(message, p2pHost, buyerBuffers, protocol)

			case "peerError": // error from buyer
				buyerError := new(types.NeuronPeerErrorMsg)
				err := json.Unmarshal(message.Contents, &buyerError)
				if err != nil {
					fmt.Println("Error un marshalling message service response message", err)
					return
				}
				switch buyerError.ErrorType {
				case types.DialError:
				case types.FlushError:
				case types.DisconnectedError:
				case types.NoKnownAddressError:
				case types.HeartBeatError:
				case types.BalanceError:
				case types.VersionError:
				case types.WriteError:
				case types.StreamError:
				case types.IpDecryptionError:
				case types.ServiceError:
				default:
					fmt.Println("Unknown error type: ", buyerError.ErrorType)
					//TODO: penalize message sender.
					hedera_helper.SendSelfErrorMessage(types.BadMessageError, "I received a message that I don't understand", types.StopSending)
					return
				}
			default:
				fmt.Println("Forwarding message to dapp:", messageType)
				sellerCaseTopicCallBack(message)

			}

		})
}

// preparePunchMeRequest creates a punchMeRequest message for hole punching coordination
func preparePunchMeRequest(buyerPeerID peer.ID, buyerStdIn hedera.TopicID, p2pHost host.Host, consensusTimestamp time.Time) (types.TopicPostalEnvelope, error) {
	// 1. Get seller's public IP  addresses
	sellerAddresses := p2pHost.Addrs()
	if len(sellerAddresses) == 0 {
		return types.TopicPostalEnvelope{}, fmt.Errorf("no public addresses available for seller")
	}

	// 2. Convert addresses to string format for encryption
	var addrStrings []string
	for _, addr := range sellerAddresses {
		addrStrings = append(addrStrings, addr.String())
	}

	// 3. Get buyer's public key from peer ID
	buyerPublicKey, err := getBuyerPublicKeyFromPeerID(buyerPeerID)
	if err != nil {
		return types.TopicPostalEnvelope{}, fmt.Errorf("failed to get buyer public key: %w", err)
	}

	// 4. Encrypt seller's IP addresses with buyer's public key
	// Convert addrStrings to []byte for encryption
	addrBytes := []byte(strings.Join(addrStrings, " "))
	encryptedIP, err := keylib.EncryptForOtherside(addrBytes, os.Getenv("private_key"), buyerPublicKey)
	if err != nil {
		return types.TopicPostalEnvelope{}, fmt.Errorf("failed to encrypt seller IP addresses: %w", err)
	}

	// 5. Get seller's public key
	sellerPublicKey, err := getSellerPublicKey()
	if err != nil {
		return types.TopicPostalEnvelope{}, fmt.Errorf("failed to get seller public key: %w", err)
	}

	// 6. Create punchMeRequest message
	punchMeMsg := types.NeuronPunchMeRequestMsg{
		MessageType:        "punchMeRequest",
		EncryptedIpAddress: encryptedIP,
		StdInTopic:         buyerStdIn.Topic,
		PublicKey:          sellerPublicKey,
		HederaTimestamp:    consensusTimestamp.Format(time.RFC3339Nano),
		PunchDelay:         10, // Default 10 second delay
		Version:            "0.4",
	}

	log.Printf("Prepared punchMeRequest for peer %s with timestamp %s", buyerPeerID, punchMeMsg.HederaTimestamp)

	return types.TopicPostalEnvelope{
		OtherStdInTopic: buyerStdIn,
		Message:         punchMeMsg,
	}, nil
}

// handlePunchMeAcknowledgment processes the buyer's acknowledgment of a punchMeRequest
func handlePunchMeAcknowledgment(topicMessage hedera.TopicMessage, p2pHost host.Host, buyerBuffers *commonlib.NodeBuffers, protocol protocol.ID) {
	var ackMsg types.NeuronPunchMeAcknowledgmentMsg
	if err := json.Unmarshal(topicMessage.Contents, &ackMsg); err != nil {
		log.Printf("Failed to unmarshal punchMeAcknowledgment: %v", err)
		return
	}

	log.Printf("Received punchMeAcknowledgment from buyer with timestamp %s", ackMsg.HederaTimestamp)

	// Convert buyer's public key to peer ID
	buyerPeerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(ackMsg.PublicKey)
	if err != nil {
		log.Printf("Error converting buyer public key to peer ID: %v", err)
		return
	}
	buyerPeerID, err := peer.Decode(buyerPeerIDStr)
	if err != nil {
		log.Printf("Error decoding buyer peer ID: %v", err)
		return
	}

	// Update buffer state to indicate hole punching is acknowledged
	buyerBuffers.UpdateBufferLibP2PState(buyerPeerID, types.HolePunchingInProgress)

	// Schedule synchronized hole punching
	scheduleSellerHolePunching(ackMsg.HederaTimestamp, 10, buyerPeerID, p2pHost, protocol, buyerBuffers)
}

// scheduleSellerHolePunching schedules the seller's side of synchronized hole punching
func scheduleSellerHolePunching(hederaTimestamp string, delaySeconds int, buyerPeerID peer.ID, p2pHost host.Host, protocol protocol.ID, buyerBuffers *commonlib.NodeBuffers) {
	// 1. Parse Hedera timestamp
	consensusTime, err := time.Parse(time.RFC3339Nano, hederaTimestamp)
	if err != nil {
		log.Printf("Failed to parse Hedera timestamp: %v", err)
		return
	}

	// 2. Calculate punch time (consensus time + delay)
	punchTime := consensusTime.Add(time.Duration(delaySeconds) * time.Second)

	log.Printf("Scheduling seller hole punching for peer %s at %s (delay: %ds)", buyerPeerID, punchTime, delaySeconds)

	// 3. Schedule the hole punching
	go func() {
		time.Sleep(time.Until(punchTime))

		// 4. Perform hole punching
		performSellerHolePunching(buyerPeerID, p2pHost, protocol, buyerBuffers)
	}()
}

// performSellerHolePunching performs the actual hole punching attempt from seller side
func performSellerHolePunching(buyerPeerID peer.ID, p2pHost host.Host, protocol protocol.ID, buyerBuffers *commonlib.NodeBuffers) {
	log.Printf("Performing seller hole punching with peer %s", buyerPeerID)

	// Get buyer's address info from buffer
	buffer, exists := buyerBuffers.GetBuffer(buyerPeerID)
	if !exists {
		log.Printf("No buffer found for peer %s during hole punching", buyerPeerID)
		return
	}

	// Use libp2p's hole punching capabilities
	ctx := context.Background()
	holePunchCtx := network.WithSimultaneousConnect(ctx, false, "hole-punching") // false for seller (not client)
	forceDirectConnCtx := network.WithForceDirectDial(holePunchCtx, "hole-punching")

	// Attempt connection with hole punching
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, 30*time.Second)
	defer cancel()

	// Create addrInfo from the stored address
	addrInfo, err := peer.AddrInfoFromString(buffer.LastOtherSideMultiAddress)
	if err != nil {
		log.Printf("Failed to parse buyer address for hole punching: %v", err)
		return
	}

	if err := p2pHost.Connect(dialCtx, *addrInfo); err != nil {
		log.Printf("Seller hole punching failed for peer %s: %v", buyerPeerID, err)
		buyerBuffers.UpdateBufferLibP2PState(buyerPeerID, types.CanNotConnectUnknownReason)
		return
	}

	log.Printf("Seller hole punching successful with peer %s", buyerPeerID)
	buyerBuffers.UpdateBufferLibP2PState(buyerPeerID, types.HolePunchingCompleted)

	// Try to create stream if connection successful
	stream, err := p2pHost.NewStream(dialCtx, buyerPeerID, protocol)
	if err != nil {
		log.Printf("Failed to create stream after seller hole punching: %v", err)
		return
	}

	log.Printf("Stream created successfully after seller hole punching: %v", stream)
	buyerBuffers.UpdateBufferLibP2PState(buyerPeerID, types.Connected)
}

// Helper functions

// getBuyerPublicKeyFromPeerID extracts the public key from a peer ID
func getBuyerPublicKeyFromPeerID(peerID peer.ID) (string, error) {
	pubKey, err := peerID.ExtractPublicKey()
	if err != nil {
		return "", fmt.Errorf("failed to extract public key from peer ID: %w", err)
	}

	pubKeyBytes, err := pubKey.Raw()
	if err != nil {
		return "", fmt.Errorf("failed to get raw public key bytes: %w", err)
	}

	return fmt.Sprintf("%x", pubKeyBytes), nil
}

// getSellerPublicKey gets the seller's public key from environment
func getSellerPublicKey() (string, error) {
	return commonlib.MyPublicKey.StringRaw(), nil
}

// sellerTryReconnectFromBBolt attempts to connect to buyers using persisted IPs from BBolt database
// This function is called on startup to avoid unnecessary Hedera topic queries and prioritize cached connections
// It mirrors the buyer's tryReconnectFromBBolt but for the seller context
func sellerTryReconnectFromBBolt(ctx context.Context, p2pHost host.Host, buyerBuffers *commonlib.NodeBuffers, protocol protocol.ID) {
	// Use GetBufferMap which is thread-safe and returns a copy
	buffersCopy := buyerBuffers.GetBufferMap()

	if len(buffersCopy) == 0 {
		log.Println("💾 [Seller] No persisted buyers found in BBolt to reconnect to")
		return
	}

	log.Printf("💾 [Seller] Attempting BBolt-first reconnection to %d persisted buyers", len(buffersCopy))

	for buyerPeerID, buffer := range buffersCopy {
		// Skip if we don't have persisted IPs
		if buffer.LastOtherSideMultiAddress == "" {
			log.Printf("⏭️ [Seller] No persisted IPs for buyer %s, will rely on Hedera", buyerPeerID.ShortString())
			continue
		}

		// Skip if we don't have a valid SharedAccID (indicates no previous successful connection)
		if buffer.SharedAccID == 0 {
			log.Printf("⏭️ [Seller] No SharedAccID for buyer %s, will rely on Hedera", buyerPeerID.ShortString())
			continue
		}

		log.Printf("🔄 [Seller] Attempting BBolt-first reconnection to buyer %s using persisted IPs: %s (SharedAccID: %d)",
			buyerPeerID.ShortString(), buffer.LastOtherSideMultiAddress, buffer.SharedAccID)

		// Attempt direct connection using persisted IPs (in goroutine to avoid blocking)
		go func(peerID peer.ID, cachedAddr string, sharedAccID uint64) {
			// Use the unified AttemptReconnectFromCache function
			err := commonlib.AttemptReconnectFromCache(ctx, p2pHost, peerID, buyerBuffers, protocol)

			if err == nil {
				log.Printf("✅ [Seller] BBolt-first reconnection SUCCESS! Connected to buyer %s (SharedAccID: %d)",
					peerID.ShortString(), sharedAccID)

				// Update buffer state
				buyerBuffers.UpdateBufferLibP2PState(peerID, types.Connected)

				// Try to open stream to the buyer
				stream, streamErr := p2pHost.NewStream(context.Background(), peerID, protocol)
				if streamErr == nil {
					log.Printf("✅ [Seller] Stream opened successfully to buyer %s via BBolt-cached IP", peerID.ShortString())
					// Keep the stream open - seller uses it for data transmission
					_ = stream // Seller's data transmission logic will use this
				} else {
					log.Printf("⚠️ [Seller] Stream opening failed to buyer %s: %v", peerID.ShortString(), streamErr)
				}
			} else {
				log.Printf("⚠️ [Seller] BBolt reconnection failed for buyer %s: %v, will rely on Hedera messages",
					peerID.ShortString(), err)
			}
		}(buyerPeerID, buffer.LastOtherSideMultiAddress, buffer.SharedAccID)
	}
}
