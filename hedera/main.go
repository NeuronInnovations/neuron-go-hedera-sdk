package hedera_helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"os"
	"strings"
	"sync"
	"time"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/hederacontract"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/status"
)

var (
	hederaWriteLimiter = rate.NewLimiter(rate.Every(250*time.Millisecond), 2)
	hederaWriteSem     = make(chan struct{}, 2)
	// Reserved lane for user-triggered connect/reconnect envelopes so
	// background maintenance writes cannot starve interactive flows.
	hederaPriorityWriteLimiter = rate.NewLimiter(rate.Every(250*time.Millisecond), 1)
	hederaPriorityWriteSem     = make(chan struct{}, 1)
	hRpcMu             sync.Mutex
	hRpcClient         *ethclient.Client
	hRpcCaller         *hederacontract.HederacontractCaller
	hRpcAddress        string
)

func acquireHederaWriteSlot(op string) (func(), error) {
	return acquireHederaWriteSlotWithTimeout(op, 10*time.Second)
}

func acquireHederaPriorityWriteSlot(op string) (func(), error) {
	return acquireHederaPriorityWriteSlotWithTimeout(op, 1500*time.Millisecond)
}

func acquireHederaWriteSlotWithTimeout(op string, timeout time.Duration) (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case hederaWriteSem <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("hedera write queue timeout (%s): %w", op, ctx.Err())
	}

	if err := hederaWriteLimiter.Wait(ctx); err != nil {
		<-hederaWriteSem
		return nil, fmt.Errorf("hedera write limiter wait failed (%s): %w", op, err)
	}

	released := false
	return func() {
		if !released {
			released = true
			<-hederaWriteSem
		}
	}, nil
}

func acquireHederaPriorityWriteSlotWithTimeout(op string, timeout time.Duration) (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case hederaPriorityWriteSem <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("hedera priority write queue timeout (%s): %w", op, ctx.Err())
	}

	if err := hederaPriorityWriteLimiter.Wait(ctx); err != nil {
		<-hederaPriorityWriteSem
		return nil, fmt.Errorf("hedera priority write limiter wait failed (%s): %w", op, err)
	}

	released := false
	return func() {
		if !released {
			released = true
			<-hederaPriorityWriteSem
		}
	}, nil
}

func GetHRpcClient() *hederacontract.HederacontractCaller {
	const defaultSCAddress = "0x87e2fc64dc1eae07300c2fc50d6700549e1632ca"
	scAddress := strings.ToLower(strings.TrimSpace(os.Getenv("smart_contract_address")))
	if scAddress == "" {
		scAddress = defaultSCAddress
	}
	// Keep this aligned in process without rewriting env file on every call.
	if scAddress != defaultSCAddress {
		scAddress = defaultSCAddress
	}
	os.Setenv("smart_contract_address", scAddress)

	hRpcMu.Lock()
	defer hRpcMu.Unlock()

	if hRpcCaller != nil && hRpcClient != nil && hRpcAddress == scAddress {
		return hRpcCaller
	}

	if hRpcClient != nil {
		hRpcClient.Close()
		hRpcClient = nil
		hRpcCaller = nil
		hRpcAddress = ""
	}

	client, err := ethclient.Dial(os.Getenv("eth_rpc_url"))
	if err != nil {
		log.Panicf("failed to dial eth rpc: %v", err)
	}

	contractCaller, err := hederacontract.NewHederacontractCaller(
		common.HexToAddress(scAddress),
		client,
	)
	if err != nil {
		client.Close()
		log.Panicf("failed to create hedera contract caller: %v", err)
	}

	hRpcClient = client
	hRpcCaller = contractCaller
	hRpcAddress = scAddress
	return hRpcCaller
}

func GetHederaClientUsingEnv() *hedera.Client {
	c, err1 := hedera.ClientForName(hedera.NetworkNameTestnet.String())
	op, err2 := hedera.AccountIDFromString(os.Getenv("hedera_id"))

	if err1 != nil || err2 != nil {
		log.Fatalf("error creating hedera client: %v %v", err1, err2)
	}

	pkString := os.Getenv("private_key")

	if len(pkString) == 64 {
		pk, _ := hedera.PrivateKeyFromStringECDSA(pkString)
		c.SetOperator(op, pk)
		return c
	} else { // it's an ed25519
		pk, _ := hedera.PrivateKeyFromStringEd25519(pkString)
		c.SetOperator(op, pk)
		return c
	}

}

// dead function, not used.
func CreateAccountFromParent() {
	hederaParent, err := hedera.ClientForName(hedera.NetworkNameTestnet.String())
	if err != nil {
		println(err.Error(), ": error creating client")
		return
	}

	operatorAccountID, err := hedera.AccountIDFromString(os.Getenv("parent_hedera_id"))
	if err != nil {
		println(err.Error(), ": error converting string to AccountID")
		return
	}

	operatorKey, err := hedera.PrivateKeyFromStringECDSA(os.Getenv("parent_private_key"))
	if err != nil {
		println(err.Error(), ": error converting string to PrivateKey")
		return
	}

	// Setting the client operator ID and key
	hederaParent.SetOperator(operatorAccountID, operatorKey)

	newKey, _ := hedera.PrivateKeyFromStringDer(os.Getenv("private_key"))

	fmt.Printf("private = %v\n", newKey)
	fmt.Printf("public = %v\n", newKey.PublicKey().StringRaw())

	transactionResponse, err := hedera.NewAccountCreateTransaction().
		SetKey(newKey.PublicKey()).
		SetReceiverSignatureRequired(false).
		SetMaxAutomaticTokenAssociations(1).
		SetTransactionMemo("apple pear").
		SetInitialBalance(
			hedera.HbarFrom(100, hedera.HbarUnits.Hbar),
		).
		Execute(hederaParent)
	if err != nil {
		println(err.Error(), ": error executing account create transaction}")
		return
	}

	transactionReceipt, err := transactionResponse.GetReceipt(hederaParent)
	if err != nil {
		println(err.Error(), ": error getting receipt}")
		return
	}

	newAccountID := *transactionReceipt.AccountID
	fmt.Printf("account = %v\n", newAccountID)
}

func CreateTopic() (string, error) {
	c := GetHederaClientUsingEnv()
	defer c.Close()
	transactionResponse, err := hedera.NewTopicCreateTransaction().
		SetTransactionMemo("liveness topic").
		SetAdminKey(c.GetOperatorPublicKey()).
		Execute(c)

	if err != nil {
		println(err.Error(), ": error creating topic")
		return "", err
	}

	transactionReceipt, err := transactionResponse.GetReceipt(c)

	if err != nil {
		println(err.Error(), ": error getting topic create receipt")
		return "", err
	}

	topicID := *transactionReceipt.TopicID
	fmt.Printf("topicID: %v\n", topicID)
	return topicID.String(), nil

}

func SendToTopic(topicID hedera.TopicID, content string) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()

	release, err := acquireHederaWriteSlot("SendToTopic")
	if err != nil {
		return err
	}
	defer release()

	_, err = hedera.NewTopicMessageSubmitTransaction().
		SetMessage([]byte(content)).
		SetTopicID(topicID).
		Execute(client)

	return err

}

func BuyerPrepareServiceRequest(
	fromP2pPublicAddresses []multiaddr.Multiaddr,
	fromEthAddress string,
	toEthAddress string,
	arbiterEthAddress string,
	amount int64, // TODO: needs to match what is in the sla
) (*commonlib.TopicPostalEnvelope, error) {
	return BuyerPrepareServiceRequestWithSellerInfo(
		fromP2pPublicAddresses,
		fromEthAddress,
		toEthAddress,
		arbiterEthAddress,
		amount,
		nil,
	)
}

// BuyerPrepareServiceRequestWithSellerInfo builds a service request envelope and
// can reuse pre-fetched seller contract info to avoid duplicate contract reads
// on latency-sensitive manual connect paths.
func BuyerPrepareServiceRequestWithSellerInfo(
	fromP2pPublicAddresses []multiaddr.Multiaddr,
	fromEthAddress string,
	toEthAddress string,
	arbiterEthAddress string,
	amount int64, // TODO: needs to match what is in the sla
	sellerInfo *PeerInfo,
) (*commonlib.TopicPostalEnvelope, error) {
	client := GetHederaClientUsingEnv()
	defer client.Close()

	// check if all keys are valid

	fromHederaraID, err1 := hedera.AccountIDFromEvmAddress(0, 0, fromEthAddress)
	toHederaraID, err2 := hedera.AccountIDFromEvmAddress(0, 0, toEthAddress)
	arbiterHederaId, err3 := hedera.AccountIDFromEvmAddress(0, 0, arbiterEthAddress)

	if err1 != nil || err2 != nil || err3 != nil {
		return nil, errors.New("error:one of your keys cannot construct an account from a evm address")

	}

	// check if the accounts are valid by making account info queries
	accInfoFromHederaID, err1 := GetAccountInfoFromMirror(fromHederaraID)
	accInfoToHederaID, err2 := GetAccountInfoFromMirror(toHederaraID)
	accInfoArbiterHederaId, err3 := GetAccountInfoFromMirror(arbiterHederaId)

	if err1 != nil || err2 != nil || err3 != nil {
		return nil, errors.New("error:one of your keys returns an invalid account from hedera")
	}

	// the ethPublic keys are not good for signing hedera transactions, hence need to find the hedera public keys.
	fromHederaPupblicKeyEnc, err4 := hedera.PublicKeyFromStringECDSA(accInfoFromHederaID.PublicKey)
	toHederaPublicKeyEnc, err5 := hedera.PublicKeyFromStringECDSA(accInfoToHederaID.PublicKey)
	arbiterHederaKeyEnc, err6 := hedera.PublicKeyFromStringECDSA(accInfoArbiterHederaId.PublicKey)

	if err4 != nil || err5 != nil || err6 != nil {
		return nil, errors.New("for one of the keys I can't make it into an ecdsa public key")
	}

	// get the topic that the otherside wants the requests to be sent to: stdIn
	var perrInfo PeerInfo
	if sellerInfo != nil && sellerInfo.StdInTopic != 0 {
		perrInfo = *sellerInfo
	} else {
		fetchedPeerInfo, fetchErr := GetPeerInfo(toEthAddress)
		if fetchErr != nil {
			// add more text to err
			return nil, fmt.Errorf("error getting the topic for eth peer %v when looking into the contract: %v", toEthAddress, fetchErr)
		}
		perrInfo = fetchedPeerInfo
	}

	toStdInTopic, err := hedera.TopicIDFromString(fmt.Sprintf("0.0.%d", perrInfo.StdInTopic))
	if err != nil {
		return nil, err
	}

	// Check cache for existing shared account before creating a new one
	var sharedAccID hedera.AccountID
	cachedAccount, cacheErr := commonlib.LoadSharedAccount(fromEthAddress, toEthAddress, arbiterEthAddress)
	if cacheErr == nil && cachedAccount.SharedAccID != 0 {
		// Validate the cached account still exists on Hedera (free mirror query)
		cachedAccID := hedera.AccountID{Shard: 0, Realm: 0, Account: cachedAccount.SharedAccID}
		if _, validationErr := GetAccountInfoFromMirror(cachedAccID); validationErr == nil {
			// Cached account is valid, reuse it
			sharedAccID = cachedAccID
			log.Printf("Reusing cached shared account %d for seller %s (saved HBAR!)", sharedAccID.Account, toEthAddress)
		} else {
			log.Printf("Cached shared account %d no longer valid, creating new one", cachedAccount.SharedAccID)
			cachedAccount = nil // Force new account creation
		}
	}

	// Create new shared account if no valid cached one exists
	if sharedAccID.Account == 0 {
		sharedAccTx, err := createSharedAccount(fromHederaPupblicKeyEnc, toHederaPublicKeyEnc, arbiterHederaKeyEnc, amount)
		if err != nil {
			return nil, fmt.Errorf("error preparing a shared account: %v", err)
		}
		release, slotErr := acquireHederaWriteSlot("CreateSharedAccount")
		if slotErr != nil {
			return nil, slotErr
		}
		sharedAccTxResponse, err :=
			sharedAccTx.SetMaxBackoff(time.Second * 5).SetMaxRetry(10).Execute(client)
		release()

		if err != nil {
			return nil, fmt.Errorf("error creating a shared account: %v", err)
		}
		sharedAccTxReceipt, err := sharedAccTxResponse.GetReceipt(client)
		if err != nil {
			return nil, err
		}
		sharedAccID = *sharedAccTxReceipt.AccountID
		log.Printf("Created new shared account %d for seller %s", sharedAccID.Account, toEthAddress)

		// Cache the newly created shared account for future use
		saveErr := commonlib.SaveSharedAccount(&commonlib.SharedAccountRecord{
			BuyerEthAddress:   fromEthAddress,
			SellerEthAddress:  toEthAddress,
			ArbiterEthAddress: arbiterEthAddress,
			SharedAccID:       sharedAccID.Account,
		})
		if saveErr != nil {
			log.Printf("Warning: Failed to cache shared account: %v", saveErr)
			// Continue without caching - graceful degradation
		}
	}
	fmt.Printf("shared account id: %v\n", sharedAccID)
	serialized := fmt.Sprintf("%s", fromP2pPublicAddresses)
	fmt.Printf("Sending serialized multiaddr: %s to seller %s \n", serialized, toEthAddress)
	encyptedIpAddress, encErr := keylib.EncryptForOtherside([]byte(serialized), os.Getenv("private_key"), toHederaPublicKeyEnc.StringRaw())
	if encErr != nil {
		return nil, encErr
	}

	peerInfo, err := GetPeerInfo(fromEthAddress)
	if err != nil {
		log.Panic(err)
	}

	m := &commonlib.NeuronServiceRequestMsg{
		MessageType:        "serviceRequest",
		SlaAgreed:          1,
		ServiceType:        string(commonlib.MyProtocol),
		SharedAccID:        sharedAccID.Account,
		EncryptedIpAddress: encyptedIpAddress,
		EthPublicKey:       fromEthAddress,
		PublicKey:          fromHederaPupblicKeyEnc.StringRaw(),
		StdInTopic:         peerInfo.StdInTopic,
		Version:            "0.4",
	}

	ret := commonlib.TopicPostalEnvelope{
		Message:         m,
		OtherStdInTopic: toStdInTopic,
	}

	return &ret, nil

}

func SellerSendScheduledTransferRequest(
	sharedAccID hedera.AccountID, // move money out from here
	toHederaParentID hedera.AccountID, // move money into here, that's the sellers account id, not the device id. The seller should know his own parent's id. get from .env
	toHederaDeviceID hedera.AccountID, // move money into here, that's the device
	buyerStdIn hedera.TopicID, // inform buyer that a schedule is up for counter signing
) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()

	// Payment split: Device gets 90%, Parent gets 10%
	// Total: 0.1 HBAR (10,000,000 tinybars)
	transferTx, err := hedera.NewTransferTransaction().
		AddHbarTransfer(sharedAccID, hedera.HbarFrom(-0.1, hedera.HbarUnits.Hbar)).
		AddHbarTransfer(toHederaParentID, hedera.HbarFrom(0.01, hedera.HbarUnits.Hbar)).
		AddHbarTransfer(toHederaDeviceID, hedera.HbarFrom(0.09, hedera.HbarUnits.Hbar)).
		// TODO: AddTokenTransfer() transfer tokens to  other fee and reward accounts.
		SetTransactionMemo(uuid.New().String()).
		FreezeWith(client)

	if err != nil {
		return err
	}

	// Prepare the transfer transaction to be scheduled
	scheduledTransferTx, err := transferTx.Schedule()
	if err != nil {
		return err
	}

	release, slotErr := acquireHederaWriteSlot("SellerSendScheduledTransferRequest.schedule")
	if slotErr != nil {
		return slotErr
	}
	scheduledTxResponse, err := scheduledTransferTx.Execute(client)
	release()

	if err != nil {
		return err
	}

	receipt, err := scheduledTxResponse.GetReceipt(client)
	if err != nil {
		return err
	}

	scheduleId := receipt.ScheduleID
	fmt.Printf("receipt: %v  and  shed tx id: \n %v and sched id  %v \n", receipt, receipt.ScheduledTransactionID, scheduleId)

	// prepare message and send to the buyer's topic.

	m := &commonlib.NeuronScheduleSignRequestMsg{
		MessageType: "scheduleSignRequest",
		ScheduleID:  scheduleId.Schedule,
		SharedAccID: sharedAccID.Account,
		Version:     "0.4", // get this from the compiler
	}
	jsonBytes, _ := json.Marshal(m)

	release, slotErr = acquireHederaWriteSlot("SellerSendScheduledTransferRequest.notifyBuyer")
	if slotErr != nil {
		return slotErr
	}
	txResponse, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(buyerStdIn).
		Execute(client)
	release()

	if err != nil {
		return err
	}

	fmt.Println(txResponse)

	return nil
}

func BuyerCounterSignSchedule(scheduleID hedera.ScheduleID) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()

	fmt.Println("signing scheduleID: ", scheduleID)

	sigerr := SignSchedule(scheduleID, os.Getenv("private_key"))
	if sigerr != nil {
		log.Println(sigerr)
	}
	return sigerr
}

func PeerSendErrorMessage(otherSideStdIn hedera.TopicID, errorType commonlib.ErrorType, errorMessage string, recoverAction commonlib.RecoverAction) {
	go func() {
		client := GetHederaClientUsingEnv()
		defer client.Close()
		m := &commonlib.NeuronPeerErrorMsg{
			MessageType:   "peerError",
			StdInTopic:    commonlib.MyStdIn.Topic,
			PublicKey:     commonlib.MyPublicKey.StringRaw(),
			ErrorType:     errorType,
			ErrorMessage:  errorMessage,
			RecoverAction: recoverAction,
			Version:       "0.1",
		}

		jsonBytes, _ := json.Marshal(m)

		release, slotErr := acquireHederaWriteSlot("PeerSendErrorMessage")
		if slotErr != nil {
			log.Println(slotErr)
			return
		}
		_, err := hedera.NewTopicMessageSubmitTransaction().
			SetMessage(jsonBytes).
			SetTopicID(otherSideStdIn).
			Execute(client)
		release()
		if err != nil {
			log.Println(err)
		}
	}()
}

func SendSelfErrorMessage(errorType commonlib.ErrorType, errorMessage string, recoverAction commonlib.RecoverAction) error {

	client := GetHederaClientUsingEnv()
	defer client.Close()
	m := &commonlib.NeuronSelfErrorMsg{
		MessageType:   "selfError",
		StdInTopic:    commonlib.MyStdIn.Topic,
		ErrorType:     errorType,
		ErrorMessage:  errorMessage,
		RecoverAction: recoverAction,
		Version:       "0.1",
	}

	jsonBytes, _ := json.Marshal(m)

	release, slotErr := acquireHederaWriteSlot("SendSelfErrorMessage")
	if slotErr != nil {
		return slotErr
	}
	_, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(commonlib.MyStdErr).
		Execute(client)
	release()
	return err
}

func SendTransactionEnvelope(tx commonlib.TopicPostalEnvelope) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	jsonBytes, marshallingError := json.Marshal(tx.Message)
	if marshallingError != nil {
		return marshallingError
	}

	release, slotErr := acquireHederaWriteSlot("SendTransactionEnvelope")
	if slotErr != nil {
		return slotErr
	}
	_, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(tx.OtherStdInTopic).
		Execute(client)
	release()
	return err
}

// SendTransactionEnvelopePriority is reserved for user-triggered connect/reconnect
// requests. It bypasses the shared background write queue to keep manual actions
// responsive under heavy Hedera maintenance traffic.
func SendTransactionEnvelopePriority(tx commonlib.TopicPostalEnvelope) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	jsonBytes, marshallingError := json.Marshal(tx.Message)
	if marshallingError != nil {
		return marshallingError
	}

	release, slotErr := acquireHederaPriorityWriteSlot("SendTransactionEnvelopePriority")
	if slotErr != nil {
		return slotErr
	}
	_, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(tx.OtherStdInTopic).
		Execute(client)
	release()
	return err
}

// SendTransactionEnvelopeBestEffort submits a topic message but returns quickly
// if Hedera is currently busy. Intended for periodic/background retries so they
// don't block manual connect flows.
func SendTransactionEnvelopeBestEffort(tx commonlib.TopicPostalEnvelope) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	jsonBytes, marshallingError := json.Marshal(tx.Message)
	if marshallingError != nil {
		return marshallingError
	}

	release, slotErr := acquireHederaWriteSlotWithTimeout("SendTransactionEnvelopeBestEffort", 1200*time.Millisecond)
	if slotErr != nil {
		return slotErr
	}
	_, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(tx.OtherStdInTopic).
		Execute(client)
	release()
	return err
}

// is used as a subroutine
func ListenToTopicAndCallBack(stdInTopic hedera.TopicID, callback func(message hedera.TopicMessage)) error {
	myEthAddress := os.Getenv("hedera_evm_id")
	if myEthAddress == "" {
		return errors.New("myEthAddress is empty")
	}
	downloadAndListen(stdInTopic, callback)
	// the above should never return; reaching the next line is an error.
	return errors.New("failed to ListenToTopicAndCallBack")
}

func downloadAndListen(topicID hedera.TopicID, callback func(message hedera.TopicMessage)) {
	// Create a new client with the operator account ID and key
	client := GetHederaClientUsingEnv()
	defer client.Close()
	// Channel to signal message receipt
	messageReceived := make(chan struct{}, 1)
	subscriptionDone := make(chan struct{}, 1)
	// Main loop to keep subscribing
	lastStdInTimestamp := time.Now().UTC()
	reconnectDelay := 1 * time.Second
mainLoop:
	// Re-subscribe from last seen timestamp (with small overlap) so we don't
	// miss messages during subscription churn.
	startTime := lastStdInTimestamp.Add(-2 * time.Second)
	handle, err := subscribe(client, topicID, startTime, func(message hedera.TopicMessage) {
		ts := message.ConsensusTimestamp
		if !ts.IsZero() && ts.After(lastStdInTimestamp) {
			lastStdInTimestamp = ts
		}
		callback(message)
	}, messageReceived, subscriptionDone)
	if err != nil {
		log.Println("SELFERROR:Error subscribing to topic: ", err) // TODO: send error to error topic
		time.Sleep(reconnectDelay)
		reconnectDelay = minDuration(reconnectDelay*2, 30*time.Second)
		goto mainLoop
	}
	reconnectDelay = 1 * time.Second
	timeout := time.NewTimer(45 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-messageReceived:
			// Message received, reset the timer
			if !timeout.Stop() {
				<-timeout.C // Drain the channel
			}
			timeout.Reset(45 * time.Second)
		case <-subscriptionDone:
			// Underlying SDK completed subscription unexpectedly; resubscribe now.
			handle.Unsubscribe()
			time.Sleep(reconnectDelay)
			reconnectDelay = minDuration(reconnectDelay*2, 30*time.Second)
			goto mainLoop
		case <-timeout.C:
			// Watchdog: no messages for a while could mean dead subscription.
			handle.Unsubscribe()
			goto mainLoop // Break out of the outer loop to restart the subscription]
		}
	}
}

func subscribe(
	client *hedera.Client,
	topicID hedera.TopicID,
	startTime time.Time,
	callback func(message hedera.TopicMessage),
	messageReceived chan struct{},
	subscriptionDone chan struct{},
) (hedera.SubscriptionHandle, error) {

	handle, err := hedera.NewTopicMessageQuery().
		SetTopicID(topicID).
		SetStartTime(startTime.Add(1*time.Nanosecond)).
		SetMaxAttempts(0). // Define how many retry attempts to make
		SetRetryHandler(
			func(err error) bool {
				//log.Printf("Retry handler: Subscription error: %v. don't retry...\n", err)
				return false // Don't Retry
			},
		).
		SetErrorHandler(
			func(stat status.Status) {
				//log.Printf("Subs Query error: %v...\n", stat)
				select {
				case subscriptionDone <- struct{}{}:
				default:
				}
			},
		).
		SetCompletionHandler(
			func() {
				log.Printf("Subscription completed unexpectedly\n")
				select {
				case subscriptionDone <- struct{}{}:
				default:
				}
			},
		).
		Subscribe(
			client,
			func(message hedera.TopicMessage) {
				// Signal that a message was received
				select {
				case messageReceived <- struct{}{}:
				default:
					// Avoid blocking if no one is listening
				}
				callback(message)
			},
		)

	if err != nil {
		log.Printf("Error subscribing: %v\n", err)
		return hedera.SubscriptionHandle{}, err
	}

	return handle, nil
}

func minDuration(a time.Duration, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func SignSchedule(scheduleId hedera.ScheduleID, privateKey string) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	hederaPrivateKey, err := hedera.PrivateKeyFromStringECDSA(privateKey)
	if err != nil {
		return err
	}

	// Sign the schedule
	scheduleSignTx, err := hedera.NewScheduleSignTransaction().
		SetScheduleID(scheduleId).
		FreezeWith(client)

	if err != nil {
		return err
	}
	release, slotErr := acquireHederaWriteSlot("SignSchedule")
	if slotErr != nil {
		return slotErr
	}
	scheduleSignTxResponse, err := scheduleSignTx.Sign(hederaPrivateKey).Execute(client)
	release()
	if err != nil {
		return err
	}

	signResponseReceipt, err := scheduleSignTxResponse.GetReceipt(client)
	if err != nil {
		return err
	}
	fmt.Println("Schedule signed with receipt:", signResponseReceipt)

	query, err := hedera.NewScheduleInfoQuery().
		SetScheduleID(scheduleId).
		Execute(client)

	if err != nil {
		fmt.Println("No problem - Error getting schedule info: ", err)
		return err

	}

	fmt.Println("Schedule signatories: ", query, "signers", query.Signatories)
	return nil
}

// SignScheduleBestEffort attempts schedule signing with a short queue wait so
// payment maintenance cannot starve connect/reconnect traffic.
func SignScheduleBestEffort(scheduleId hedera.ScheduleID, privateKey string) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	hederaPrivateKey, err := hedera.PrivateKeyFromStringECDSA(privateKey)
	if err != nil {
		return err
	}

	scheduleSignTx, err := hedera.NewScheduleSignTransaction().
		SetScheduleID(scheduleId).
		FreezeWith(client)
	if err != nil {
		return err
	}

	release, slotErr := acquireHederaWriteSlotWithTimeout("SignScheduleBestEffort", 1200*time.Millisecond)
	if slotErr != nil {
		return slotErr
	}
	scheduleSignTxResponse, err := scheduleSignTx.Sign(hederaPrivateKey).Execute(client)
	release()
	if err != nil {
		return err
	}

	_, err = scheduleSignTxResponse.GetReceipt(client)
	return err
}

func createSharedAccount(buyer, seller, arbiter hedera.PublicKey, initialPayment int64) (*hedera.AccountCreateTransaction, error) {

	kl := hedera.KeyListWithThreshold(3).AddAllPublicKeys(
		[]hedera.PublicKey{
			buyer,
			buyer,
			arbiter,
			seller,
		})
	return hedera.NewAccountCreateTransaction().
		SetKey(kl).
		SetInitialBalance(
			hedera.HbarFrom(float64(initialPayment), hedera.HbarUnits.Millibar),
		), nil

}

func GetAccountInfoFromNetwork(accountID hedera.AccountID) (hedera.AccountInfo, error) {
	client := GetHederaClientUsingEnv()
	defer client.Close()

	accountInfo, err := hedera.NewAccountInfoQuery().
		SetAccountID(accountID).
		Execute(client)
	if err != nil {
		return hedera.AccountInfo{}, err
	}
	return accountInfo, nil

}
func DepositToSharedAccount(sharedAccountID hedera.AccountID, amount float64) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	release, slotErr := acquireHederaWriteSlot("DepositToSharedAccount")
	if slotErr != nil {
		return slotErr
	}
	_, err := hedera.NewTransferTransaction().
		AddHbarTransfer(client.GetOperatorAccountID(), hedera.HbarFrom(-amount, hedera.HbarUnits.Hbar)). // Send 3 HBAR
		AddHbarTransfer(sharedAccountID, hedera.HbarFrom(amount, hedera.HbarUnits.Hbar)).                // Receive 3 HBAR
		Execute(client)
	release()
	return err
}

// DepositToSharedAccountBestEffort attempts to top up shared account with a
// short queue wait; skipped deposits can be retried on later schedule cycles.
func DepositToSharedAccountBestEffort(sharedAccountID hedera.AccountID, amount float64) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	release, slotErr := acquireHederaWriteSlotWithTimeout("DepositToSharedAccountBestEffort", 1200*time.Millisecond)
	if slotErr != nil {
		return slotErr
	}
	_, err := hedera.NewTransferTransaction().
		AddHbarTransfer(client.GetOperatorAccountID(), hedera.HbarFrom(-amount, hedera.HbarUnits.Hbar)).
		AddHbarTransfer(sharedAccountID, hedera.HbarFrom(amount, hedera.HbarUnits.Hbar)).
		Execute(client)
	release()
	return err
}
