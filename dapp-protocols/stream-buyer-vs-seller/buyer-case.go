package streambuyervsseller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	commonlib "neuron/sdk/common-lib"
	flags "neuron/sdk/common-lib"
	neuronbuffers "neuron/sdk/common-lib"
	hedera_helper "neuron/sdk/hedera"
	"neuron/sdk/keylib"
	"neuron/sdk/upnp"
	validatorLib "neuron/sdk/validator-lib"
	"neuron/sdk/whoami"
	"os"
	"strings"
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/umahmood/haversine"
	"golang.org/x/time/rate"
)

func HandleBuyerCase(ctx context.Context, p2pHost host.Host, buyerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers), buyerCaseTopicCallBack func(topicMessage hedera.TopicMessage)) {
	fmt.Println("Acting as a data buyer (I'll be initiating a request and then waiting for data to come in)")

	if !whoami.NatReachability {
		fmt.Printf("Your consumer node is not reachable on port %d and you will need to add a port forward to your router.\n", whoami.NatPort)
		fmt.Printf("We can do this for you using uPNP if your router has it enabled and you have passed the enable-upnp flag. Status of the flag is: %v \n", *flags.EnableUpPNPFlag)
		errorPrompt := " 💔💔💔 UPnP forwarding didn't move on; we'll continue but you may not get incoming data"
		if *flags.EnableUpPNPFlag {
			fmt.Println("Attempting to open port using UPnP...")
			controlURL, err := upnp.SendSSDPRequest()
			if err != nil {
				fmt.Println("Failed to get control URL; ", errorPrompt, err)
				time.Sleep(3 * time.Second)
			} else {
				pfErr := upnp.AddPortMapping(controlURL, whoami.NatPort, whoami.NatPort, "UDP", "Neuron")
				if pfErr != nil {
					fmt.Println("Failed to enable UPnP; ", errorPrompt, err)
					time.Sleep(3 * time.Second)
				} else {
					fmt.Println("Port opened successfully!")
					whoami.GetNatInfoAndUpdateGlobals(flags.PortFlag)
				}
			}
		} else {
			fmt.Println(errorPrompt)
			time.Sleep(3 * time.Second)
		}
	}

	// wait for reachableAddresses to have at least two addresses
	reachableAddresses := p2pHost.Addrs()
	// repeatedly probe and wait until there is something in it - not really need for pion but let's leave here.
	for len(reachableAddresses) < 1 {
		log.Println(" reachable Addresses: ", reachableAddresses)
		reachableAddresses = p2pHost.Addrs()
		time.Sleep(1 * time.Second)
	}

	fmt.Println("final reachable Addresses: ", reachableAddresses)
	immutable := make([]multiaddr.Multiaddr, len(reachableAddresses))
	copy(immutable, reachableAddresses)

	sellerBuffers := commonlib.NodeBuffersInstance
	if sellerBuffers == nil {
		sellerBuffers = commonlib.NewNodeBuffers()
	}

	go buyerCase(ctx, p2pHost, sellerBuffers)

	// ------- LISTEN -----------

	go hedera_helper.ListenToTopicAndCallBack(commonlib.MyStdIn, func(topicMessage hedera.TopicMessage) {
		fmt.Println("received: ", topicMessage.Contents)

		// TODO: is this someone I want to respond to? Have I asked him for services?
		validatorLib.IsRequestPermitted()
		if !validatorLib.IsRequestPermitted() {
			return
		}
		messageType, ok := commonlib.CheckMessageType(topicMessage.Contents)
		if !ok {
			fmt.Println("The message doesn't parse")
			return
		}
		switch messageType {
		case "scheduleSignRequest": // invoice from seller, schedule countersignature request
			scheduleSignRequest := new(commonlib.NeuronScheduleSignRequestMsg)
			err := json.Unmarshal(topicMessage.Contents, &scheduleSignRequest)
			if err != nil {
				fmt.Println("Error un marshalling message service response message", err)
				return
			}
			if scheduleSignRequest.Version != "0.4" {
				fmt.Printf("Ignore %s message as it does not match the current version\n", messageType)
				return
			}
			sid, err := hedera.ScheduleIDFromString(fmt.Sprintf("0.0.%d", scheduleSignRequest.ScheduleID))
			if err != nil {
				fmt.Println("SELFERROR:could not parse scheduleID", err)
				//TODO: shall we send this to the error topic?
				return
			}
			if !validatorLib.IsRequestPermitted() {
				return
			}
			// sign the schedule to release the money
			hedera_helper.SignSchedule(sid, os.Getenv("private_key"))
			// add more money to shared acount for next round.
			// TODO: use the amount from the SLA
			if !validatorLib.IsRequestPermitted() {
				return
			}
			sharedAcc, _ := hedera.AccountIDFromString(fmt.Sprintf("0.0.%d", scheduleSignRequest.SharedAccID))
			fmt.Println("adding money to shared account for next round:", sharedAcc)
			err = hedera_helper.DepositToSharedAccount(sharedAcc, 1)
			if err != nil {
				fmt.Println("SELFERROR:could not deposit to shared account ", err)
			}
		case "peerError": // error from seller
			sellerError := new(commonlib.NeuronPeerErrorMsg)
			err := json.Unmarshal(topicMessage.Contents, &sellerError)
			if err != nil {
				fmt.Println("Error un marshalling message service response message", err)
				return
			}
			switch sellerError.ErrorType {
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
				fmt.Println("Unknown error type: ", sellerError.ErrorType)
				//TODO: penalize message sender.
				hedera_helper.SendSelfErrorMessage(commonlib.BadMessageError, "I received a message that I don't understand", commonlib.StopSending)
				return
			}
		default:
			// forward all other messages to the dapp developer.
			buyerCaseTopicCallBack(topicMessage)
			return
		}

	})
	// -------------------------- END OF BUYER SIDE callback --------------------------

	// -------------------------- LIST SELLERS AND BUY       --------------------------

	type Seller struct {
		PublicKey string
		Lat       float64
		Lon       float64
	}

	listOfSellers := make(map[Seller]bool)

	// if the list of sellers source is the environment file the we get it from there (otherwise ask explorer)
	if *flags.ListOfSellersSourceFlag == "env" {
		var listOfSellersEnvList = os.Getenv("list_of_sellers")
		if listOfSellersEnvList == "" {
			log.Println("list_of_sellers is empty")
			return
		}

		for _, seller := range strings.Split(listOfSellersEnvList, ",") {
			sEvm := keylib.ConverHederaPublicKeyToEthereunAddress(seller)
			peerInfo, err := hedera_helper.GetPeerInfo(sEvm)
			if err != nil {
				log.Fatal(err)
			}

			if m, ok := getPeerHeartbeatIfRecent(peerInfo); ok {
				heartbeatMessage := new(commonlib.NeuronHeartBeatMsg)
				base64Decoded, _ := base64.StdEncoding.DecodeString(m.Message)
				err = json.Unmarshal([]byte(base64Decoded), &heartbeatMessage)
				if err != nil {
					log.Println("error unmarshalling heartbeat message: ", err)
				} else {
					// make a Seller object and fill it up
					seller := Seller{
						PublicKey: seller,
						Lat:       heartbeatMessage.Location.Latitude,
						Lon:       heartbeatMessage.Location.Longitude,
					}
					listOfSellers[seller] = true
				}
			} else {
				log.Fatal("Node heartbeat is not ok. Provide a list where every single node has a heartbeat:  ", seller, peerInfo)
			}
		}
	} else { // if the flag list-of-sellers is not set to env then get it from the explorer
		go func() {
			var limiter = rate.NewLimiter(5, 1)
			for {
				// get the list of devices from the explorer
				devices, err := hedera_helper.GetAllDevicesFromExplorer()
				if err != nil {
					log.Println("💀  GetAllPeers error: ", err)
					hedera_helper.SendSelfErrorMessage(commonlib.ExplorerReachError, "Could not get devices from the explorer", commonlib.RebootMe)
					continue
				}

				log.Println("🔎  got ", len(devices), " devices from the explorer")

				for _, device := range devices {

					if publicKey, ok := device["publickey"].(string); ok {
						if devicerole, ok := device["devicerole"].(float64); ok {
							if devicerole == 0 { // it's a seller dvice
								stdout := device["topic_stdout"].(string)
								stdoutTyped, _ := hedera.TopicIDFromString(stdout)
								// do an api call to see if there is a heartbeat
								limiter.Wait(context.Background())
								m, lastMessageError := hedera_helper.GetLastMessageFromTopic(stdoutTyped)
								if lastMessageError != nil {
									continue
								}
								if m.Timestamp.IsZero() || m.Timestamp.Before(time.Now().Add(-10*time.Minute)) {
									continue
								}
								hpub, err := hedera.PublicKeyFromString(publicKey)
								if err != nil {
									log.Println("💀  hedera.PublicKeyFromString error: ", err)
									continue
								}

								// finally, include the seller in the list

								publicKey = hpub.StringRaw()
								heartbeatMessage := new(commonlib.NeuronHeartBeatMsg)
								base64Decoded, _ := base64.StdEncoding.DecodeString(m.Message)
								err = json.Unmarshal([]byte(base64Decoded), &heartbeatMessage)
								if err != nil {
									log.Println("error un marshalling heartbeat message: ", err)
								} else {

									seller := Seller{
										PublicKey: publicKey,
										Lat:       heartbeatMessage.Location.Latitude,
										Lon:       heartbeatMessage.Location.Longitude,
									}

									// if the radius flag is set then check if the seller is in the radius
									if *flags.RadiusFlag != 0 {

										centerLat := commonlib.MyLocation.Latitude
										centerLon := commonlib.MyLocation.Longitude
										// get the radius
										radius := flags.RadiusFlag
										// calculate the distance using the haversine formula
										center := haversine.Coord{Lat: centerLat, Lon: centerLon}
										farPoint := haversine.Coord{Lat: seller.Lat, Lon: seller.Lon}
										_, distanceKm := haversine.Distance(center, farPoint)
										// check if the distance is less than the radius
										if int(distanceKm) < *radius {
											listOfSellers[seller] = true
										}
									} else { // no filtering by radius set.
										listOfSellers[seller] = true
									}
								}
								// todo: deduplicate the list
							}
						}
					}
				}
				time.Sleep(120 * time.Second)
			}
		}()
	}

	/*
		Every 20 seconds we will be handling every seller indivudually.
		- We want to send him a request for service if we have not done so before
		- We want to check if the seller has established a connection with us (after a request was sent)
		- We want to check if a seller has lost a connection when there previously was one and send him a re-request, which is the same as the initial request but with nack-noConnection in front of MessageType
	*/

	var prepareServiceRequestMsg = func(seller string) (commonlib.TopicPostalEnvelope, error) {
		res, err := hedera_helper.BuyerPrepareServiceRequest(
			immutable, //HostsPublicAddressesSorted(p2pHost)[0],
			os.Getenv("hedera_evm_id"),
			keylib.ConverHederaPublicKeyToEthereunAddress(seller),
			"e2436b1e019e993215e832762f9242020d199940", // that's the london address, yes; it's fixed for now but a parameter in env MyArbiterPublicKey in the future.
			1, // price, every seller gets the same for now
		)
		if err != nil {
			return commonlib.TopicPostalEnvelope{}, err
		}
		return *res, err
	}

	for {

		for seller := range listOfSellers {

			sellerEvnAddress := keylib.ConverHederaPublicKeyToEthereunAddress(seller.PublicKey)
			peerIDStr := keylib.ConvertHederaPublicKeyToPeerID(seller.PublicKey)
			peerID, _ := peer.Decode(peerIDStr)
			perrInfo, err := hedera_helper.GetPeerInfo(sellerEvnAddress)

			if err != nil {
				continue
			}
			if !perrInfo.Available {
				continue
			}

			peerBuffer, peerHasBuffer := sellerBuffers.GetBuffer(peerID)

			if peerHasBuffer && !peerBuffer.IsOtherSideValidAccount {
				fmt.Printf("skipping invalid seller: evm address %s ", sellerEvnAddress)
				continue
			}

			if peerHasBuffer && peerBuffer.NextScheduleRequestTime.After(time.Now()) {
				continue
			}

			//TODO: check if the remote peer has a heartbeat in the past 5 minutes

			conns := p2pHost.Network().ConnsToPeer(peerID)

			if len(conns) == 0 {

				if !peerHasBuffer { // no cons and never requested
					envelope, setupErr := prepareServiceRequestMsg(seller.PublicKey)
					if setupErr != nil {
						sellerBuffers.AddBuffer2(peerID, envelope, false, commonlib.NotInitiated, neuronbuffers.LibP2PState(commonlib.BadMessageError))
						log.Printf("💀 envelope setup error; seller %s will be blacklisted, err: %v \n", sellerEvnAddress, setupErr)
						hedera_helper.SendSelfErrorMessage(neuronbuffers.BadMessageError, "Could not create envelope for: "+sellerEvnAddress, commonlib.DoNothing)
						continue
					}

					sellerBuffers.IncrementReconnectAttempts(peerID)
					if execErr := hedera_helper.SendTransactionEnvelope(envelope); execErr != nil {
						// has errors
						sellerBuffers.AddBuffer2(peerID, envelope, true, commonlib.SendFail, commonlib.Connecting)
						log.Printf("💀 send hedera transaction envelope error %s, will allow to try later %v \n", sellerEvnAddress, setupErr)
						hedera_helper.SendSelfErrorMessage(neuronbuffers.ServiceError, "Could not send the reqquest to: "+sellerEvnAddress, commonlib.DoNothing)
						continue
					}
					// has no errors
					sellerBuffers.AddBuffer2(peerID, envelope, true, commonlib.SendOK, commonlib.Connecting)

				} else { // have buffer,  no cons and requested before: re-submit for up to backoff
					if isTooEarly, _ := commonlib.IsRequestTooEarly(sellerBuffers, peerID); isTooEarly {
						continue
					} else {
						a, b, _ := sellerBuffers.GetReconnectInfo(peerID)
						log.Println("re-submit because it's time now", isTooEarly, a, b)
					}
					sellerBuffers.IncrementReconnectAttempts(peerID)
					secondExecError := hedera_helper.SendTransactionEnvelope(peerBuffer.RequestOrResponse)
					if secondExecError != nil {
						log.Printf("💀-2  skip that seller %s because ExecuteHederaTransaction error: %v", sellerEvnAddress, secondExecError)
						// TODO: 💥 tell to myself that I could not send the transaction to the other side
						hedera_helper.SendSelfErrorMessage(neuronbuffers.ServiceError, "Could not send the reqquest to: "+sellerEvnAddress, commonlib.DoNothing)
						sellerBuffers.UpdateBufferRendezvousState(peerID, commonlib.SendFail)
						sellerBuffers.UpdateBufferLibP2PState(peerID, commonlib.CanNotConnectUnknownReason)
						continue
					}
					sellerBuffers.UpdateBufferRendezvousState(peerID, commonlib.SendOK)
					sellerBuffers.UpdateBufferLibP2PState(peerID, commonlib.Connecting)
				}
			} else { // there are cons
				if !peerHasBuffer {
					// it's possible to not have a buffer when you reboot but people still talk to you and that's why you have cons.
					// just close those cons and issue a new request, you can be lazy and let the loop do that in the next iteration.
					fmt.Println("I am connected but have no buffer. .. i'll just hang around for the loop to issue a fresh request ", peerID)
					p2pHost.Network().ClosePeer(peerID)
					continue
				}

				streams := 0
				//log.Println("This one has cons", conns)
				for _, conn := range conns {
					streams += len(conn.GetStreams())
				}
				if streams == 0 { // there are cons but no streams
					log.Println("I am connected but have no streams. .. i'll just hang around ", peerID, streams)
					continue
				} else {
					sellerBuffers.UpdateBufferRendezvousState(peerID, commonlib.SendOK)
					sellerBuffers.UpdateBufferLibP2PState(peerID, commonlib.Connected)
					sellerBuffers.SetLastOtherSideMultiAddress(peerID, conns[0].RemoteMultiaddr())

				}
			} // end if there are conns
		} // end for

		time.Sleep(20 * time.Second)
	} // end for

}

func getPeerHeartbeatIfRecent(peerInfo hedera_helper.PeerInfo) (hedera_helper.HCSMessage, bool) {

	stdoutTyped, err := hedera.TopicIDFromString(fmt.Sprintf("0.0.%d", peerInfo.StdOutTopic))
	if err != nil {
		log.Fatal(err, peerInfo.StdOutTopic)
	}
	m, lastMessageError := hedera_helper.GetLastMessageFromTopic(stdoutTyped)
	if lastMessageError != nil {
		log.Println("🪰🪰  GetLastMessageFromTopic error: ", lastMessageError)
		//log.Fatal(lastMessageError)
		return hedera_helper.HCSMessage{}, false
	}

	if m.Timestamp.IsZero() || m.Timestamp.Before(time.Now().Add(-5*time.Minute)) {
		fmt.Println("node seems dead")
		return hedera_helper.HCSMessage{}, false
	}
	return m, true
}
