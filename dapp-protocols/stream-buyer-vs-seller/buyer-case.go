package streambuyervsseller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/whoami"

	validatorLib "github.com/NeuronInnovations/neuron-go-hedera-sdk/validator-lib"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/upnp"

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
	"github.com/umahmood/haversine"
	"golang.org/x/time/rate"
)

type Seller struct {
	PublicKey string
	Lat       float64
	Lon       float64
}

func HandleBuyerCase(ctx context.Context, p2pHost host.Host, protocol protocol.ID, buyerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers), buyerCaseTopicCallBack func(topicMessage hedera.TopicMessage)) {
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
	constMyReachableAddresses := make([]multiaddr.Multiaddr, len(reachableAddresses))
	copy(constMyReachableAddresses, reachableAddresses)

	sellerBuffers := commonlib.NodeBuffersInstance
	if sellerBuffers == nil {
		sellerBuffers = commonlib.NewNodeBuffers()
	}

	go buyerCase(ctx, p2pHost, sellerBuffers)

	// ------- LISTEN -----------

	go hedera_helper.ListenToTopicAndCallBack(commonlib.MyStdIn, func(topicMessage hedera.TopicMessage) {
		fmt.Printf("received %s: ", topicMessage.Contents)

		// TODO: is this someone I want to respond to? Have I asked him for services?

		fmt.Printf("request from other side: %s ", topicMessage.Contents)

		//lastStdInTimestamp := topicMessage.ConsensusTimestamp.Format(time.RFC3339Nano)

		//commonlib.UpdateEnvVariable("last_stdin_timestamp", lastStdInTimestamp, commonlib.MyEnvFile)

		validatorLib.IsRequestPermitted()
		if !validatorLib.IsRequestPermitted() {
			return
		}
		messageType, ok := types.CheckMessageType(topicMessage.Contents)
		if !ok {
			fmt.Println("The message doesn't parse")
			return
		}
		switch messageType {
		case "scheduleSignRequest": // invoice from seller, schedule countersignature request
			scheduleSignRequest := new(types.NeuronScheduleSignRequestMsg)
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
			err = hedera_helper.DepositToSharedAccount(sharedAcc, 100)
			if err != nil {
				fmt.Println("SELFERROR:could not deposit to shared account ", err)
			}
		case "punchMeRequest":
			// Handle punchMeRequest from seller for hole punching
			handlePunchMeRequest(topicMessage, p2pHost, sellerBuffers, protocol)

		case "peerError": // error from seller
			sellerError := new(types.NeuronPeerErrorMsg)
			err := json.Unmarshal(topicMessage.Contents, &sellerError)
			if err != nil {
				fmt.Println("Error un marshalling message service response message", err)
				return
			}
			switch sellerError.ErrorType {
			case types.DialError:
				// get the public key of th sender
				///peerIDStr := keylib.ConvertHederaPublicKeyToPeerID(acc.PublicKey)
				//p2pHost.Network().ClosePeer(peer.ID(peerIDStr))
				//log.Println("Response to dial error cleaning streams to: ", peer.ID(peerIDStr))
			case types.FlushError:
			case types.DisconnectedError:
			case types.NoKnownAddressError:
			case types.HeartBeatError:
			case types.BalanceError:
			case types.VersionError:
			case types.WriteError:
				fmt.Println("Write error: ", sellerError)
				switch sellerError.RecoverAction {
				case types.SendFreshHederaRequest:
					// look into the buffers, we have the request there and send it again
					processSeller(Seller{PublicKey: sellerError.PublicKey}, p2pHost, sellerBuffers, constMyReachableAddresses, protocol)
				case types.PunchMe:
					// Enhanced handling: The seller is requesting hole punching
					log.Printf("Received PunchMe request from seller %s, waiting for punchMeRequest message", sellerError.PublicKey)
				case types.DoNothing:
				}
			case types.StreamError:
			case types.IpDecryptionError:
			case types.ServiceError:
			default:
				fmt.Println("Unknown error type: ", sellerError.ErrorType)
				//TODO: penalize message sender.
				hedera_helper.SendSelfErrorMessage(types.BadMessageError, "I received a message that I don't understand", types.StopSending)
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

	var (
		listOfSellers     = make(map[Seller]bool)
		listOfSellersLock sync.RWMutex
	)

	startSecondLoop := make(chan struct{})

	// if the list of sellers source is the environment file the we get it from there (otherwise ask explorer)
	if *flags.ListOfSellersSourceFlag == "env" {
		var listOfSellersEnvList = os.Getenv("list_of_sellers")
		if listOfSellersEnvList == "" {
			log.Println("list_of_sellers is empty")
			return
		}

		go func() {
			for {
				for _, seller := range strings.Split(listOfSellersEnvList, ",") {
					sEvm := keylib.ConverHederaPublicKeyToEthereunAddress(seller)
					peerInfo, err := hedera_helper.GetPeerInfo(sEvm)
					if err != nil {
						log.Fatal(err)
					}

					if m, ok := getPeerHeartbeatIfRecent(peerInfo); ok {
						heartbeatMessage := new(types.NeuronHeartBeatMsg)
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
							listOfSellersLock.Lock()
							listOfSellers[seller] = true
							listOfSellersLock.Unlock()
						}
					} else {
						log.Println("Node heartbeat is not ok. I'll skip him in this round but will try again in 120 seconds  ", seller, peerInfo)
					}
				}
				select {
				case startSecondLoop <- struct{}{}:
				default:
				}
				time.Sleep(120 * time.Second)
			}
		}()
	} else { // if the flag list-of-sellers is not set to env then get it from the explorer
		go func() {
			var limiter = rate.NewLimiter(5, 1)
			for {
				// get the list of devices from the explorer  every 120 seconds
				devices, err := hedera_helper.GetAllDevicesFromExplorer()
				if err != nil {
					log.Println("💀  GetAllPeers error: ", err)
					hedera_helper.SendSelfErrorMessage(types.ExplorerReachError, "Could not get devices from the explorer", types.RebootMe)
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
								heartbeatMessage := new(types.NeuronHeartBeatMsg)
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
									if *flags.RadiusFlag > 0 {

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
											listOfSellersLock.Lock()
											listOfSellers[seller] = true
											listOfSellersLock.Unlock()
										}
									} else { // no filtering by radius set.
										listOfSellersLock.Lock()
										listOfSellers[seller] = true
										listOfSellersLock.Unlock()
									}
								}
								// todo: deduplicate the list
							}
						}
					}
				}
				select {
				case startSecondLoop <- struct{}{}:
				default:
				}
				time.Sleep(120 * time.Second)
			}
		}()
	}

	/*
		Every 60 seconds we will be handling every seller individually.
		- We want to send him a request for service if we have not done so before
		- We want to check if the seller has established a connection with us (after a request was sent)
		- We want to check if a seller has lost a connection when there previously was one and send him a re-request, which is the same as the initial request but with nack-noConnection in front of MessageType
	*/

	go func() {
		<-startSecondLoop
		for {
			listOfSellersLock.RLock()                            // Lock before reading the map
			sellersCopy := make([]Seller, 0, len(listOfSellers)) // Slice to store copied keys

			// Copy the keys (Seller structs) into the slice
			for seller := range listOfSellers {
				sellersCopy = append(sellersCopy, seller)
			}
			listOfSellersLock.RUnlock() // Unlock after copying

			for _, seller := range sellersCopy {
				processSeller(seller, p2pHost, sellerBuffers, constMyReachableAddresses, protocol)
			}

			time.Sleep(60 * time.Second)
		} // end for
	}()
}

// handlePunchMeRequest processes the punchMeRequest message from seller and initiates buyer's hole punching
func handlePunchMeRequest(topicMessage hedera.TopicMessage, p2pHost host.Host, sellerBuffers *commonlib.NodeBuffers, protocol protocol.ID) {
	// 1. Parse punchMeRequest
	var punchMeMsg types.NeuronPunchMeRequestMsg
	if err := json.Unmarshal(topicMessage.Contents, &punchMeMsg); err != nil {
		log.Printf("Failed to unmarshal punchMeRequest: %v", err)
		return
	}

	log.Printf("Received punchMeRequest from seller with timestamp %s", punchMeMsg.HederaTimestamp)

	// 2. Convert seller's public key to peer ID
	sellerPeerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(punchMeMsg.PublicKey)
	if err != nil {
		log.Printf("Error converting seller public key to peer ID: %v", err)
		return
	}
	sellerPeerID, err := peer.Decode(sellerPeerIDStr)
	if err != nil {
		log.Printf("Error decoding seller peer ID: %v", err)
		return
	}

	// 3. Decrypt seller's IP addresses
	sellerIPBytes, err := keylib.DecryptFromOtherside(
		punchMeMsg.EncryptedIpAddress,
		os.Getenv("private_key"),
		punchMeMsg.PublicKey,
	)
	if err != nil {
		log.Printf("Failed to decrypt seller IP addresses: %v", err)
		return
	}

	// 4. Parse seller's IP addresses
	sellerIPString := string(sellerIPBytes)
	sellerIPs := strings.Fields(sellerIPString)

	// 5. Send acknowledgment to seller
	ackEnvelope, err := preparePunchMeAcknowledgment(punchMeMsg)
	if err != nil {
		log.Printf("Failed to prepare punchMeAcknowledgment: %v", err)
		return
	}

	if err := hedera_helper.SendTransactionEnvelope(ackEnvelope); err != nil {
		log.Printf("Failed to send punchMeAcknowledgment: %v", err)
		return
	}

	log.Printf("Sent punchMeAcknowledgment to seller %s", sellerPeerID)

	// 6. Update buffer state
	sellerBuffers.UpdateBufferLibP2PState(sellerPeerID, types.HolePunchingInProgress)

	// 7. Schedule synchronized hole punching
	scheduleBuyerHolePunching(punchMeMsg.HederaTimestamp, punchMeMsg.PunchDelay, sellerIPs, sellerPeerID, p2pHost, protocol, sellerBuffers)
}

// preparePunchMeAcknowledgment creates an acknowledgment message for the punchMeRequest
func preparePunchMeAcknowledgment(punchMeMsg types.NeuronPunchMeRequestMsg) (types.TopicPostalEnvelope, error) {
	// Get buyer's public key
	buyerPublicKey, err := getBuyerPublicKey()
	if err != nil {
		return types.TopicPostalEnvelope{}, fmt.Errorf("failed to get buyer public key: %w", err)
	}

	// Create acknowledgment message
	ackMsg := types.NeuronPunchMeAcknowledgmentMsg{
		MessageType:     "punchMeAcknowledgment",
		RequestTopic:    punchMeMsg.StdInTopic,
		PublicKey:       buyerPublicKey,
		HederaTimestamp: punchMeMsg.HederaTimestamp,
		Version:         "0.4",
	}

	// Create seller's stdin topic
	sellerStdIn := hedera.TopicID{
		Shard: 0,
		Realm: 0,
		Topic: punchMeMsg.StdInTopic,
	}

	return types.TopicPostalEnvelope{
		OtherStdInTopic: sellerStdIn,
		Message:         ackMsg,
	}, nil
}

// scheduleBuyerHolePunching schedules the buyer's side of synchronized hole punching
func scheduleBuyerHolePunching(hederaTimestamp string, delaySeconds int, sellerIPs []string, sellerPeerID peer.ID, p2pHost host.Host, protocol protocol.ID, sellerBuffers *commonlib.NodeBuffers) {
	// 1. Parse Hedera timestamp
	consensusTime, err := time.Parse(time.RFC3339Nano, hederaTimestamp)
	if err != nil {
		log.Printf("Failed to parse Hedera timestamp: %v", err)
		return
	}

	// 2. Calculate punch time (consensus time + delay)
	punchTime := consensusTime.Add(time.Duration(delaySeconds) * time.Second)

	log.Printf("Scheduling buyer hole punching for seller %s at %s (delay: %ds)", sellerPeerID, punchTime, delaySeconds)

	// 3. Schedule the hole punching
	go func() {
		time.Sleep(time.Until(punchTime))

		// 4. Perform hole punching
		performBuyerHolePunching(sellerIPs, sellerPeerID, p2pHost, protocol, sellerBuffers)
	}()
}

// performBuyerHolePunching performs the actual hole punching attempt from buyer side
func performBuyerHolePunching(sellerIPs []string, sellerPeerID peer.ID, p2pHost host.Host, protocol protocol.ID, sellerBuffers *commonlib.NodeBuffers) {
	log.Printf("Performing buyer hole punching with seller %s", sellerPeerID)

	// Try each seller IP address
	for _, sellerIP := range sellerIPs {
		// Create addrInfo from seller IP
		addrInfo, err := peer.AddrInfoFromString(sellerIP + "/p2p/" + sellerPeerID.String())
		if err != nil {
			log.Printf("Failed to parse seller address %s: %v", sellerIP, err)
			continue
		}

		// Use libp2p's hole punching capabilities
		ctx := context.Background()
		holePunchCtx := network.WithSimultaneousConnect(ctx, true, "hole-punching") // true for buyer (client)
		forceDirectConnCtx := network.WithForceDirectDial(holePunchCtx, "hole-punching")

		// Attempt connection with hole punching
		dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, 30*time.Second)
		defer cancel()

		if err := p2pHost.Connect(dialCtx, *addrInfo); err != nil {
			log.Printf("Buyer hole punching failed for seller %s with address %s: %v", sellerPeerID, sellerIP, err)
			continue
		}

		log.Printf("Buyer hole punching successful ❤️❤️❤️ with seller %s using address %s", sellerPeerID, sellerIP)
		sellerBuffers.UpdateBufferLibP2PState(sellerPeerID, types.HolePunchingCompleted)

		/*
			// Try to create stream if connection successful
			stream, err := p2pHost.NewStream(dialCtx, sellerPeerID, protocol)
			if err != nil {
				log.Printf("Failed to create stream after buyer hole punching: %v", err)
				continue
			}

			log.Printf("Stream created successfully after buyer hole punching: %v", stream)
			sellerBuffers.UpdateBufferLibP2PState(sellerPeerID, types.Connected)
		*/
		// Store the successful address
		sellerBuffers.SetLastOtherSideMultiAddress(sellerPeerID, sellerIP)

		// Success - no need to try other addresses
		return
	}

	// If we get here, all addresses failed
	log.Printf("All buyer hole punching attempts failed for seller %s", sellerPeerID)
	sellerBuffers.UpdateBufferLibP2PState(sellerPeerID, types.CanNotConnectUnknownReason)
}

// getBuyerPublicKey gets the buyer's public key
func getBuyerPublicKey() (string, error) {
	return commonlib.MyPublicKey.StringRaw(), nil
}

func prepareServiceRequestMsg(seller string, myReachableAddresses []multiaddr.Multiaddr) (types.TopicPostalEnvelope, error) {
	res, err := hedera_helper.BuyerPrepareServiceRequest(
		myReachableAddresses,
		os.Getenv("hedera_evm_id"),
		keylib.ConverHederaPublicKeyToEthereunAddress(seller),
		"e2436b1e019e993215e832762f9242020d199940",
		100, // 100 milli hbar
	)

	if err != nil {
		return types.TopicPostalEnvelope{}, err
	}

	return *res, nil
}

func processSeller(seller Seller, p2pHost host.Host, sellerBuffers *commonlib.NodeBuffers, myReachableAddresses []multiaddr.Multiaddr, protocolID protocol.ID) {
	sellerEvnAddress := keylib.ConverHederaPublicKeyToEthereunAddress(seller.PublicKey)
	// Convert seller's public key to peer ID
	peerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(seller.PublicKey)
	if err != nil {
		log.Printf("Error converting seller public key to peer ID: %v", err)
		return
	}
	targetPeerID, err := peer.Decode(peerIDStr)
	if err != nil {
		log.Printf("Error decoding peer ID: %v", err)
		return
	}
	perrInfo, err := hedera_helper.GetPeerInfo(sellerEvnAddress)

	if err != nil {
		return
	}
	if !perrInfo.Available {
		return
	}

	peerBuffer, peerHasBuffer := sellerBuffers.GetBuffer(targetPeerID)

	if peerHasBuffer && peerBuffer == nil {
		log.Printf("Warning: Got nil peerBuffer for peer %s", targetPeerID)
		return
	}

	if peerHasBuffer && !peerBuffer.IsOtherSideValidAccount {
		fmt.Printf("skipping invalid seller: evm address %s ", sellerEvnAddress)
		return
	}

	if peerHasBuffer && peerBuffer.NextScheduleRequestTime.After(time.Now()) {
		return
	}

	connsToPeer := p2pHost.Network().ConnsToPeer(targetPeerID)

	if len(connsToPeer) == 0 {
		if !peerHasBuffer {
			envelope, setupErr := prepareServiceRequestMsg(seller.PublicKey, myReachableAddresses)
			if setupErr != nil {
				sellerBuffers.AddBuffer2(targetPeerID, envelope, false, types.NotInitiated, types.ConnectionLost)
				log.Printf("💀 envelope setup error; seller %s will be blacklisted, err: %v \n", sellerEvnAddress, setupErr)
				hedera_helper.SendSelfErrorMessage(types.BadMessageError, "Could not create envelope for: "+sellerEvnAddress, types.DoNothing)
				return
			}

			sellerBuffers.IncrementReconnectAttempts(targetPeerID)
			if execErr := hedera_helper.SendTransactionEnvelope(envelope); execErr != nil {
				sellerBuffers.AddBuffer2(targetPeerID, envelope, true, types.SendFail, types.Connecting)
				log.Printf("💀 send hedera transaction envelope error %s, will allow to try later %v \n", sellerEvnAddress, setupErr)
				hedera_helper.SendSelfErrorMessage(types.ServiceError, "Could not send the reqquest to: "+sellerEvnAddress, types.DoNothing)
				return
			}
			sellerBuffers.AddBuffer2(targetPeerID, envelope, true, types.SendOK, types.Connecting)
		} else if isTooEarly, _ := commonlib.IsRequestTooEarly(sellerBuffers, targetPeerID); isTooEarly {
			return
		} else {
			a, b, _ := sellerBuffers.GetReconnectInfo(targetPeerID)
			log.Println("re-submit because it's time now", isTooEarly, a, b)
		}
		sellerBuffers.IncrementReconnectAttempts(targetPeerID)
		if peerBuffer != nil && peerBuffer.RequestOrResponse.Message != nil {
			secondExecError := hedera_helper.SendTransactionEnvelope(peerBuffer.RequestOrResponse)
			if secondExecError != nil {
				log.Printf("💀-2  skip that seller %s because ExecuteHederaTransaction error: %v", sellerEvnAddress, secondExecError)
				hedera_helper.SendSelfErrorMessage(types.ServiceError, "Could not send the reqquest to: "+sellerEvnAddress, types.DoNothing)
				sellerBuffers.UpdateBufferRendezvousState(targetPeerID, types.SendFail)
				sellerBuffers.UpdateBufferLibP2PState(targetPeerID, types.CanNotConnectUnknownReason)
				return
			}
			sellerBuffers.UpdateBufferRendezvousState(targetPeerID, types.SendOK)
			sellerBuffers.UpdateBufferLibP2PState(targetPeerID, types.Connecting)
		} else {
			log.Printf("Warning: peerBuffer or RequestOrResponse.Message is nil for peer %s", targetPeerID)
			return
		}
	} else if !peerHasBuffer {
		fmt.Println("I am connected but have no buffer. .. i'll close the peer and just hang around for the loop to issue a fresh request ", targetPeerID)
		p2pHost.Network().ClosePeer(targetPeerID)
		return
	} else {
		streams := 0
		for _, conn := range connsToPeer {
			streams += len(conn.GetStreams())
		}
		if streams == 0 {
			log.Println("I am connected but have no streams. Because I'm a buyer, I'll just hang around for the loop to issue a fresh request. I am not supposed to do newStream - the seller is responsible for that.", targetPeerID, streams)

		} else {
			log.Println("I am connected and have streams..", targetPeerID, streams)

			sellerBuffers.UpdateBufferRendezvousState(targetPeerID, types.SendOK)
			sellerBuffers.UpdateBufferLibP2PState(targetPeerID, types.Connected)
			sellerBuffers.SetLastOtherSideMultiAddress(targetPeerID, connsToPeer[0].RemoteMultiaddr().String())
		}
	}
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

// ReplaceSellers updates the connected sellers list with the provided list of seller public keys.
// It handles three scenarios:
// - When a seller was there already: keep him, do nothing
// - When a seller wasn't there before: add him and processSeller
// - When a seller was there but is not in the update list: remove him from the buffer, disconnect him
// Returns the updated seller list and any error that occurred
// If currentSellers is nil, it will automatically determine current sellers from the buffers
func ReplaceSellers(sellerPublicKeys []string, currentSellers map[Seller]bool, p2pHost host.Host, sellerBuffers *commonlib.NodeBuffers, myReachableAddresses []multiaddr.Multiaddr, protocolID protocol.ID) (map[Seller]bool, error) {
	// If currentSellers is nil, automatically determine them from buffers
	if currentSellers == nil {
		currentSellers = ShowCurrentPeerStatus(sellerBuffers)
		log.Printf("Automatically determined %d current sellers from buffers", len(currentSellers))
	}

	// Create a map of new sellers for efficient lookup
	newSellersMap := make(map[string]Seller)

	// Process each seller public key in the input list
	for _, sellerPublicKey := range sellerPublicKeys {
		// Create a Seller struct with the provided public key
		seller := Seller{
			PublicKey: sellerPublicKey,
		}

		// Get seller's EVM address
		sellerEvnAddress := keylib.ConverHederaPublicKeyToEthereunAddress(seller.PublicKey)

		// Get peer info to verify availability in SC
		peerInfo, err := hedera_helper.GetPeerInfo(sellerEvnAddress)
		if err != nil {
			log.Printf("Failed to get peer SC info for %s: %v", sellerPublicKey, err)
			continue // Skip this seller but continue with others
		}
		if !peerInfo.Available {
			log.Printf("Seller %s is not present in the network SC", sellerPublicKey)
			continue // Skip this seller but continue with others
		}

		// Get the last heartbeat message to get location info
		if m, ok := getPeerHeartbeatIfRecent(peerInfo); ok {
			heartbeatMessage := new(types.NeuronHeartBeatMsg)
			base64Decoded, _ := base64.StdEncoding.DecodeString(m.Message)
			err = json.Unmarshal([]byte(base64Decoded), &heartbeatMessage)
			if err != nil {
				log.Printf("Failed to parse heartbeat message for %s: %v", sellerPublicKey, err)
				// Continue without location info
			} else {
				// Update seller location from heartbeat
				seller.Lat = heartbeatMessage.Location.Latitude
				seller.Lon = heartbeatMessage.Location.Longitude
			}
		}

		// Add to new sellers map
		newSellersMap[sellerPublicKey] = seller
	}

	// Create the new seller list
	updatedSellers := make(map[Seller]bool)

	// Process new sellers that weren't there before
	for sellerPublicKey, seller := range newSellersMap {
		// Convert seller's public key to peer ID
		peerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(seller.PublicKey)
		if err != nil {
			log.Printf("Error converting seller public key to peer ID: %v", err)
			continue
		}
		targetPeerID, err := peer.Decode(peerIDStr)
		if err != nil {
			log.Printf("Error decoding peer ID: %v", err)
			continue
		}

		// Check if this seller is already in our current sellers list
		sellerExists := false
		for currentSeller := range currentSellers {
			if currentSeller.PublicKey == seller.PublicKey {
				sellerExists = true
				// Keep the existing seller (with potentially different location data)
				updatedSellers[currentSeller] = true
				log.Printf("Seller already exists, keeping: %s", sellerPublicKey)
				break
			}
		}

		if !sellerExists {
			// Check if this seller is already in our buffers
			_, peerHasBuffer := sellerBuffers.GetBuffer(targetPeerID)

			if !peerHasBuffer {
				// New seller - add and process
				log.Printf("Adding new seller: %s", sellerPublicKey)
				processSeller(seller, p2pHost, sellerBuffers, myReachableAddresses, protocolID)
			} else {
				log.Printf("Seller has buffer but not in current list, adding: %s", sellerPublicKey)
			}

			// Add to updated sellers list
			updatedSellers[seller] = true
		}
	}

	// Remove sellers that are no longer in the new list
	for currentSeller := range currentSellers {
		found := false
		for _, newSeller := range newSellersMap {
			if currentSeller.PublicKey == newSeller.PublicKey {
				found = true
				break
			}
		}

		if !found {
			// This seller is no longer in the list - remove and disconnect
			log.Printf("Removing seller no longer in list: %s", currentSeller.PublicKey)

			// Convert seller's public key to peer ID
			peerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(currentSeller.PublicKey)
			if err != nil {
				log.Printf("Error converting seller public key to peer ID for removal: %v", err)
				continue
			}
			targetPeerID, err := peer.Decode(peerIDStr)
			if err != nil {
				log.Printf("Error decoding peer ID for removal: %v", err)
				continue
			}

			// Close the connection
			p2pHost.Network().ClosePeer(targetPeerID)

			// Remove from buffers
			sellerBuffers.RemoveBuffer(targetPeerID)
		}
	}

	return updatedSellers, nil
}

// ReplaceSellersSimple is a simplified version of ReplaceSellers that works with the seller buffers
// to determine current connections. It's easier to use as a drop-in replacement but may not be
// as precise as the full ReplaceSellers function.
func ReplaceSellersSimple(sellerPublicKeys []string, p2pHost host.Host, sellerBuffers *commonlib.NodeBuffers, myReachableAddresses []multiaddr.Multiaddr, protocolID protocol.ID) error {
	// Create a map of new sellers for efficient lookup
	newSellersMap := make(map[string]Seller)

	// Process each seller public key in the input list
	for _, sellerPublicKey := range sellerPublicKeys {
		// Create a Seller struct with the provided public key
		seller := Seller{
			PublicKey: sellerPublicKey,
		}

		// Get seller's EVM address
		sellerEvnAddress := keylib.ConverHederaPublicKeyToEthereunAddress(seller.PublicKey)

		// Get peer info to verify availability in SC
		peerInfo, err := hedera_helper.GetPeerInfo(sellerEvnAddress)
		if err != nil {
			log.Printf("Failed to get peer SC info for %s: %v", sellerPublicKey, err)
			continue // Skip this seller but continue with others
		}
		if !peerInfo.Available {
			log.Printf("Seller %s is not present in the network SC", sellerPublicKey)
			continue // Skip this seller but continue with others
		}

		// Get the last heartbeat message to get location info
		if m, ok := getPeerHeartbeatIfRecent(peerInfo); ok {
			heartbeatMessage := new(types.NeuronHeartBeatMsg)
			base64Decoded, _ := base64.StdEncoding.DecodeString(m.Message)
			err = json.Unmarshal([]byte(base64Decoded), &heartbeatMessage)
			if err != nil {
				log.Printf("Failed to parse heartbeat message for %s: %v", sellerPublicKey, err)
				// Continue without location info
			} else {
				// Update seller location from heartbeat
				seller.Lat = heartbeatMessage.Location.Latitude
				seller.Lon = heartbeatMessage.Location.Longitude
			}
		}

		// Add to new sellers map
		newSellersMap[sellerPublicKey] = seller
	}

	// Get current peer IDs from buffers
	currentPeerIDs := make(map[string]peer.ID)
	for peerID := range sellerBuffers.GetBufferMap() {
		peerIDStr := peerID.String()
		currentPeerIDs[peerIDStr] = peerID
	}

	// Process new sellers that weren't there before
	for sellerPublicKey, seller := range newSellersMap {
		// Convert seller's public key to peer ID
		peerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(seller.PublicKey)
		if err != nil {
			log.Printf("Error converting seller public key to peer ID: %v", err)
			continue
		}
		targetPeerID, err := peer.Decode(peerIDStr)
		if err != nil {
			log.Printf("Error decoding peer ID: %v", err)
			continue
		}

		// Check if this seller is already in our buffers
		_, peerHasBuffer := sellerBuffers.GetBuffer(targetPeerID)

		if !peerHasBuffer {
			// New seller - add and process
			log.Printf("Adding new seller: %s", sellerPublicKey)
			processSeller(seller, p2pHost, sellerBuffers, myReachableAddresses, protocolID)
		} else {
			// Seller already exists - do nothing (keep him)
			log.Printf("Seller already exists, keeping: %s", sellerPublicKey)
		}
	}

	// Remove sellers that are no longer in the new list
	for peerIDStr, targetPeerID := range currentPeerIDs {
		// Check if this peer ID corresponds to any of our new sellers
		found := false
		for _, seller := range newSellersMap {
			peerIDStrForSeller, err := keylib.ConvertHederaPublicKeyToPeerID(seller.PublicKey)
			if err != nil {
				continue
			}
			if peerIDStrForSeller == peerIDStr {
				found = true
				break
			}
		}

		if !found {
			// This seller is no longer in the list - remove and disconnect
			log.Printf("Removing seller no longer in list: %s", peerIDStr)

			// Close the connection
			p2pHost.Network().ClosePeer(targetPeerID)

			// Remove from buffers
			sellerBuffers.RemoveBuffer(targetPeerID)
		}
	}

	return nil
}

// ShowCurrentPeerStatus extracts the current sellers from the seller buffers
// by converting peer IDs back to public keys and creating Seller structs
func ShowCurrentPeerStatus(sellerBuffers *commonlib.NodeBuffers) map[Seller]bool {
	currentSellers := make(map[Seller]bool)

	for peerID := range sellerBuffers.GetBufferMap() {
		// Extract public key from peer ID
		pubKey, err := peerID.ExtractPublicKey()
		if err != nil {
			log.Printf("Error extracting public key for peer %s: %v", peerID, err)
			continue
		}

		pubKeyBytes, err := pubKey.Raw()
		if err != nil {
			log.Printf("Error converting public key to bytes for peer %s: %v", peerID, err)
			continue
		}

		// Convert to hex string (this is the Hedera public key format)
		pubKeyStr := fmt.Sprintf("%x", pubKeyBytes)

		// Create a Seller struct with the extracted public key
		seller := Seller{
			PublicKey: pubKeyStr,
			// Note: We don't have location info here, so Lat/Lon will be 0
			// This is acceptable since the main purpose is to identify existing sellers
		}

		currentSellers[seller] = true
		log.Printf("Found current seller: %s (peer ID: %s)", pubKeyStr, peerID.String())
	}

	return currentSellers
}

// ReplaceSellersAuto automatically determines current sellers from buffers and updates them
// with the provided list of seller public keys. This is the most convenient version to use.
func ReplaceSellersAuto(sellerPublicKeys []string, p2pHost host.Host, sellerBuffers *commonlib.NodeBuffers, myReachableAddresses []multiaddr.Multiaddr, protocolID protocol.ID) error {
	// Automatically get current sellers from buffers
	currentSellers := ShowCurrentPeerStatus(sellerBuffers)

	// Use the full ReplaceSellers function with the automatically determined current sellers
	_, err := ReplaceSellers(sellerPublicKeys, currentSellers, p2pHost, sellerBuffers, myReachableAddresses, protocolID)
	return err
}

/*
Usage Examples:

1. ReplaceSellersAuto (Recommended - simplest):
   err := ReplaceSellersAuto(sellerPublicKeys, p2pHost, sellerBuffers, myReachableAddresses, protocolID)

2. ReplaceSellers with automatic current seller detection:
   updatedSellers, err := ReplaceSellers(sellerPublicKeys, nil, p2pHost, sellerBuffers, myReachableAddresses, protocolID)

3. ReplaceSellers with manual current seller management:
   currentSellers := ShowCurrentPeerStatus(sellerBuffers)
   updatedSellers, err := ReplaceSellers(sellerPublicKeys, currentSellers, p2pHost, sellerBuffers, myReachableAddresses, protocolID)

4. ShowCurrentPeerStatus for debugging:
   currentSellers := ShowCurrentPeerStatus(sellerBuffers)
   for seller := range currentSellers {
       fmt.Printf("Current seller: %s\n", seller.PublicKey)
   }
*/
