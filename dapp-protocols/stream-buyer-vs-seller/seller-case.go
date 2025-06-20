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
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func HandleSellerCase(ctx context.Context, p2pHost host.Host, protocol protocol.ID, sellerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers), sellerCaseTopicCallBack func(topicMessage hedera.TopicMessage)) {
	fmt.Println("Acting as a data seller (I'll be waiting on my topic for requests and serve them on the stream)")

	buyerBuffers := commonlib.NodeBuffersInstance
	if buyerBuffers == nil {
		buyerBuffers = commonlib.NewNodeBuffers()
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
				switch message := bufferInfo.RequestOrResponse.Message.(type) {
				case *types.NeuronServiceRequestMsg:
					// Convert types.NeuronServiceRequestMsg to commonlib.NeuronServiceRequestMsg
					msgBytes, _ := json.Marshal(message)
					json.Unmarshal(msgBytes, &requestMsgFromOtherSide)
				case map[string]interface{}:
					// Handle the case where the Message is a map[string]interface{}
					fmt.Println("Message is a map[string]interface{}:", message)
					requestMsgFromOtherSide := types.NeuronServiceRequestMsg{}
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
					log.Panic(err2, "error sending scheduled transfer request")
				}

			}
			time.Sleep(1 * time.Minute)
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

				if buyerSharedAccountInfo.Balance.AsTinybar() < 100_000_000 {
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.BalanceError, "Your balance is too low, but I will serve you anyway", types.DoNothing)
				}

				otherPublicKey := requestMsgFromOtherSide.PublicKey
				otherPeerID := keylib.ConvertHederaPublicKeyToPeerID(otherPublicKey)
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
					log.Panic(decodeErr)
				}
				initiationError := commonlib.InitialConnect(ctx, p2pHost, *addrInfo, buyerBuffers, protocol)
				if initiationError != nil {
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, types.DialError, fmt.Sprintf("I tried to initialise a connection but got this error: %v", initiationError.Error()), types.PunchMe)
				}

				envelope := commonlib.TopicPostalEnvelope{
					OtherStdInTopic: otherSideStdIn,
					Message:         requestMsgFromOtherSide,
				}
				buyerBuffers.SetLastOtherSideMultiAddress(addrInfo.ID, addrInfo.Addrs[0].String())
				buyerBuffers.SetNeuronSellerRequest(addrInfo.ID, types.TopicPostalEnvelope(envelope))
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
