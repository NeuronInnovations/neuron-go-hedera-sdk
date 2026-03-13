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
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/umahmood/haversine"
	"golang.org/x/time/rate"
)

type Seller struct {
	PublicKey string
	Lat       float64
	Lon       float64
}

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
	constMyReachableAddresses := make([]multiaddr.Multiaddr, len(reachableAddresses))
	copy(constMyReachableAddresses, reachableAddresses)

	sellerBuffers := commonlib.NodeBuffersInstance
	if sellerBuffers == nil {
		sellerBuffers = commonlib.NewNodeBuffers()
	}

	// Keep topic callback lightweight: heavy retry/recovery work goes through
	// a bounded queue so Hedera-related calls cannot block callback progress.
	const recoveryWorkerCount = 3
	recoveryJobs := make(chan Seller, 128)
	var recoveryQueuedMu sync.Mutex
	recoveryQueued := make(map[string]bool) // keyed by seller public key

	for i := 0; i < recoveryWorkerCount; i++ {
		go func() {
			for seller := range recoveryJobs {
				processSeller(seller, p2pHost, sellerBuffers, constMyReachableAddresses)
				recoveryQueuedMu.Lock()
				delete(recoveryQueued, seller.PublicKey)
				recoveryQueuedMu.Unlock()
			}
		}()
	}

	enqueueRecovery := func(publicKey string) {
		publicKey = strings.TrimSpace(publicKey)
		if publicKey == "" {
			return
		}
		recoveryQueuedMu.Lock()
		if recoveryQueued[publicKey] {
			recoveryQueuedMu.Unlock()
			return
		}
		recoveryQueued[publicKey] = true
		recoveryQueuedMu.Unlock()

		select {
		case recoveryJobs <- Seller{PublicKey: publicKey}:
		default:
			// Queue is full; do not block callback path.
			recoveryQueuedMu.Lock()
			delete(recoveryQueued, publicKey)
			recoveryQueuedMu.Unlock()
			log.Printf("recovery queue full, skipping immediate retry for seller %s", publicKey)
		}
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
			// Process payment maintenance asynchronously so it cannot block
			// connect/reconnect message handling in this topic callback path.
			go func(req *commonlib.NeuronScheduleSignRequestMsg, scheduleID hedera.ScheduleID) {
				if err := hedera_helper.SignScheduleBestEffort(scheduleID, os.Getenv("private_key")); err != nil {
					log.Printf("best-effort schedule sign skipped/failed (%d): %v", req.ScheduleID, err)
					return
				}
				if !validatorLib.IsRequestPermitted() {
					return
				}
				sharedAcc, _ := hedera.AccountIDFromString(fmt.Sprintf("0.0.%d", req.SharedAccID))

				accountInfo, balErr := hedera_helper.GetAccountInfoFromNetwork(sharedAcc)
				if balErr == nil && accountInfo.Balance.AsTinybar() >= 10_000_000 {
					fmt.Printf("Shared account %s has sufficient balance (%d tinybars), skipping deposit\n",
						sharedAcc, accountInfo.Balance.AsTinybar())
					return
				}

				fmt.Println("Adding funds to shared account:", sharedAcc)
				if depErr := hedera_helper.DepositToSharedAccountBestEffort(sharedAcc, 0.1); depErr != nil {
					log.Printf("best-effort deposit skipped/failed (%s): %v", sharedAcc, depErr)
				}
			}(scheduleSignRequest, sid)
		case "peerError": // error from seller
			sellerError := new(commonlib.NeuronPeerErrorMsg)
			err := json.Unmarshal(topicMessage.Contents, &sellerError)
			if err != nil {
				fmt.Println("Error un marshalling message service response message", err)
				return
			}
			switch sellerError.ErrorType {
			case commonlib.DialError:
				// get the public key of th sender
				///peerIDStr := keylib.ConvertHederaPublicKeyToPeerID(acc.PublicKey)
				//p2pHost.Network().ClosePeer(peer.ID(peerIDStr))
				//log.Println("Response to dial error cleaning streams to: ", peer.ID(peerIDStr))
			case commonlib.FlushError:
			case commonlib.DisconnectedError:
			case commonlib.NoKnownAddressError:
			case commonlib.HeartBeatError:
			case commonlib.BalanceError:
			case commonlib.VersionError:
			case commonlib.WriteError:
				fmt.Println("Write error: ", sellerError)
				switch sellerError.RecoverAction {
				case commonlib.SendFreshHederaRequest:
					// Queue recoveries; keep topic callback non-blocking.
					enqueueRecovery(sellerError.PublicKey)
				case commonlib.PunchMe:
				case commonlib.DoNothing:
				}
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

	var (
		listOfSellers     = make(map[Seller]bool)
		listOfSellersLock sync.RWMutex
	)

	// Buffered so an early startup signal (env/explorer path) is not lost
	// before the worker goroutine begins receiving.
	startSecondLoop := make(chan struct{}, 1)

	// if the list of sellers source is the environment file the we get it from there (otherwise ask explorer)
	if *flags.ListOfSellersSourceFlag == "env" {
		var listOfSellersEnvList = os.Getenv("list_of_sellers")
		if listOfSellersEnvList == "" {
			log.Println("list_of_sellers is empty")
			return
		}
		// Env source is explicit and static: avoid repeated sequential contract scans
		// that can choke runtime add/connect flows.
		for _, seller := range strings.Split(listOfSellersEnvList, ",") {
			seller = strings.TrimSpace(seller)
			if seller == "" {
				continue
			}
			listOfSellersLock.Lock()
			listOfSellers[Seller{PublicKey: seller}] = true
			listOfSellersLock.Unlock()
		}
		select {
		case startSecondLoop <- struct{}{}:
		default:
		}
	} else { // if the flag list-of-sellers is not set to env then get it from the explorer
		go func() {
			var limiter = rate.NewLimiter(5, 1)
			const explorerWorkerCount = 8
			for {
				// get the list of devices from the explorer  every 120 seconds
				devices, err := hedera_helper.GetAllDevicesFromExplorer()
				if err != nil {
					log.Println("💀  GetAllPeers error: ", err)
					// Keep explorer discovery fully non-blocking: avoid Hedera write-path
					// side effects from this background task.
					time.Sleep(10 * time.Second)
					continue
				}

				log.Println("🔎  got ", len(devices), " devices from the explorer")
				discovered := make(map[Seller]bool)
				var discoveredMu sync.Mutex
				jobs := make(chan map[string]interface{}, len(devices))
				var wg sync.WaitGroup

				worker := func() {
					defer wg.Done()
					for device := range jobs {
						publicKey, ok := device["publickey"].(string)
						if !ok {
							continue
						}
						devicerole, ok := device["devicerole"].(float64)
						if !ok || devicerole != 0 { // seller device only
							continue
						}
						stdout, ok := device["topic_stdout"].(string)
						if !ok || strings.TrimSpace(stdout) == "" {
							continue
						}
						stdoutTyped, err := hedera.TopicIDFromString(stdout)
						if err != nil {
							continue
						}

						// Global mirror pacing with per-job timeout.
						waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
						waitErr := limiter.Wait(waitCtx)
						waitCancel()
						if waitErr != nil {
							continue
						}

						m, lastMessageError := getLastMessageFromTopicWithTimeout(stdoutTyped, 4*time.Second)
						if lastMessageError != nil {
							continue
						}
						if m.Timestamp.IsZero() || m.Timestamp.Before(time.Now().Add(-10*time.Minute)) {
							continue
						}
						hpub, err := hedera.PublicKeyFromString(publicKey)
						if err != nil {
							continue
						}

						publicKey = hpub.StringRaw()
						heartbeatMessage := new(commonlib.NeuronHeartBeatMsg)
						base64Decoded, _ := base64.StdEncoding.DecodeString(m.Message)
						err = json.Unmarshal([]byte(base64Decoded), &heartbeatMessage)
						if err != nil {
							continue
						}

						seller := Seller{
							PublicKey: publicKey,
							Lat:       heartbeatMessage.Location.Latitude,
							Lon:       heartbeatMessage.Location.Longitude,
						}

						if *flags.RadiusFlag > 0 {
							centerLat := commonlib.MyLocation.Latitude
							centerLon := commonlib.MyLocation.Longitude
							radius := flags.RadiusFlag
							center := haversine.Coord{Lat: centerLat, Lon: centerLon}
							farPoint := haversine.Coord{Lat: seller.Lat, Lon: seller.Lon}
							_, distanceKm := haversine.Distance(center, farPoint)
							if int(distanceKm) >= *radius {
								continue
							}
						}

						discoveredMu.Lock()
						discovered[seller] = true
						discoveredMu.Unlock()
					}
				}

				for i := 0; i < explorerWorkerCount; i++ {
					wg.Add(1)
					go worker()
				}
				for _, device := range devices {
					jobs <- device
				}
				close(jobs)
				wg.Wait()

				if len(discovered) > 0 {
					listOfSellersLock.Lock()
					for seller := range discovered {
						listOfSellers[seller] = true
					}
					listOfSellersLock.Unlock()
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

			// Process sellers concurrently so one slow/rate-limited seller lookup
			// does not stall all other sellers in the cycle.
			const maxConcurrentSellerWorkers = 3
			sem := make(chan struct{}, maxConcurrentSellerWorkers)
			var wg sync.WaitGroup
			for _, seller := range sellersCopy {
				wg.Add(1)
				sem <- struct{}{}
				go func(s Seller) {
					defer wg.Done()
					defer func() { <-sem }()
					processSeller(s, p2pHost, sellerBuffers, constMyReachableAddresses)
				}(seller)
			}
			wg.Wait()

			time.Sleep(60 * time.Second)
		} // end for
	}()

}

func getLastMessageFromTopicWithTimeout(topicID hedera.TopicID, timeout time.Duration) (hedera_helper.HCSMessage, error) {
	type result struct {
		m   hedera_helper.HCSMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		m, err := hedera_helper.GetLastMessageFromTopic(topicID)
		ch <- result{m: m, err: err}
	}()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-time.After(timeout):
		return hedera_helper.HCSMessage{}, fmt.Errorf("timeout waiting for mirror topic %s", topicID)
	}
}

func prepareServiceRequestMsg(seller string, myReachableAddresses []multiaddr.Multiaddr) (commonlib.TopicPostalEnvelope, error) {
	res, err := hedera_helper.BuyerPrepareServiceRequest(
		myReachableAddresses, //HostsPublicAddressesSorted(p2pHost)[0],
		os.Getenv("hedera_evm_id"),
		keylib.ConverHederaPublicKeyToEthereunAddress(seller),
		"e2436b1e019e993215e832762f9242020d199940", // that's the london address, yes; it's fixed for now but a parameter in env MyArbiterPublicKey in the future.
		100, // millibar (0.1 HBAR) - initial balance
	)
	if err != nil {
		return commonlib.TopicPostalEnvelope{}, err
	}
	return *res, err
}

func processSeller(seller Seller, p2pHost host.Host, sellerBuffers *commonlib.NodeBuffers, myReachableAddresses []multiaddr.Multiaddr) {
	sellerEvnAddress := keylib.ConverHederaPublicKeyToEthereunAddress(seller.PublicKey)
	peerIDStr := keylib.ConvertHederaPublicKeyToPeerID(seller.PublicKey)
	peerID, _ := peer.Decode(peerIDStr)

	peerBuffer, peerHasBuffer := sellerBuffers.GetBuffer(peerID)

	if peerHasBuffer && !peerBuffer.IsOtherSideValidAccount {
		fmt.Printf("skipping invalid seller: evm address %s ", sellerEvnAddress)
		return
	}

	if peerHasBuffer && peerBuffer.NextScheduleRequestTime.After(time.Now()) {
		return
	}

	//TODO: check if the remote peer has a heartbeat in the past 5 minutes

	conns := p2pHost.Network().ConnsToPeer(peerID)

	if len(conns) == 0 {
		if !peerHasBuffer { // no cons and never requested
			// Initial attempt: check contract availability once before sending.
			perrInfo, err := hedera_helper.GetPeerInfo(sellerEvnAddress)
			if err != nil {
				return
			}
			if !perrInfo.Available {
				return
			}
			envelope, setupErr := prepareServiceRequestMsg(seller.PublicKey, myReachableAddresses)
			if setupErr != nil {
				sellerBuffers.AddBuffer2(peerID, envelope, false, commonlib.NotInitiated, neuronbuffers.LibP2PState(commonlib.BadMessageError))
				sellerBuffers.SetPeerPublicKey(peerID, seller.PublicKey)
				log.Printf("💀 envelope setup error; seller %s will be blacklisted, err: %v \n", sellerEvnAddress, setupErr)
				hedera_helper.SendSelfErrorMessage(neuronbuffers.BadMessageError, "Could not create envelope for: "+sellerEvnAddress, commonlib.DoNothing)
				return
			}

			sellerBuffers.IncrementReconnectAttempts(peerID)
			if execErr := hedera_helper.SendTransactionEnvelope(envelope); execErr != nil {
				// has errors
				sellerBuffers.AddBuffer2(peerID, envelope, true, commonlib.SendFail, commonlib.Connecting)
				sellerBuffers.SetPeerPublicKey(peerID, seller.PublicKey)
				log.Printf("💀 send hedera transaction envelope error %s, will allow to try later %v \n", sellerEvnAddress, execErr)
				hedera_helper.SendSelfErrorMessage(neuronbuffers.ServiceError, "Could not send the reqquest to: "+sellerEvnAddress, commonlib.DoNothing)
				return
			}
			// has no errors
			sellerBuffers.AddBuffer2(peerID, envelope, true, commonlib.SendOK, commonlib.Connecting)
			sellerBuffers.SetPeerPublicKey(peerID, seller.PublicKey)

		} else { // have buffer,  no cons and requested before: re-submit for up to backoff
			if isTooEarly, _ := commonlib.IsRequestTooEarly(sellerBuffers, peerID); isTooEarly {
				return
			} else {
				a, b, _ := sellerBuffers.GetReconnectInfo(peerID)
				log.Println("re-submit because it's time now", isTooEarly, a, b)
			}
			sellerBuffers.IncrementReconnectAttempts(peerID)
			secondExecError := hedera_helper.SendTransactionEnvelopeBestEffort(peerBuffer.RequestOrResponse)
			if secondExecError != nil {
				log.Printf("💀-2  skip that seller %s because ExecuteHederaTransaction error: %v", sellerEvnAddress, secondExecError)
				// TODO: 💥 tell to myself that I could not send the transaction to the other side
				hedera_helper.SendSelfErrorMessage(neuronbuffers.ServiceError, "Could not send the reqquest to: "+sellerEvnAddress, commonlib.DoNothing)
				sellerBuffers.UpdateBufferRendezvousState(peerID, commonlib.SendFail)
				sellerBuffers.UpdateBufferLibP2PState(peerID, commonlib.CanNotConnectUnknownReason)
				return
			}
			sellerBuffers.UpdateBufferRendezvousState(peerID, commonlib.SendOK)
			sellerBuffers.UpdateBufferLibP2PState(peerID, commonlib.Connecting)
		}
	} else { // there are cons
		if !peerHasBuffer {
			// Do not aggressively close from buyer side. A can have late/valid inbound
			// streams while buffer state catches up; force-closing here causes random drops.
			fmt.Println("Connected but no buffer yet; keeping connection open and waiting", peerID)
			return
		}

		streams := 0
		//log.Println("This one has cons", conns)
		for _, conn := range conns {
			streams += len(conn.GetStreams())
		}
		if streams == 0 { // there are cons but no streams
			// Keep the connection open and allow stream establishment/recovery.
			log.Println("Connected but no streams yet; keeping peer open", peerID, streams)
			return
		} else {
			sellerBuffers.UpdateBufferRendezvousState(peerID, commonlib.SendOK)
			sellerBuffers.UpdateBufferLibP2PState(peerID, commonlib.Connected)
			sellerBuffers.SetLastOtherSideMultiAddress(peerID, conns[0].RemoteMultiaddr())

		}
	} // end if there are conns
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
