package sdk

import (
	"context"
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
	buyerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
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
			initBuyer(ctx, p2pHost, protocol, address, peerId)
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

func initBuyer(ctx context.Context, p2pHost host.Host, protocol protocol.ID, address string, peerId peer.ID) {
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

	// Simple message handling loop
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Shutting down buyer...")
			return
		case msg := <-receivedData:
			fmt.Println("Received message from inbound channel", string(msg))
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
