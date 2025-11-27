package hedera_helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

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

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
)

func GetHRpcClient() *hederacontract.HederacontractCaller {

	// Check command-line flag first (highest priority), then environment variable
	var scAddress string
	if commonlib.SmartContractAddressFlag != nil && *commonlib.SmartContractAddressFlag != "" {
		scAddress = *commonlib.SmartContractAddressFlag
	} else {
		scAddress = os.Getenv("smart_contract_address")
	}

	// Validate smart contract address format
	if scAddress == "" {
		log.Fatal("smart_contract_address is not set. Please provide it via --smart-contract-address flag or smart_contract_address environment variable")
	}

	if !keylib.IsValidEthereumAddress(scAddress) {
		log.Fatalf("invalid smart contract address format: %s. Must be a valid Ethereum address (0x + 40 hex characters)", scAddress)
	}

	client, _ := ethclient.Dial(os.Getenv("eth_rpc_url"))

	contractCaller, _ := hederacontract.NewHederacontractCaller(
		common.HexToAddress(scAddress),
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
	existingSharedAccID uint64, // Pass 0 to create new account, or existing account ID to reuse
) (*types.TopicPostalEnvelope, error) {
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

	// Determine shared account: reuse existing or create new
	var sharedAccID hedera.AccountID

	if existingSharedAccID > 0 {
		// Try to reuse existing shared account
		sharedAccID = hedera.AccountID{Shard: 0, Realm: 0, Account: existingSharedAccID}

		// Validate the account exists and check balance
		accountInfo, err := GetAccountInfoFromNetwork(sharedAccID)
		if err != nil {
			log.Printf("Existing SharedAccID %d not found on network, creating new: %v", existingSharedAccID, err)
			existingSharedAccID = 0 // Reset to create new
		} else {
			currentBalance := accountInfo.Balance.As(hedera.HbarUnits.Millibar)
			if currentBalance < float64(amount) {
				// Top up the account
				topUpAmount := float64(amount) - currentBalance
				log.Printf("SharedAccID %d has insufficient balance (%.2f millibar), topping up with %.2f millibar",
					existingSharedAccID, currentBalance, topUpAmount)
				if err := DepositToSharedAccount(sharedAccID, topUpAmount); err != nil {
					log.Printf("Failed to top up SharedAccID %d, creating new: %v", existingSharedAccID, err)
					existingSharedAccID = 0 // Reset to create new
				} else {
					log.Printf("Successfully reusing SharedAccID %d after top-up", existingSharedAccID)
				}
			} else {
				log.Printf("Reusing existing SharedAccID %d with balance %.2f millibar", existingSharedAccID, currentBalance)
			}
		}
	}

	// Create new shared account if needed
	if existingSharedAccID == 0 {
		sharedAccTx, err := createSharedAccount(fromHederaPupblicKeyEnc, toHederaPublicKeyEnc, arbiterHederaKeyEnc, amount)
		if err != nil {
			return nil, fmt.Errorf("error preparing a shared account: %v", err)
		}
		sharedAccTxResponse, err := sharedAccTx.SetMaxBackoff(time.Second * 5).SetMaxRetry(10).Execute(client)

		if err != nil {
			return nil, fmt.Errorf("error creating a shared account: %v", err)
		}
		sharedAccTxReceipt, err := sharedAccTxResponse.GetReceipt(client)
		if err != nil {
			return nil, err
		}
		sharedAccID = *sharedAccTxReceipt.AccountID
		log.Printf("Created new SharedAccID: %v", sharedAccID)
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

	m := &types.NeuronServiceRequestMsg{
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

	ret := types.TopicPostalEnvelope{
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

	// Get the current balance of the shared account
	accountInfo, err := GetAccountInfoFromNetwork(sharedAccID)
	if err != nil {
		// Network error - queue invoice for later and continue streaming
		if isNetworkError(err) {
			log.Printf("⚠️ Network error getting balance, queueing invoice for SharedAccID %d", sharedAccID.Account)
			queueInvoiceForLater(sharedAccID.Account, 0, buyerStdIn.Topic)
			return nil // Don't fail - continue streaming
		}
		return fmt.Errorf("failed to get account balance: %v", err)
	}

	totalAmount := accountInfo.Balance.As(hedera.HbarUnits.Millibar)
	log.Printf("Shared account balance: %f millibar", totalAmount)

	// Move the entire amount out of the shared account with 60/40 split
	sixtyPercent := float64(totalAmount) * 0.6
	fortyPercent := float64(totalAmount) * 0.4

	transferTx, err := hedera.NewTransferTransaction().
		AddHbarTransfer(sharedAccID, hedera.HbarFrom(-float64(totalAmount), hedera.HbarUnits.Millibar)).
		AddHbarTransfer(toHederaParentID, hedera.HbarFrom(sixtyPercent, hedera.HbarUnits.Millibar)). // 60% of total
		AddHbarTransfer(toHederaDeviceID, hedera.HbarFrom(fortyPercent, hedera.HbarUnits.Millibar)). // 40% of total
		SetTransactionMemo(uuid.New().String()).
		FreezeWith(client)

	if err != nil {
		// Network error - queue invoice for later
		if isNetworkError(err) {
			log.Printf("⚠️ Network error creating transfer, queueing invoice for SharedAccID %d", sharedAccID.Account)
			queueInvoiceForLater(sharedAccID.Account, totalAmount, buyerStdIn.Topic)
			return nil // Don't fail - continue streaming
		}
		return err
	}

	// Prepare the transfer transaction to be scheduled
	scheduledTransferTx, err := transferTx.Schedule()
	if err != nil {
		if isNetworkError(err) {
			log.Printf("⚠️ Network error scheduling transfer, queueing invoice for SharedAccID %d", sharedAccID.Account)
			queueInvoiceForLater(sharedAccID.Account, totalAmount, buyerStdIn.Topic)
			return nil
		}
		return err
	}

	scheduledTxResponse, err := scheduledTransferTx.Execute(client)

	if err != nil {
		if isNetworkError(err) {
			log.Printf("⚠️ Network error executing scheduled transfer, queueing invoice for SharedAccID %d", sharedAccID.Account)
			queueInvoiceForLater(sharedAccID.Account, totalAmount, buyerStdIn.Topic)
			return nil
		}
		return err
	}

	receipt, err := scheduledTxResponse.GetReceipt(client)
	if err != nil {
		if isNetworkError(err) {
			log.Printf("⚠️ Network error getting receipt, queueing invoice for SharedAccID %d", sharedAccID.Account)
			queueInvoiceForLater(sharedAccID.Account, totalAmount, buyerStdIn.Topic)
			return nil
		}
		return err
	}

	scheduleId := receipt.ScheduleID
	fmt.Printf("receipt: %v  and  shed tx id: \n %v and sched id  %v \n", receipt, receipt.ScheduledTransactionID, scheduleId)

	// prepare message and send to the buyer's topic.

	m := &types.NeuronScheduleSignRequestMsg{
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
		if isNetworkError(err) {
			log.Printf("⚠️ Network error sending to topic, queueing invoice for SharedAccID %d", sharedAccID.Account)
			queueInvoiceForLater(sharedAccID.Account, totalAmount, buyerStdIn.Topic)
			return nil
		}
		return err
	}

	fmt.Println(txResponse)

	return nil
}

// isNetworkError checks if the error is a network-related error vs an account/validation error
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Network-related error patterns
	networkPatterns := []string{
		"connection refused",
		"connection reset",
		"network is unreachable",
		"timeout",
		"dial tcp",
		"no route to host",
		"i/o timeout",
		"context deadline exceeded",
		"EOF",
		"UNAVAILABLE",
		"RESOURCE_EXHAUSTED",
	}
	for _, pattern := range networkPatterns {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// queueInvoiceForLater adds an invoice to the pending queue
func queueInvoiceForLater(sharedAccID uint64, amount float64, buyerStdIn uint64) {
	// We don't have the peerID here, so we'll use a placeholder
	// The invoice will be identified by SharedAccID
	commonlib.InvoiceQueueMutex.Lock()
	defer commonlib.InvoiceQueueMutex.Unlock()

	invoice := commonlib.QueuedInvoice{
		SharedAccID: sharedAccID,
		Amount:      amount,
		BuyerStdIn:  buyerStdIn,
		QueuedAt:    time.Now(),
		RetryCount:  0,
	}

	commonlib.PendingInvoices = append(commonlib.PendingInvoices, invoice)
	log.Printf("📋 Queued invoice (SharedAccID: %d, Amount: %.2f) - queue size: %d",
		sharedAccID, amount, len(commonlib.PendingInvoices))
}

// StartInvoiceFlushWorker starts a background worker that periodically flushes queued invoices
// This should be called once during seller initialization
func StartInvoiceFlushWorker(stopChan <-chan struct{}) {
	log.Println("📋 Starting invoice flush worker (interval: 60s)")

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				FlushPendingInvoices()
			case <-stopChan:
				log.Println("📋 Invoice flush worker stopping, flushing remaining invoices...")
				FlushPendingInvoices()
				return
			}
		}
	}()
}

// FlushPendingInvoices attempts to send all queued invoices to Hedera
func FlushPendingInvoices() {
	invoices := commonlib.GetPendingInvoices()
	if len(invoices) == 0 {
		return
	}

	log.Printf("📋 Flushing %d pending invoices", len(invoices))

	var processedIndices []int

	for i, invoice := range invoices {
		// Skip invoices that have been retried too many times
		if invoice.RetryCount >= 5 {
			log.Printf("⚠️ Invoice for SharedAccID %d exceeded max retries, removing from queue", invoice.SharedAccID)
			processedIndices = append(processedIndices, i)
			continue
		}

		// Attempt to send the invoice
		err := retryInvoice(invoice)
		if err == nil {
			log.Printf("✅ Successfully flushed queued invoice for SharedAccID %d", invoice.SharedAccID)
			processedIndices = append(processedIndices, i)
		} else {
			log.Printf("⚠️ Failed to flush invoice for SharedAccID %d: %v", invoice.SharedAccID, err)
			commonlib.IncrementInvoiceRetry(i)
		}
	}

	// Remove successfully processed invoices
	if len(processedIndices) > 0 {
		commonlib.ClearProcessedInvoices(processedIndices)
		log.Printf("📋 Cleared %d processed invoices, %d remaining",
			len(processedIndices), commonlib.GetPendingInvoiceCount())
	}
}

// retryInvoice attempts to send a queued invoice
func retryInvoice(invoice commonlib.QueuedInvoice) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()

	// Create the account IDs
	sharedAccID := hedera.AccountID{Shard: 0, Realm: 0, Account: invoice.SharedAccID}
	buyerStdIn := hedera.TopicID{Shard: 0, Realm: 0, Topic: invoice.BuyerStdIn}

	// Get seller's accounts
	myDeviceAccountID, err := hedera.AccountIDFromEvmAddress(0, 0, os.Getenv("hedera_evm_id"))
	if err != nil {
		return fmt.Errorf("failed to get device account: %w", err)
	}

	myParentAccountID, err := GetDeviceParent(os.Getenv("hedera_evm_id"))
	if err != nil {
		return fmt.Errorf("failed to get parent account: %w", err)
	}

	// Get current balance
	accountInfo, err := GetAccountInfoFromNetwork(sharedAccID)
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}

	totalAmount := accountInfo.Balance.As(hedera.HbarUnits.Millibar)
	if totalAmount < 1 {
		log.Printf("⚠️ SharedAccID %d has insufficient balance (%.2f), skipping invoice", invoice.SharedAccID, totalAmount)
		return nil // Don't retry - mark as processed
	}

	sixtyPercent := float64(totalAmount) * 0.6
	fortyPercent := float64(totalAmount) * 0.4

	transferTx, err := hedera.NewTransferTransaction().
		AddHbarTransfer(sharedAccID, hedera.HbarFrom(-float64(totalAmount), hedera.HbarUnits.Millibar)).
		AddHbarTransfer(myParentAccountID, hedera.HbarFrom(sixtyPercent, hedera.HbarUnits.Millibar)).
		AddHbarTransfer(myDeviceAccountID, hedera.HbarFrom(fortyPercent, hedera.HbarUnits.Millibar)).
		SetTransactionMemo(uuid.New().String()).
		FreezeWith(client)

	if err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}

	scheduledTransferTx, err := transferTx.Schedule()
	if err != nil {
		return fmt.Errorf("failed to schedule transfer: %w", err)
	}

	scheduledTxResponse, err := scheduledTransferTx.Execute(client)
	if err != nil {
		return fmt.Errorf("failed to execute scheduled transfer: %w", err)
	}

	receipt, err := scheduledTxResponse.GetReceipt(client)
	if err != nil {
		return fmt.Errorf("failed to get receipt: %w", err)
	}

	scheduleId := receipt.ScheduleID

	// Send notification to buyer
	m := &types.NeuronScheduleSignRequestMsg{
		MessageType: "scheduleSignRequest",
		ScheduleID:  scheduleId.Schedule,
		SharedAccID: sharedAccID.Account,
		Version:     "0.4",
	}
	jsonBytes, _ := json.Marshal(m)

	_, err = hedera.NewTopicMessageSubmitTransaction().
		SetMessage(jsonBytes).
		SetTopicID(buyerStdIn).
		Execute(client)

	if err != nil {
		return fmt.Errorf("failed to send to topic: %w", err)
	}

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

func PeerSendErrorMessage(otherSideStdIn hedera.TopicID, errorType types.ErrorType, errorMessage string, recoverAction types.RecoverAction) {
	go func() {
		client := GetHederaClientUsingEnv()
		defer client.Close()
		m := &types.NeuronPeerErrorMsg{
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

func SendSelfErrorMessage(errorType types.ErrorType, errorMessage string, recoverAction types.RecoverAction) error {
	client := GetHederaClientUsingEnv()
	defer client.Close()
	m := &types.NeuronSelfErrorMsg{
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

func SendTransactionEnvelope(tx types.TopicPostalEnvelope) error {
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
	// Load last processed timestamp from database
	var lastStdInTimestamp time.Time
	topicKey := fmt.Sprintf("%d.%d.%d", topicID.Shard, topicID.Realm, topicID.Topic)

	if commonlib.GlobalStateManager != nil && !commonlib.GlobalStateManager.IsInDegradedMode() {
		loadedTime, err := commonlib.GlobalStateManager.LoadTopicPosition(topicKey)
		if err == nil && !loadedTime.IsZero() {
			lastStdInTimestamp = loadedTime
			log.Printf("Resuming topic %s from %s", topicKey, lastStdInTimestamp.Format(time.RFC3339))
		} else {
			// Default: start from 24 hours ago
			lastStdInTimestamp = time.Now().UTC().Add(-24 * time.Hour)
			log.Printf("No saved position for topic %s, starting from 24 hours ago", topicKey)
		}
	} else {
		// No state manager or degraded mode - start from now
		lastStdInTimestamp = time.Now().UTC()
		log.Printf("Starting topic %s from now (no persistence available)", topicKey)
	}

	// Wrap callback to persist timestamp after each message
	wrappedCallback := func(message hedera.TopicMessage) {
		callback(message) // Process message

		// Persist timestamp (batched - not critical)
		if commonlib.GlobalStateManager != nil {
			commonlib.GlobalStateManager.PersistTopicPosition(topicKey, message.ConsensusTimestamp)
		}
	}

	handle, err := subscribe(client, topicID, lastStdInTimestamp, wrappedCallback, messageReceived)
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
		AddHbarTransfer(client.GetOperatorAccountID(), hedera.HbarFrom(-amount, hedera.HbarUnits.Millibar)). // Send 3 HBAR
		AddHbarTransfer(sharedAccountID, hedera.HbarFrom(amount, hedera.HbarUnits.Millibar)).                // Receive 3 HBAR
		Execute(client)
	return err
}

// ValidateSharedAccount checks if a shared account exists and has sufficient balance
func ValidateSharedAccount(sharedAccID uint64, requiredBalanceMillibar int64) (bool, error) {
	if sharedAccID == 0 {
		return false, fmt.Errorf("invalid shared account ID: 0")
	}

	accountID := hedera.AccountID{Shard: 0, Realm: 0, Account: sharedAccID}
	accountInfo, err := GetAccountInfoFromNetwork(accountID)

	if err != nil {
		return false, fmt.Errorf("account not found on network: %v", err)
	}

	if accountInfo.AccountID.IsZero() {
		return false, fmt.Errorf("account is zero/invalid")
	}

	// Check balance (convert millibar to tinybar: 1 millibar = 100,000 tinybar)
	currentBalanceMillibar := accountInfo.Balance.As(hedera.HbarUnits.Millibar)
	if currentBalanceMillibar < float64(requiredBalanceMillibar) {
		return false, fmt.Errorf("insufficient balance: %.2f millibar, need %d millibar",
			currentBalanceMillibar, requiredBalanceMillibar)
	}

	log.Printf("SharedAccID %d validated: balance %.2f millibar", sharedAccID, currentBalanceMillibar)
	return true, nil
}
