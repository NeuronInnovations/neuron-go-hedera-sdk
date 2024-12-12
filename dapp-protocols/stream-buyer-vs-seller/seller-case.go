package streambuyervsseller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	commonlib "neuron/sdk/common-lib"
	flags "neuron/sdk/common-lib"
	neuronbuffers "neuron/sdk/common-lib"
	hedera_helper "neuron/sdk/hedera"
	"neuron/sdk/keylib"
	validatorLib "neuron/sdk/validator-lib"
	"os"
	"strings"
	"time"

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
					log.Panicf("Unexpected Message type: %T", message)
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
					log.Panic(err2)
				}

			}
			time.Sleep(45 * time.Minute)
		}
	}()

	go sellerCase(ctx, p2pHost, buyerBuffers)

	hedera_helper.ListenToTopicAndCallBack(commonlib.MyStdIn,

		func(message hedera.TopicMessage) {

			fmt.Println("request from other side: ", message.Contents)

			lastStdInTimestamp := message.ConsensusTimestamp.Format(time.RFC3339Nano)

			commonlib.UpdateEnvVariable("last_stdin_timestamp", lastStdInTimestamp, commonlib.MyEnvFile)

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

				if requestMsgFromOtherSide.Version != "0.3" {
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
					fmt.Println("That seller doesn't exist on the hedera network:", err)
					return
				}

				// TODO: check what it says in the SLA
				validatorLib.IsRequestPermitted()
				if !validatorLib.IsRequestPermitted() {
					log.Println("NACK: Ignore message as it is not match the SLA") // TODO: send to the other side
					return
				}

				if buyerSharedAccountInfo.Balance.AsTinybar() < 100000000 {
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
				initiationError := commonlib.InitialConnect(ctx, p2pHost, *addrInfo, buyerBuffers, protocol)
				if initiationError != nil {
					hedera_helper.PeerSendErrorMessage(otherSideStdIn, commonlib.DialError, fmt.Sprintf("I tried to initialise a connection but got this error: %v", initiationError.Error()), commonlib.PunchMe)
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
				case commonlib.ServiceError:
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
