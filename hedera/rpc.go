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
	var emptyTopic hedera.TopicID

	hostPubKey, err := p2pHost.ID().ExtractPublicKey()
	if err != nil {
		return emptyTopic, emptyTopic, emptyTopic, fmt.Errorf("failed to extract public key from peer: %w", err)
	}
	hostPubKeyByte, err := hostPubKey.Raw()
	if err != nil {
		return emptyTopic, emptyTopic, emptyTopic, fmt.Errorf("failed to get raw public key bytes: %w", err)
	}
	hostPubKeyStr := common.Bytes2Hex(hostPubKeyByte)

	toEthAddress := keylib.ConverHederaPublicKeyToEthereunAddress(string(hostPubKeyStr))

	peerInfo, err := GetPeerInfo(toEthAddress)
	if err != nil {
		return emptyTopic, emptyTopic, emptyTopic, fmt.Errorf("failed to get peer info from blockchain or cache: %w", err)
	}

	{
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

// GetPeerInfo retrieves peer information using blockchain-first, cache-fallback strategy.
// It first attempts to query the Hedera blockchain. If successful, the result is cached.
// If the blockchain query fails (network issues, rate limits, insufficient funds),
// it falls back to locally cached data from bbolt.
func GetPeerInfo(hederaAccEvmAddress string) (PeerInfo, error) {
	log.Println("getting contract info for ", hederaAccEvmAddress)

	// Get the smart contract address for error reporting
	scAddress := getSmartContractAddress()

	// 1. Try blockchain first with reduced retries (3 instead of 25 for faster fallback)
	peerInfo, err := getPeerInfoFromBlockchain(hederaAccEvmAddress, 3)
	if err == nil {
		// Success - cache for future fallback
		if commonlib.GlobalStateManager != nil {
			cachedInfo := &commonlib.CachedPeerInfo{
				Available:   peerInfo.Available,
				PeerID:      peerInfo.PeerID,
				StdOutTopic: peerInfo.StdOutTopic,
				StdInTopic:  peerInfo.StdInTopic,
				StdErrTopic: peerInfo.StdErrTopic,
				CachedAt:    time.Now(),
			}
			commonlib.GlobalStateManager.PersistPeerInfo(hederaAccEvmAddress, cachedInfo)
		}
		return peerInfo, nil
	}

	log.Printf("⚠️ Blockchain query failed for %s: %v, attempting cache fallback", hederaAccEvmAddress, err)

	// 2. Fallback to cached data
	if commonlib.GlobalStateManager != nil {
		cached, cacheErr := commonlib.GlobalStateManager.LoadPeerInfo(hederaAccEvmAddress)
		if cacheErr == nil {
			// Check if cache is stale
			if commonlib.IsCacheStale(cached.CachedAt, commonlib.DefaultPeerInfoCacheTTL) {
				log.Printf("⚠️ Cache for %s is stale (cached %v ago), but using anyway due to blockchain unavailability",
					hederaAccEvmAddress, time.Since(cached.CachedAt).Round(time.Minute))
			} else {
				log.Printf("📦 Using cached PeerInfo for %s (cached %v ago)",
					hederaAccEvmAddress, time.Since(cached.CachedAt).Round(time.Minute))
			}
			return PeerInfo{
				Available:   cached.Available,
				PeerID:      cached.PeerID,
				StdOutTopic: cached.StdOutTopic,
				StdInTopic:  cached.StdInTopic,
				StdErrTopic: cached.StdErrTopic,
			}, nil
		}
		log.Printf("⚠️ Cache lookup also failed: %v", cacheErr)
	}

	// 3. Both failed - return original blockchain error
	return PeerInfo{}, fmt.Errorf("blockchain unavailable and no valid cache for %s [contract: %s]: %w",
		hederaAccEvmAddress, scAddress, err)
}

// getPeerInfoFromBlockchain queries the Hedera blockchain directly for peer info.
// This is the internal function that performs the actual blockchain query with retries.
func getPeerInfoFromBlockchain(hederaAccEvmAddress string, maxRetries int) (PeerInfo, error) {
	var peerInfo PeerInfo
	var err error
	baseDelay := time.Second

	scAddress := getSmartContractAddress()

	for attempt := 1; attempt <= maxRetries; attempt++ {
		contractCaller := GetHRpcClient()
		peerInfo, err = contractCaller.HederaAddressToPeer(
			&bind.CallOpts{},
			common.HexToAddress(hederaAccEvmAddress),
		)
		if err == nil {
			// Check if returned peerInfo is empty/default struct
			if peerInfo == (PeerInfo{}) {
				return peerInfo, fmt.Errorf("peer not found in the hedera contract for address; peer must be a registered neuron node: %s (contract: %s)",
					hederaAccEvmAddress, scAddress)
			}
			return peerInfo, nil
		}
		log.Printf("⚠️ Blockchain query attempt %d/%d failed [contract: %s]: %v", attempt, maxRetries, scAddress, err)
		if attempt < maxRetries {
			backoff := baseDelay * (1 << (attempt - 1)) // Exponential backoff: 1s, 2s, 4s
			time.Sleep(backoff)
		}
	}

	return peerInfo, fmt.Errorf("max retries exceeded getting peer info [contract: %s]: %v", scAddress, err)
}

// getSmartContractAddress returns the smart contract address from flag or environment
func getSmartContractAddress() string {
	if commonlib.SmartContractAddressFlag != nil && *commonlib.SmartContractAddressFlag != "" {
		return *commonlib.SmartContractAddressFlag
	}
	return os.Getenv("smart_contract_address")
}
// GetAllPeers retrieves all registered peer addresses using blockchain-first, cache-fallback strategy.
// It first attempts to query the Hedera blockchain. If successful, the result is cached.
// If the blockchain query fails, it falls back to locally cached data from bbolt.
func GetAllPeers() ([]string, error) {
	// 1. Try blockchain first
	peers, err := getAllPeersFromBlockchain()
	if err == nil {
		// Success - cache for future fallback
		if commonlib.GlobalStateManager != nil {
			cachedList := &commonlib.CachedPeerList{
				Addresses: peers,
				CachedAt:  time.Now(),
			}
			commonlib.GlobalStateManager.PersistPeerList(cachedList)
		}
		return peers, nil
	}

	log.Printf("⚠️ Blockchain query for peer list failed: %v, attempting cache fallback", err)

	// 2. Fallback to cached data
	if commonlib.GlobalStateManager != nil {
		cached, cacheErr := commonlib.GlobalStateManager.LoadPeerList()
		if cacheErr == nil && len(cached.Addresses) > 0 {
			// Check if cache is stale
			if commonlib.IsCacheStale(cached.CachedAt, commonlib.DefaultPeerInfoCacheTTL) {
				log.Printf("⚠️ Cached peer list is stale (cached %v ago), but using anyway due to blockchain unavailability",
					time.Since(cached.CachedAt).Round(time.Minute))
			} else {
				log.Printf("📦 Using cached peer list (%d peers, cached %v ago)",
					len(cached.Addresses), time.Since(cached.CachedAt).Round(time.Minute))
			}
			return cached.Addresses, nil
		}
		if cacheErr != nil {
			log.Printf("⚠️ Cache lookup also failed: %v", cacheErr)
		}
	}

	// 3. Both failed - return original blockchain error
	return nil, fmt.Errorf("blockchain unavailable and no valid peer list cache: %w", err)
}

// getAllPeersFromBlockchain queries the Hedera blockchain directly for the full peer list.
// This is the internal function that performs the actual blockchain query.
func getAllPeersFromBlockchain() ([]string, error) {
	contractCaller := GetHRpcClient()
	scAddress := getSmartContractAddress()

	peerArraySize, err := GetPeerArraySize()
	if err != nil {
		return nil, err
	}

	peerList := make([]string, 0)
	for i := big.NewInt(0); i.Cmp(peerArraySize) < 0; i.Add(i, big.NewInt(1)) {
		address, err1 := contractCaller.PeerList(&bind.CallOpts{}, i)

		// Use the direct blockchain query to avoid recursive caching
		perrInfo, err2 := getPeerInfoFromBlockchain(address.String(), 3)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("failed to get peer list at index %d [contract: %s]: %v", i, scAddress, err1)
		}
		// check if the address bytes start with 0x0000000, that is a lot of zeros
		// then it's not an address that has been derived by a private key
		// but an address internally generated by hedera. Reject it.
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
