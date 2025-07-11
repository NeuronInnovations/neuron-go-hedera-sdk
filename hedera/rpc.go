package hedera_helper

import (
	"bytes"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/host"
)

func EnsureTopicsAndNotifyContract(p2pHost host.Host) (hedera.TopicID, hedera.TopicID, hedera.TopicID, error) {

	hostPubKey, err := p2pHost.ID().ExtractPublicKey()
	if err != nil {
		log.Panic("The peer must have a public key but we couldn't get it: ", err)
	}
	hostPubKeyByte, err := hostPubKey.Raw()

	if err != nil {
		log.Panic("The peer must have a public key but we couldn't get it: ", err)
	}
	hostPubKeyStr := common.Bytes2Hex(hostPubKeyByte)

	toEthAddress := keylib.ConverHederaPublicKeyToEthereunAddress(string(hostPubKeyStr))

	peerInfo, err := GetPeerInfo(toEthAddress)
	if err != nil {
		log.Panic("We must be able to  correctly talk to the smart contract to continue;\n perhaps you are pointing to the wrong contract \n or your address doesn't exist, err:", err)
	} else {
		// check if peerInfo has data
		if peerInfo.StdInTopic != 0 && peerInfo.StdOutTopic != 0 {
			// return the topics
			stdOutTopicID, _ := hedera.TopicIDFromString(fmt.Sprintf("0.0.%d", peerInfo.StdOutTopic))
			stdInTopicID, _ := hedera.TopicIDFromString(fmt.Sprintf("0.0.%d", peerInfo.StdInTopic))
			stdErrTopicID, _ := hedera.TopicIDFromString(fmt.Sprintf("0.0.%d", peerInfo.StdErrTopic))
			return stdOutTopicID, stdInTopicID, stdErrTopicID, nil
		} else { // branch is disabled, the rest is dead-code]
			var allowSelfRegistration = false
			if allowSelfRegistration {
				freshTopicNum := func(topicName string) (hedera.TopicID, error) {
					var emptyTopicID hedera.TopicID
					c := GetHederaClientUsingEnv()
					defer c.Close()
					transactionResponse, err := hedera.NewTopicCreateTransaction().
						SetTransactionMemo(topicName).
						SetAdminKey(c.GetOperatorPublicKey()).
						Execute(c)

					if err != nil {
						println(err.Error(), ": error creating topic")
						return emptyTopicID, err
					}

					// Get the receipt
					transactionReceipt, err := transactionResponse.GetReceipt(c)
					if err != nil {
						println(err.Error(), ": error getting topic create receipt")
						return emptyTopicID, err
					}

					// Get the topic id from receipt
					topicID := *transactionReceipt.TopicID
					fmt.Printf("topicID: %v - %s\n", topicID, topicName)

					return topicID, err
				}

				stdOutTopicID, err := freshTopicNum("stdout")
				if err != nil {
					return hedera.TopicID{}, hedera.TopicID{}, hedera.TopicID{}, err
				}
				stdInTopicID, err := freshTopicNum("stdin")
				if err != nil {
					return hedera.TopicID{}, hedera.TopicID{}, hedera.TopicID{}, err
				}
				stdErrTopicID, err := freshTopicNum("stderr")

				newContractID, err := hedera.ContractIDFromString(os.Getenv("smart_contract_id"))
				if err != nil {
					println(err.Error(), ": error finding that smart contract")
					return hedera.TopicID{}, hedera.TopicID{}, hedera.TopicID{}, err
				}
				fmt.Println("newContractID: ", newContractID.EvmAddress)

				c := GetHederaClientUsingEnv()
				defer c.Close()
				peerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(string(hostPubKeyStr))
				if err != nil {
					log.Panic("Error converting host public key to peer ID: ", err)
				}
				callResult, err := hedera.NewContractExecuteTransaction().
					SetContractID(newContractID).
					SetTransactionMemo("broadcast liveness topic for self").
					SetGas(500000).
					SetFunction("putPeerAvailableSelf", hedera.NewContractFunctionParameters().
						AddUint64(stdOutTopicID.Topic).
						AddUint64(stdInTopicID.Topic).
						AddUint64(stdErrTopicID.Topic).
						AddString(peerIDStr)).
					Execute(c)
				if err != nil {
					log.Panic(err, ": error calling the smart contract function")
				}
				fmt.Printf("contract call result: %v\n", callResult)
				return stdOutTopicID, stdInTopicID, stdErrTopicID, nil
			} else {
				log.Panic("We could not find your topics in the smart contract")
			}
		}
	}

	return hedera.TopicID{}, hedera.TopicID{}, hedera.TopicID{}, err
}

func GetPeerArraySize() (*big.Int, error) {
	contractCaller := GetHRpcClient()

	// Get the smart contract address for error reporting
	var scAddress string
	if commonlib.SmartContractAddressFlag != nil && *commonlib.SmartContractAddressFlag != "" {
		scAddress = *commonlib.SmartContractAddressFlag
	} else {
		scAddress = os.Getenv("smart_contract_address")
	}

	size, error := contractCaller.GetPeerArraySize(
		&bind.CallOpts{},
	)
	if error != nil {
		return nil, fmt.Errorf("failed to get peer array size [contract: %s]: %v", scAddress, error)
	}
	return size, error
}

type PeerInfo struct {
	Available   bool
	PeerID      string
	StdOutTopic uint64
	StdInTopic  uint64
	StdErrTopic uint64
}

// TODO: retry on failure. This one likes to return 502 bad gateway and eth rate limit exceeded.
// however, currently we stop the world on failure but should keep retrying.
func GetPeerInfo(hederaAccEvmAddress string) (PeerInfo, error) {
	log.Println("getting contract info for ", hederaAccEvmAddress)
	var peerInfo PeerInfo
	var err error
	maxRetries := 25
	baseDelay := time.Second

	// Get the smart contract address for error reporting
	var scAddress string
	if commonlib.SmartContractAddressFlag != nil && *commonlib.SmartContractAddressFlag != "" {
		scAddress = *commonlib.SmartContractAddressFlag
	} else {
		scAddress = os.Getenv("smart_contract_address")
	}

	for i := 0; i < maxRetries; i++ {
		contractCaller := GetHRpcClient()
		peerInfo, err = contractCaller.HederaAddressToPeer(
			&bind.CallOpts{},
			common.HexToAddress(hederaAccEvmAddress),
		)
		if err == nil {
			// Check if returned peerInfo is empty/default struct
			if peerInfo == (PeerInfo{}) {
				return peerInfo, fmt.Errorf("peer not found in the hedera contract for address; peer must be a registered neuron node: %s (contract: %s)", hederaAccEvmAddress, scAddress)
			}
			return peerInfo, nil
		}
		fmt.Printf("Error getting rpc peer info, retrying: %d th time [contract: %s] - %v\n", i, scAddress, err)
		time.Sleep(baseDelay * (1 << i)) // Exponential backoff
	}

	return peerInfo, fmt.Errorf("max retries exceeded getting peer info [contract: %s]: %v", scAddress, err)
}
func GetAllPeers() ([]string, error) {
	contractCaller := GetHRpcClient()

	// Get the smart contract address for error reporting
	var scAddress string
	if commonlib.SmartContractAddressFlag != nil && *commonlib.SmartContractAddressFlag != "" {
		scAddress = *commonlib.SmartContractAddressFlag
	} else {
		scAddress = os.Getenv("smart_contract_address")
	}

	peerArraySize, error := GetPeerArraySize()
	if error != nil {
		return nil, error
	}

	peerList := make([]string, 0)
	for i := big.NewInt(0); i.Cmp(peerArraySize) < 0; i.Add(i, big.NewInt(1)) {
		address, err1 := contractCaller.PeerList(&bind.CallOpts{}, i)

		perrInfo, err2 := GetPeerInfo(address.String())
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("failed to get peer list at index %d [contract: %s]: %v", i, scAddress, err1)
		}
		// check if the address bytes start with 0x0000000, that is a lot of zeros
		// then it's not an address that has been derived by a private key
		// but an address internally genearated by hedera. Reject it.
		if bytes.HasPrefix(address.Bytes(), make([]byte, 12)) {
			continue
		}
		if perrInfo.Available {
			peerList = append(peerList, address.String())
		}
	}
	return peerList, nil
}

func createDummySLA() string {

	client := GetHederaClientUsingEnv()
	// Create a new file
	createTx, err := hedera.NewFileCreateTransaction().
		SetContents([]byte("This is the SLA that binds you to x y z")).
		Execute(client)

	if err != nil {
		log.Fatal(err)
	}

	createReceipt, err := createTx.GetReceipt(client)
	if err != nil {
		log.Fatal(err)
	}

	fileId := createReceipt.FileID
	fmt.Printf("File ID: %v\n", fileId)

	appendTx, err := hedera.NewFileAppendTransaction().
		SetFileID(*fileId).
		SetContents([]byte(" Appending more text!")).
		Execute(client)

	if err != nil {
		log.Fatal(err)
	}

	appendReceipt, err := appendTx.GetReceipt(client)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Append Receipt: %v\n", appendReceipt.Status)
	return fileId.String()
}
