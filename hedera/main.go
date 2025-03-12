package hedera_helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"os"
	"time"

	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/hederacontract"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/multiformats/go-multiaddr"
	"google.golang.org/grpc/status"
)

func GetHRpcClient() *hederacontract.HederacontractCaller {

	client, _ := ethclient.Dial(os.Getenv("eth_rpc_url"))

	contractCaller, _ := hederacontract.NewHederacontractCaller(
		common.HexToAddress(os.Getenv("smart_contract_address")),
		client,
	)
	return contractCaller
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

	_, err := hedera.NewTopicMessageSubmitTransaction().
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
	perrInfo, err := GetPeerInfo(toEthAddress)
	if err != nil {
		// add more text to err
		return nil, fmt.Errorf("error getting the topic for eth peer %v when looking into the contract: %v", toEthAddress, err)
	}

	toStdInTopic, err := hedera.TopicIDFromString(fmt.Sprintf("0.0.%d", perrInfo.StdInTopic))
	if err != nil {
		return nil, err
	}

	// create a shared account and deposit the payment the sensor wants.

	sharedAccTx, err := createSharedAccount(fromHederaPupblicKeyEnc, toHederaPublicKeyEnc, arbiterHederaKeyEnc, amount)
	if err != nil {
		return nil, fmt.Errorf("error preparing a shared account: %v", err)
	}
	sharedAccTxResponse, err :=
		sharedAccTx.SetMaxBackoff(time.Second * 5).SetMaxRetry(10).Execute(client)

	if err != nil {
		return nil, fmt.Errorf("error creating a shared account: %v", err)
	}
	sharedAccTxReceipt, err := sharedAccTxResponse.GetReceipt(client)
	if err != nil {
		return nil, err

	}
	sharedAccID := *sharedAccTxReceipt.AccountID
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

	scheduledTxResponse, err := scheduledTransferTx.Execute(client)

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

	txResponse, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(buyerStdIn).
		Execute(client)

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

		_, err := hedera.NewTopicMessageSubmitTransaction().
			SetMessage(jsonBytes).
			SetTopicID(otherSideStdIn).
			Execute(client)
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

	_, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(commonlib.MyStdErr).
		Execute(client)
	return err
}

func SendTransactionEnvelope(tx commonlib.TopicPostalEnvelope) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	jsonBytes, marshallingError := json.Marshal(tx.Message)
	if marshallingError != nil {
		return marshallingError
	}

	_, err := hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(tx.OtherStdInTopic).
		Execute(client)
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
	// Main loop to keep subscribing
mainLoop:
	// Get the last time you received a message from the environment variable called "last_stdin_timestamp"
	var lastStdInTimestamp time.Time
	lastStdInTimestampEnv := os.Getenv("last_stdin_timestamp")
	if lastStdInTimestampEnv != "" {
		lastStdInTimestamp, _ = time.Parse(time.RFC3339Nano, lastStdInTimestampEnv)
	} else {
		lastStdInTimestamp = time.Now().UTC()
		log.Default().Println("last_stdin_timestamp not set, defaulting to now")
	}

	handle, err := subscribe(client, topicID, lastStdInTimestamp, callback, messageReceived)
	if err != nil {
		log.Println("SELFERROR:Error subscribing to topic: ", err) // TODO: send error to error topic
	}
	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-messageReceived:
			// Message received, reset the timer
			if !timeout.Stop() {
				<-timeout.C // Drain the channel
			}
			timeout.Reset(3 * time.Minute)
		case <-timeout.C:
			// Timeout, no messages received for 3 minutes (could be because subsription didn't work too)
			handle.Unsubscribe()
			goto mainLoop // Break out of the outer loop to restart the subscription]
		}
	}
}

func subscribe(client *hedera.Client, topicID hedera.TopicID, startTime time.Time, callback func(message hedera.TopicMessage), messageReceived chan struct{}) (hedera.SubscriptionHandle, error) {

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

			},
		).
		SetCompletionHandler(
			func() {
				log.Printf("Subscription completed unexpectedly\n")
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
	scheduleSignTxResponse, err := scheduleSignTx.Sign(hederaPrivateKey).Execute(client)
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
	_, err := hedera.NewTransferTransaction().
		AddHbarTransfer(client.GetOperatorAccountID(), hedera.HbarFrom(-amount, hedera.HbarUnits.Hbar)). // Send 3 HBAR
		AddHbarTransfer(sharedAccountID, hedera.HbarFrom(amount, hedera.HbarUnits.Hbar)).                // Receive 3 HBAR
		Execute(client)
	return err
}
