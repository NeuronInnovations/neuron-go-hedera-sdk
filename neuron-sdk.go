package sdk

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	flags "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	neuronbuffers "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	hedera_helper "github.com/NeuronInnovations/neuron-go-hedera-sdk/hedera"
	sdk "github.com/NeuronInnovations/neuron-go-hedera-sdkv2/sdk"
	sdktypes "github.com/NeuronInnovations/neuron-go-hedera-sdkv2/types"
	p2p "github.com/NeuronInnovations/neuron-go-tunnel-sdkv2/p2p"
	p2ptypes "github.com/NeuronInnovations/neuron-go-tunnel-sdkv2/types"
	"github.com/NeuronInnovations/neuron-go-tunnel-sdkv2/whoami"
	"github.com/common-nighthawk/go-figure"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	"github.com/umahmood/haversine"
	"golang.org/x/time/rate"
)

var outboundChannel = make(chan []byte)
var stateChannel = make(chan p2ptypes.ConnectionStateChange)

func init() {
	commonlib.InitFlags()

	commonlib.InitEnv()

	myFigure := figure.NewFigure("NEURON NODE ", "doom", true)
	myFigure.Print()

	// check if the smart contract was loaded in the environment.
	if os.Getenv("smart_contract_address") == "" {
		log.Fatalf("smart_contract_address is not set in the %s file", commonlib.MyEnvFile)
	}
	// check the reachability of the node.
	if (flags.PortFlag == nil) || (*flags.PortFlag == "" || *flags.PortFlag == "0") {
		log.Fatal("port is not set")
	}
}

func LaunchSDK(
	version string,
	protocol protocol.ID,
	keyAndLocationConfigurator func(envIsReady chan bool, envFile string) error,
	buyerCase func(ctx context.Context, p2pHost host.Host, receivedData chan []byte),
	buyerCaseTopicListener func(topicMessage hedera.TopicMessage),
	sellerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
	sellerCaseTopicListener func(topicMessage hedera.TopicMessage),
) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	privateKey, err := loadPrivateKey(os.Getenv("PRIVATE_KEY"))
	if err != nil {
		log.Fatalf("Failed to load private key: %v", err)
	}

	whoami.GetNatInfoAndUpdateGlobals(*commonlib.PortFlag)

	p2pHost, address, peerId := p2p.SetupPeerHost(privateKey, outboundChannel, stateChannel)

	// Create a channel to listen for OS signals at the top level
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create a done channel to coordinate shutdown
	// done := make(chan struct{})

	// Start the appropriate mode in a goroutine
	go func() {
		switch *flags.BuyerOrSellerFlag {
		case "buyer":
			initBuyer(ctx, p2pHost, protocol, address, peerId, buyerCase)
		case "seller":
			initSeller(ctx, p2pHost)
		default:
			log.Panic("unknown buyerOrSellerFlag")
		}
	}()

	fmt.Println("Node is running. Press Ctrl+C to stop.")

	// Wait for interrupt signal
	<-sigChan
	fmt.Println("\nReceived interrupt signal. Shutting down...")
	cancel()

	// Give some time for cleanup
	time.Sleep(500 * time.Millisecond)

	// Exit the program
	os.Exit(0)
}

func initBuyer(ctx context.Context, p2pHost host.Host, protocol protocol.ID, address string, peerId peer.ID, buyerCase func(ctx context.Context, p2pHost host.Host, receivedData chan []byte)) {
	buffers := p2p.NewNodeBuffers()

	addressMultiaddr, err := multiaddr.NewMultiaddr(address)

	if err != nil {
		log.Fatalf("Failed to create multiaddr: %v", err)
	}

	json.Unmarshal([]byte(os.Getenv("location")), &commonlib.MyLocation)

	buyerHederaConfig := sdktypes.Config{
		Version:         string(protocol),
		Mode:            "buyer",
		PeerID:          peerId,
		NatReachability: false,
		Multiaddr:       []multiaddr.Multiaddr{addressMultiaddr},
		ConnectedPeers:  []peer.ID{},
		PhysicalLocation: sdktypes.PhysicalLocation{
			Latitude:  commonlib.MyLocation.Latitude,
			Longitude: commonlib.MyLocation.Longitude,
			Altitude:  commonlib.MyLocation.Altitude,
		},
	}

	buyerHostInfo := sdktypes.HostInfo{
		ID:    buyerHederaConfig.PeerID,
		Addrs: buyerHederaConfig.Multiaddr,
		Peers: buyerHederaConfig.ConnectedPeers,
	}

	stdOutTopic, stdInTopic, stdErrTopic, err := sdk.InitialiseNode(ctx, buyerHederaConfig, buyerHostInfo, handleBuyerStdIn)

	if err != nil {
		log.Fatalf("Failed to initialise node: %v", err)
	}

	buyerTopics := sdktypes.Topics{
		StdOutTopic: *stdOutTopic,
		StdInTopic:  *stdInTopic,
		StdErrTopic: *stdErrTopic,
	}

	fmt.Println("buyerTopics", buyerTopics)

	receivedData := make(chan []byte)

	go p2p.CreateStreamHandler(p2pHost, buffers, protocol, receivedData)

	go buyerCase(ctx, p2pHost, receivedData)

	if *flags.ListOfSellersSourceFlag == "env" {
		var listOfSellersEnvList = os.Getenv("list_of_sellers")

		if listOfSellersEnvList == "" {
			log.Println("list_of_sellers is empty")
			return
		}

		fmt.Println("Loading sellers from env")
	} else {
		fmt.Println("Loading sellers from explorer")

		sellerList := make(map[string]bool)

		go func() {
			var limiter = rate.NewLimiter(5, 1)

			for {
				devices, err := hedera_helper.GetAllDevicesFromExplorer()

				if err != nil {
					log.Println("💀  GetAllPeers error: ", err)
				}

				log.Println("🔎  got ", len(devices), " devices from the explorer")

				center := haversine.Coord{
					Lat: commonlib.MyLocation.Latitude,
					Lon: commonlib.MyLocation.Longitude,
				}

				for _, device := range devices {
					publicKey, publicKeyOk := device["publickey"].(string)
					devicerole, deviceRoleOk := device["devicerole"].(float64)

					// Check if the device has a valid public key and device role
					if !publicKeyOk || !deviceRoleOk {
						continue
					}

					// Check if the device is a seller
					if devicerole != 0 {
						continue
					}

					stdout := device["topic_stdout"].(string)
					stdoutTyped, _ := hedera.TopicIDFromString(stdout)

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

					publicKey = hpub.StringRaw()
					heartbeatMessage := new(commonlib.NeuronHeartBeatMsg)
					base64Decoded, _ := base64.StdEncoding.DecodeString(m.Message)
					err = json.Unmarshal([]byte(base64Decoded), &heartbeatMessage)

					if err != nil {
						continue
					}

					// Check if the radius flag is set, if set then check if the seller is in the radius
					if *flags.RadiusFlag > 0 {
						radius := flags.RadiusFlag

						// calculate the distance using the haversine formula
						farPoint := haversine.Coord{
							Lat: heartbeatMessage.Location.Latitude,
							Lon: heartbeatMessage.Location.Longitude,
						}

						_, distanceKm := haversine.Distance(center, farPoint)

						// check if the distance is less than the radius
						if int(distanceKm) > *radius {
							continue
						}
					}

					fmt.Println("Found valid seller", publicKey)

					if _, ok := sellerList[publicKey]; !ok {
						fmt.Println("Adding seller to list", publicKey)

						sellerList[publicKey] = true

						previousSharedAccountID := ""

						_, err := sdk.ServiceRequest(ctx, string(protocol), buyerHederaConfig.Multiaddr, publicKey, &previousSharedAccountID)

						if err != nil {
							fmt.Println("Error sending service request", err)
						}
					}
				}

				time.Sleep(120 * time.Second)
			}
		}()
	}

	// Simple message handling loop
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Shutting down buyer...")
			return
		// case msg := <-receivedData:
		// 	fmt.Println("Received message from inbound channel", len(msg))
		default:
			// Add a small sleep to prevent CPU spinning
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func handleBuyerStdIn(message hedera.TopicMessage) {
	fmt.Println("Received message from stdIn topic", message)
}

func initSeller(ctx context.Context, p2pHost host.Host) {
}

func loadPrivateKey(keyHex string) (crypto.PrivKey, error) {
	keyBytes, err := hex.DecodeString(keyHex)

	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}

	privKey, err := crypto.UnmarshalSecp256k1PrivateKey(keyBytes)

	if err != nil {
		_, err = crypto.UnmarshalEd25519PrivateKey(keyBytes)

		if err != nil {
			return nil, fmt.Errorf("invalid private key format")
		}

		return nil, fmt.Errorf("Ed25519 keys are not supported; only Secp256k1 keys are allowed")
	}

	return privKey, nil
}
