package hedera_helper

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

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
				callResult, err := hedera.NewContractExecuteTransaction().
					SetContractID(newContractID).
					SetTransactionMemo("broadcast liveness topic for self").
					SetGas(500000).
					SetFunction("putPeerAvailableSelf", hedera.NewContractFunctionParameters().
						AddUint64(stdOutTopicID.Topic).
						AddUint64(stdInTopicID.Topic).
						AddUint64(stdErrTopicID.Topic).
						AddString(keylib.ConvertHederaPublicKeyToPeerID(string(hostPubKeyStr)))).
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

	size, error := contractCaller.GetPeerArraySize(
		&bind.CallOpts{},
	)
	return size, error
}

type PeerInfo struct {
	Available   bool
	PeerID      string
	StdOutTopic uint64
	StdInTopic  uint64
	StdErrTopic uint64
}

type cachedPeerInfo struct {
	info      PeerInfo
	expiresAt time.Time
}

type peerInfoResult struct {
	info PeerInfo
	err  error
}

var (
	peerInfoMu       sync.Mutex
	peerInfoCache    = make(map[string]cachedPeerInfo)
	peerInfoInFlight = make(map[string][]chan peerInfoResult)
)

// TODO: retry on failure. This one likes to return 502 bad gateway and eth rate limit exceeded.
// however, currently we stop the world on failure but should keep retrying.
func GetPeerInfo(hederaAccEvmAddress string) (PeerInfo, error) {
	key := normalizeEvmAddress(hederaAccEvmAddress)
	now := time.Now()

	peerInfoMu.Lock()
	if cached, ok := peerInfoCache[key]; ok && now.Before(cached.expiresAt) {
		info := cached.info
		peerInfoMu.Unlock()
		return info, nil
	}
	if waiters, ok := peerInfoInFlight[key]; ok {
		ch := make(chan peerInfoResult, 1)
		peerInfoInFlight[key] = append(waiters, ch)
		peerInfoMu.Unlock()
		res := <-ch
		return res.info, res.err
	}
	peerInfoInFlight[key] = nil
	peerInfoMu.Unlock()

	log.Println("getting contract info for ", key)
	info, err := fetchPeerInfoWithRetry(key)

	peerInfoMu.Lock()
	if err == nil {
		peerInfoCache[key] = cachedPeerInfo{
			info:      info,
			expiresAt: time.Now().Add(10 * time.Second),
		}
	}
	waiters := peerInfoInFlight[key]
	delete(peerInfoInFlight, key)
	peerInfoMu.Unlock()

	result := peerInfoResult{info: info, err: err}
	for _, ch := range waiters {
		ch <- result
		close(ch)
	}
	return info, err
}

func fetchPeerInfoWithRetry(hederaAccEvmAddress string) (PeerInfo, error) {
	var peerInfo PeerInfo
	var err error
	// Keep this bounded and responsive: this path is called by UI/API-triggered
	// connect/reconnect flows. Excessive exponential retries can stall the buyer.
	maxRetries := 3
	baseDelay := 200 * time.Millisecond
	maxDelay := 1 * time.Second
	perAttemptTimeout := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		contractCaller := GetHRpcClient()
		callCh := make(chan peerInfoResult, 1)
		go func() {
			info, callErr := contractCaller.HederaAddressToPeer(
				&bind.CallOpts{},
				common.HexToAddress(hederaAccEvmAddress),
			)
			callCh <- peerInfoResult{info: info, err: callErr}
		}()
		select {
		case res := <-callCh:
			peerInfo = res.info
			err = res.err
		case <-time.After(perAttemptTimeout):
			err = fmt.Errorf("GetPeerInfo timeout after %s", perAttemptTimeout)
		}
		if err == nil {
			return peerInfo, nil
		}
		fmt.Println("Error getting rpc peer info, retrying:", i, "th time", err)
		delay := baseDelay * time.Duration(1<<i) // Exponential backoff
		if delay > maxDelay {
			delay = maxDelay
		}
		time.Sleep(delay)
	}
	return peerInfo, err
}

func normalizeEvmAddress(evm string) string {
	s := strings.TrimSpace(strings.ToLower(evm))
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return ""
	}
	return "0x" + s
}

// GetPeerIDFromEvmViaMirror derives the libp2p peer ID for an EVM address by
// looking up the Hedera account via the mirror and converting its public key
// to a peer ID (same key the libp2p host uses for that identity).
// Use when the contract returns a nickname or invalid peer ID.
func GetPeerIDFromEvmViaMirror(evmAddress string) (peerID string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("derive peer ID from public key: %v", r)
			peerID = ""
		}
	}()
	key := normalizeEvmAddress(evmAddress)
	if key == "" {
		return "", fmt.Errorf("invalid evm address")
	}
	accountID, err := hedera.AccountIDFromEvmAddress(0, 0, key)
	if err != nil {
		return "", fmt.Errorf("account from evm: %w", err)
	}
	accInfo, err := GetAccountInfoFromMirror(accountID)
	if err != nil {
		return "", fmt.Errorf("mirror account info: %w", err)
	}
	if accInfo.PublicKey == "" {
		return "", errors.New("no public key in mirror account")
	}
	peerID = keylib.ConvertHederaPublicKeyToPeerID(accInfo.PublicKey)
	return peerID, nil
}
func GetAllPeers() ([]string, error) {
	contractCaller := GetHRpcClient()

	peerArraySize, error := GetPeerArraySize()
	if error != nil {
		return nil, error
	}

	peerList := make([]string, 0)
	for i := big.NewInt(0); i.Cmp(peerArraySize) < 0; i.Add(i, big.NewInt(1)) {
		address, err1 := contractCaller.PeerList(&bind.CallOpts{}, i)

		perrInfo, err2 := GetPeerInfo(address.String())
		if err1 != nil || err2 != nil {
			return nil, err1
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
