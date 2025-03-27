package sdk

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	neuronbuffers "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"

	flags "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"

	streambuyervsseller "github.com/NeuronInnovations/neuron-go-hedera-sdk/dapp-protocols/stream-buyer-vs-seller"
	hedera_helper "github.com/NeuronInnovations/neuron-go-hedera-sdk/hedera"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"

	"runtime"
	"runtime/debug"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/whoami"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p"

	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/common-nighthawk/go-figure"
	"github.com/joho/godotenv"
	"github.com/libp2p/go-libp2p/config"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	quictransprt "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var fixPrivKey_g crypto.PrivKey

var Version string

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

	whoami.GetNatInfoAndUpdateGlobals(flags.PortFlag)
}

// LaunchSDK serves as the entry point to the Neuron SDK, initializing core components,
// including state management, P2P connectivity, and protocol-specific configurations.
//
// The SDK operates in different roles, such as buyer, seller, or relay, as determined
// by the flags provided at runtime. One critical aspect of buyer and seller roles is
// the interaction with Hedera topics for communication and coordination.
//
// **Topic Listeners**:
// The SDK requires the implementor to define and provide topic listeners (`buyerCaseTopicListener`
// and `sellerCaseTopicListener`). These listeners are responsible for monitoring messages sent
// to their respective `stdInTopic` on Hedera. Each listener effectively acts as an "ear" for
// its associated role, processing messages sent by peers to coordinate activities like
// data exchange, payment requests, and error handling.
//
// **Purpose of Listeners**:
//   - The `buyerCaseTopicListener` listens to the buyer's `stdInTopic`, capturing messages such as
//     data offers, responses from sellers, or system-level notifications.
//   - The `sellerCaseTopicListener` listens to the seller's `stdInTopic`, processing incoming
//     requests from buyers and managing interactions related to data sales.
//
// These listeners must be explicitly implemented by the developer integrating the SDK. The callbacks
// create the listeners, establishing the link between Hedera topics and in-app functionality.
//
// **Future Extension**:
// Beyond simple listening, topic listeners may evolve to include message validation and reactive
// behaviors that adjust based on system state or predefined business rules. While the SDK provides
// connectivity-level validation (e.g., NAT traversal, peer discoverability), the implementor will
// eventually be required to handle application-specific validation and logic at the dApp level.
func LaunchSDK(
	version string,
	protocol protocol.ID,
	keyAndLocationConfigurator func(envIsReady chan bool, envFile string) error,
	buyerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
	buyerCaseTopicListener func(topicMessage hedera.TopicMessage),
	sellerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
	sellerCaseTopicListener func(topicMessage hedera.TopicMessage),
	// validatorCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
) {
	ctx := context.Background()
	Version = version
	commonlib.MyProtocol = protocol

	// enable persistence. TODO: use a flag to choose if you want to disable it. This is useful for stateless setups.
	commonlib.StateManagerInit(*commonlib.BuyerOrSellerFlag, *commonlib.ClearCacheFlag)

	// private keys are coming from the environment. Location is coming either from the force flag or the env variable. If any of them is missing
	// then an external fallback configurator will be used. This can be an UI, a wallet, etc.
	fixPrivKey, err := SetupKeysAndLocation(commonlib.MyEnvFile, flags.ForceLocationFlag, keyAndLocationConfigurator)
	if err != nil {
		log.Fatal(err)
	}
	fixPrivKey_g = fixPrivKey
	rawPublicKey, _ := fixPrivKey.GetPublic().Raw()
	fmt.Printf("My private key type is: %v\nMy public key is: %x\n", fixPrivKey.Type(), rawPublicKey)
	pk, _ := hedera.PublicKeyFromString(hex.EncodeToString(rawPublicKey))
	commonlib.MyPublicKey = pk
	fmt.Println("Neuron  host is starting ...")

	// Configuring a Libp2p host with dynamic options based on mode (relay or peer).
	// - Relay mode: Enables the host to act as a relay, allowing traffic to be relayed through it. Relay is WIP.
	// - Peer mode: Sets up the host for direct connections, including NAT traversal and private key-based identity.
	// Public or discovered NAT addresses are dynamically advertised using a custom AddrsFactory. Once options are set,the host is
	// started using the createHost function.
	var p2pHost host.Host

	options := []libp2p.Option{
		libp2p.Transport(quictransprt.NewTransport),

		//libp2p.Security(noise.ID, noise.New),
		libp2p.AddrsFactory(func(m []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			// advertise only public addresses for now.  TODO: reintroduce local addresses
			filtered := multiaddr.FilterAddrs(m, manet.IsPublicAddr)
			if flags.MyPublicIpFlag != nil && *flags.MyPublicIpFlag != "" && flags.MyPublicPortFlag != nil && *flags.MyPublicPortFlag != "" {
				externalAddrUDP, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%s/quic-v1", *flags.MyPublicIpFlag, *flags.MyPublicPortFlag))
				filtered = append(filtered, externalAddrUDP)
			} else {
				discoveredAddrUDP, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", whoami.NatIPAddress, whoami.NatPort))
				filtered = append(filtered, discoveredAddrUDP)
			}
			return filtered
		}),
	}

	// The node's operating mode (relay or peer) is determined by the value of the "--mode" flag.
	//
	// - Relay Mode: In this mode, the node acts as a relay to assist peers behind strict NATs
	//   or firewalls, routing their traffic through the relay. This is particularly useful in
	//   cases where peers cannot establish direct connections. The relay does not act as a buyer
	//   or seller but simply facilitates communication between other nodes.
	//
	// - Peer Mode: In this mode, the node directly participates in the network, either as a
	//   "buyer" or a "seller." The buyer initiates data requests and processes incoming data,
	//   while the seller responds to those requests and serves data. Both roles involve handling
	//   NAT traversal and maintaining peer-to-peer connections.
	//
	// The selected mode depends on the value of the flag, and the appropriate configuration
	// and behavior for relay or peer mode is applied accordingly.
	// The following sections explain the buyer and seller cases in detail.
	switch *flags.PeerOrRelayFlag {
	case "relay": // ================================== RELAY MODE
		// A relay node is intended to assist peers that are behind strict NATs (Network Address Translators)
		// or firewalls, enabling them to communicate by routing their traffic through the relay.
		// Note: This functionality is currently not implemented yet and is a work in progress (WIP).
		f := figure.NewFigure("relay", "doom", true)
		f.Print()
		options = append(options,
			libp2p.RandomIdentity,
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/udp/%s/quic-v1", *flags.PortFlag)), //fmt.Sprintf("/ip4/127.0.0.1/tcp/%s/ws", *portFlag),
			libp2p.EnableRelay(),        // i am willing to use other nodes to relay ny comms via them
			libp2p.EnableRelayService(), // i am willing to allow other nodes to use me to relay comms via me
		)
		fmt.Println("...starting in relay mode with ", len(options), "options")

		p2pHost = createHost(options)
		defer p2pHost.Close()

		// TODO: expand block to handle NAT events such as change in public IP addresses
		evbus, _ := p2pHost.EventBus().Subscribe(event.WildcardSubscription)
		go func() {
			for {
				getOne := <-evbus.Out()
				fmt.Printf("evbus.Out(): %v\n", getOne)
			}
		}()

		remoteInfo := peer.AddrInfo{
			ID:    p2pHost.ID(),
			Addrs: p2pHost.Network().ListenAddresses(),
		}
		remoteAddrs, _ := peer.AddrInfoToP2pAddrs(&remoteInfo)
		fmt.Println("... listening and relaying", remoteAddrs)

	case "peer": // ================================== PEER MODE
		// In peer mode, we launch the first style of operation: Buyer vs Seller (more operational modes are coming soon).
		//
		// - Buyer vs Seller: This mode defines callbacks that an SDK user needs to implement to specify the behavior of the buyer
		//   and the seller. The role of the node (buyer or seller) is determined by a flag (`--buyer-or-seller`), and you cannot
		//   run in both modes simultaneously (run the node once as a buyer and another time as a seller).
		//
		//   This mode also defines a protocol where the seller provides a data stream to sell, and the buyer subscribes to
		//   purchase it. The seller periodically requests payment for the data, and the buyer is responsible for fulfilling
		//   those payment requests.
		//
		// - Startup Procedures: Depending on whether you run as a buyer or seller, different startup procedures occur, which
		//   will be explained soon. However, some common behaviors are shared by both modes:
		//
		//   1. Listening to Hedera Topic Messages: Both roles interact with Hedera topics to exchange information.
		//   2. Handling Communication Errors: Any communication error, such as invalid messages or connectivity issues, is handled appropriately.
		//   3. Managing Peer Connectivity: Both buyer and seller roles rely on the Hedera network for rendezvous. They exchange part encrypted messages
		//      to discover and share IP addresses, and then establish a permanent P2P stream for data transfer.
		//
		// - Message Flow: Whether you are a buyer or seller, different types of messages are sent and received depending on the situation. Both roles
		//   are designed to catch error messages sent by their counterparts. If an error message is not explicitly handled, it is passed on to the SDK user.
		//
		// - SDK Message Handling: The primary message type passed to the SDK user is "serviceError." Any other undefined message is also forwarded to the SDK user for further handling.
		//
		// This ensures that SDK users have a clear point of interaction with unhandled errors while maintaining robust internal mechanisms for peer-to-peer connectivity, data exchange, and payment management.
		f := figure.NewFigure("peer", "alphabet", true)
		f.Print()
		fmt.Println("...starting in peer mode")
		options = append(options,
			libp2p.Identity(fixPrivKey_g),
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/udp/%s/quic-v1/", *flags.PortFlag)),
			libp2p.EnableHolePunching(),
		)
		p2pHost = createHost(options)
		defer p2pHost.Close()

		// TODO: expand block to handle NAT events such as change in public IP addresses
		evbus, _ := p2pHost.EventBus().Subscribe(event.WildcardSubscription)
		go func() {
			for {
				getOne := <-evbus.Out()
				switch e := getOne.(type) {
				case event.EvtLocalReachabilityChanged:
					log.Println("local reachability changed", e.Reachability.String())
				case event.EvtNATDeviceTypeChanged:
					log.Println("nat device type changed", e.TransportProtocol.String(), e.NatDeviceType.String())
				case event.EvtPeerConnectednessChanged:
					log.Println("peer connectednes", e.Connectedness.String())
				case event.EvtPeerIdentificationCompleted:
					log.Println("peer identification completed", e.Peer)

				default:
					log.Println("unknown event", e)
				}
			}
		}()

		stdOutTopic, stdInTopic, stdErrTopic, err := hederaAnnounceAndHeartBeat(ctx, p2pHost)
		if err != nil {
			log.Panic(err)
		}

		commonlib.MyStdIn = stdInTopic
		commonlib.MyStdOut = stdOutTopic
		commonlib.MyStdErr = stdErrTopic

		fmt.Println("Finished announcing to hedera. MyStdIn: ", commonlib.MyStdIn, " MyStdOut: ", commonlib.MyStdOut, " MyStdErr: ", commonlib.MyStdErr)

		// BuyerVsSellerApp is the first style of agent communication pattern; when more modes are added, this will need to be refactored
		go launchBuyerVersusSellerApp(ctx, p2pHost, protocol, buyerCase, buyerCaseTopicListener, sellerCase, sellerCaseTopicListener)

	default:
		log.Panic("Unknown mode: ", *flags.PeerOrRelayFlag)
	} // switch end

	fmt.Printf("The node is up and tho peerID is %v\n", p2pHost.ID())

	// Monitor connection events
	p2pHost.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, c network.Conn) {
			log.Printf("Connected to %s", c.RemotePeer())
		},
		DisconnectedF: func(n network.Network, c network.Conn) {
			log.Printf("Disconnected from %s", c.RemotePeer())
		},
	})

	// keep the node running until a keyboard interrupt
	keyboardCancelChannel := make(chan os.Signal, 1)
	signal.Notify(keyboardCancelChannel, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("Press ctrl+c to kill the libp2p node")

	<-keyboardCancelChannel

	fmt.Println("Received keyboard signal, shutting  down the libp2p node ...")
	// shut the node down
	if err := p2pHost.Close(); err != nil {
		panic(err)
	}
}

func launchBuyerVersusSellerApp(
	ctx context.Context,
	p2pHost host.Host,
	protocol protocol.ID,
	buyerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
	buyerCaseTopicCallBack func(topicMessage hedera.TopicMessage),
	sellerCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),
	sellerCaseTopicCallBack func(topicMessage hedera.TopicMessage),
	// validatorCase func(ctx context.Context, p2pHost host.Host, buffers *neuronbuffers.NodeBuffers),

) {
	switch *flags.BuyerOrSellerFlag {
	case "buyer":
		streambuyervsseller.HandleBuyerCase(ctx, p2pHost, buyerCase, buyerCaseTopicCallBack)
	case "seller":
		streambuyervsseller.HandleSellerCase(ctx, p2pHost, protocol, sellerCase, sellerCaseTopicCallBack)
	case "validator":
		// streambuyervsseller.HandleValidatorCase(ctx, p2pHost, protocol, validatorCase)
	default:
		log.Panic("unknown buyerOrSellerFlag")
	}

}

func createHost(options []config.Option) host.Host {
	p2pHost, hostError := libp2p.New(options...)

	if hostError != nil {
		panic(hostError)
	} else {
		fmt.Printf(
			"This host's identity \n\tMy host ID:%s\n\t p2pHost.Addrs():%s  \n listening on:%s  \n",
			p2pHost.ID(), p2pHost.Addrs(), p2pHost.Network().ListenAddresses(),
		)
	}
	return p2pHost
}

func getAbbreviatedPeerPublicKeys(p2pHost host.Host) []string {
	var abbreviatedKeys []string
	connectedPeers := p2pHost.Network().Peers()

	for _, peerID := range connectedPeers {

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

		pubKeyStr := fmt.Sprintf("%x", pubKeyBytes)

		ethAddress := keylib.ConverHederaPublicKeyToEthereunAddress(pubKeyStr)

		// Abbreviate the public key to the first 4 characters
		if len(pubKeyStr) >= 4 {
			abbreviatedKeys = append(abbreviatedKeys, ethAddress[len(ethAddress)-4:])
		} else {
			abbreviatedKeys = append(abbreviatedKeys, ethAddress) // In case the public key is shorter than 4 characters
		}
	}

	return abbreviatedKeys
}

// hederaAnnounceAndHeartBeat connects to the Hedera network, ensures the necessary topics are created,
// and initiates periodic heartbeat transmissions to indicate the node's presence and status.
//
// The function first interacts with the Hedera network to verify and create required topics using
// the EnsureTopicsAndNotifyContract method. This step is critical for initializing communication
// channels and ensuring synchronization with the smart contract governing network interactions.
//
// Once topics are created, the function spawns a goroutine to send regular heartbeat messages to the
// Hedera topic. These messages encapsulate vital node metadata, including its geographic location,
// NAT reachability, operating role (buyer or seller), software version, and an abbreviated list of
// connected peers' identifiers. The heartbeat ensures the node remains discoverable and responsive
// within the network.
//
// **Future Direction**:
// While the current implementation relies on a fixed periodic interval for heartbeat transmission
// (40 seconds), future iterations will transition to a **reactive notification model**. Under this
// model, heartbeat messages will be triggered dynamically by significant state changes, such as
// updates to public IP addresses, NAT reachability, or peer connectivity. This approach aims to
// reduce network overhead by eliminating redundant transmissions and aligning more closely with
// real-time status updates.
//
// Despite adopting a reactive design, a less frequent baseline heartbeat will still be maintained
// to account for scenarios where no state changes occur over extended periods. This will act as a
// "keep-alive" mechanism, ensuring that nodes remain visible and their metadata remains up-to-date
// in the network directory.
//
// In network engineering terms, the evolution towards a hybrid **event-driven status broadcast**
// coupled with a low-frequency **heartbeat beacon** aligns with modern decentralized networking
// strategies. It balances efficiency, reliability, and the need for timely updates, minimizing
// unnecessary traffic while preserving fault-tolerant node discovery and reachability.
func hederaAnnounceAndHeartBeat(ctx context.Context, p2pHost host.Host) (hedera.TopicID, hedera.TopicID, hedera.TopicID, error) {

	fmt.Println("connect to hedera and send periodic alive")
	stdOutTopic, stdInTopic, stdErrTopic, topciCreationError := hedera_helper.EnsureTopicsAndNotifyContract(p2pHost)

	if topciCreationError != nil {
		log.Fatal("Failed to create hedera topics. Let's end it here. ", topciCreationError)
	}

	go func() {
		for {
			peers := p2pHost.Network().Peers()
			myPublicAddressesEverything := hostsPublicAddressesSorted(p2pHost)
			if len(myPublicAddressesEverything) > 0 {
				json.Unmarshal([]byte(os.Getenv("location")), &commonlib.MyLocation)
				heartbeatMessage := commonlib.NeuronHeartBeatMsg{
					MessageType:        "NeuronHeartBeat",
					Location:           commonlib.MyLocation,
					NatDeviceType:      "", //natDeviceType_g,
					NatReachability:    whoami.NatReachability,
					BuyerOrSeller:      *flags.BuyerOrSellerFlag,
					Version:            Version,
					ConnectedPeersAbrv: getAbbreviatedPeerPublicKeys(p2pHost),
				}
				// Marshal the struct to JSON
				heartbeatJSON, err := json.Marshal(heartbeatMessage)
				if err != nil {
					log.Fatalf("Error marshaling heartbeat message to JSON: %v", err)
				}
				// Convert JSON bytes to a string
				heartbeatString := string(heartbeatJSON)

				// Send the JSON message to the specified topic
				err = hedera_helper.SendToTopic(stdOutTopic, heartbeatString)
				if err != nil {
					log.Printf("Error sending heartbeat message to topic: %v", err)
				}

			} else {
				log.Printf("I have no addresses in the p2pHost... and trying to get it from %d peers", len(peers))
			}
			time.Sleep(time.Second * 40)
		}
	}()
	return stdOutTopic, stdInTopic, stdErrTopic, topciCreationError
}

func hostsPublicAddressesSorted(p2pHost host.Host) []multiaddr.Multiaddr {
	myPublicAddressesEverything := p2pHost.Addrs()
	if len(myPublicAddressesEverything) > 0 {
		myPublicAddresses := multiaddr.FilterAddrs(myPublicAddressesEverything, manet.IsPublicAddr)
		sort.Slice(myPublicAddresses, func(i, j int) bool {
			return len(myPublicAddresses[i].String()) < len(myPublicAddresses[j].String())
		})
	}
	return myPublicAddressesEverything
}

func monitorMemory() {
	for range time.Tick(5 * time.Second) {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		if stats.HeapSys/1024/1024 > 50 {
			runtime.GC()
			debug.FreeOSMemory()
		}
	}
}

// SetupKeysAndLocation initializes the keys and location based on flags and environment variables.
func SetupKeysAndLocation(envFile string, forceLocationFlag *string, configurator func(envIsReady chan bool, envFile string) error) (crypto.PrivKey, error) {

	// Load location from the force flag or environment variable
	locationError := LoadLocation(forceLocationFlag, &commonlib.MyLocation)
	if locationError != nil {
		return HandleFallbackConfigurator(envFile, configurator)
	}

	// Decode the private key from the environment variable
	privKey, err := LoadPrivateKey(os.Getenv("private_key"))
	if err != nil {
		fmt.Println("We could not load your key, maybe it is in DER format? Try to un-DER. We are proceeding to load the configurator (if you have one)")

		return HandleFallbackConfigurator(envFile, configurator)
	}

	return privKey, nil
}

// LoadLocation tries to load the location JSON into the provided object.
func LoadLocation(forceLocationFlag *string, location *commonlib.EnvironmentVarLocation) error {
	if *forceLocationFlag != "" {
		return json.Unmarshal([]byte(*forceLocationFlag), location)
	}
	return json.Unmarshal([]byte(os.Getenv("location")), location)
}

// LoadPrivateKey decodes the private key and validates it.
func LoadPrivateKey(keyHex string) (crypto.PrivKey, error) {
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

// HandleFallbackConfigurator attempts to use the configurator to recover from errors.
func HandleFallbackConfigurator(envFile string, configurator func(envIsReady chan bool, envFile string) error) (crypto.PrivKey, error) {
	uiChannelEnvReady := make(chan bool)
	if configurator == nil {
		log.Panic("The configurator is nil, exiting")
	}
	configurator(uiChannelEnvReady, envFile)

	if envIsReady := <-uiChannelEnvReady; !envIsReady {
		return nil, fmt.Errorf("failed to initialize environment")
	}

	// Reload configuration
	if err := godotenv.Overload(envFile); err != nil {
		return nil, fmt.Errorf("error reloading environment file: %w", err)
	}

	// Retry location and key setup
	if err := json.Unmarshal([]byte(os.Getenv("location")), &commonlib.MyLocation); err != nil {
		return nil, fmt.Errorf("failed to load location after configurator: %w", err)
	}
	return LoadPrivateKey(os.Getenv("private_key"))
}
