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
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
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
						log.Panic("Error marshaling message: ", err)
					}
					err = json.Unmarshal(messageBytes, &requestMsgFromOtherSide)
					if err != nil {
						log.Panic("Error unmarshaling message to NeuronServiceRequestMsg: ", err)
					}
					fmt.Println("Successfully converted map to NeuronServiceRequestMsg:", requestMsgFromOtherSide)
				default:
					// Handle unexpected types
					log.Printf("Unexpected Message type: %T; the raw message was %s", message, message)
				}
				fmt.Println("Send invoice to: ", peerID)
				sharedAccID, err := hedera.AccountIDFromString(fmt.Sprintf("0.0.%d", requestMsgFromOtherSide.SharedAccID))

				if err != nil {
					log.Panic(err)
				}

				myDeviceAccountID, err := hedera.AccountIDFromEvmAddress(0, 0, os.Getenv("hedera_evm_id"))
				if err != nil {
					log.Panic(err)
				}

				myParrentAccountID, err := hedera_helper.GetDeviceParent(os.Getenv("hedera_evm_id"))
				if err != nil {
					log.Panic(err)
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
					log.Panic(decodeErr)
				}
				log.Printf("[TRACE SELLER REQUEST] dialing buyer tx=%s buyer_peer=%s addr=%s elapsed=%v",
					message.TransactionID,
					addrInfo.ID,
					pidStr,
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
