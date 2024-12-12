package hedera_helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashgraph/hedera-sdk-go/v2"
)

type HCSMessage struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"time"`
}

func GetAccountInfoFromMirror(accountID hedera.AccountID) (struct {
	Balance    float64 `json:"balance"`
	EvmAddress string  `json:"evm_address"`
	PublicKey  string  `json:"public_key"`
}, error) {

	var empty = struct {
		Balance    float64 `json:"balance"`
		EvmAddress string  `json:"evm_address"`
		PublicKey  string  `json:"public_key"`
	}{}

	mirrorApiUrl := os.Getenv("mirror_api_url")
	mirrorUrl := fmt.Sprintf("%s/accounts/%s", mirrorApiUrl, accountID)

	resp, err := http.Get(mirrorUrl)
	if err != nil {
		fmt.Println("Error:", err)
		return empty, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error:", err)
		return empty, err
	}
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println("Error:", err)
		return empty, err
	}
	if data["balance"] == nil {
		return empty, errors.New("account doesn't exist")
	}

	balance := data["balance"].(map[string]interface{})["balance"].(float64)
	evmAddress := data["evm_address"].(string)
	publicKey := data["key"].(map[string]interface{})["key"].(string)
	return struct {
		Balance    float64 `json:"balance"`
		EvmAddress string  `json:"evm_address"`
		PublicKey  string  `json:"public_key"`
	}{balance, evmAddress, publicKey}, nil
}

func GetAllDevicesFromExplorer() ([]map[string]interface{}, error) {
	explorerUrl := os.Getenv("neuron_explorer_url")

	resp, err := http.Get(explorerUrl)
	if err != nil {
		fmt.Println("Error fetching data:", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return nil, err
	}

	var devices []map[string]interface{}

	err = json.Unmarshal(body, &devices)
	if err != nil {
		fmt.Println("Error unmarshaling JSON:", err)
		return nil, err
	}

	return devices, nil
}

func GetLastMessageFromTopic(topicID hedera.TopicID) (HCSMessage, error) {

	mirrorApiUrl := os.Getenv("mirror_api_url")
	mirrorUrl := fmt.Sprintf("%s/topics/%s/messages?limit=100&order=desc", mirrorApiUrl, topicID)
	resp, err := http.Get(mirrorUrl)
	if err != nil {
		fmt.Println("Error:", err)
		return HCSMessage{}, err
	}
	// get the json from the response
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error:", err)
		return HCSMessage{}, err
	}
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println("Error:", err)
		return HCSMessage{}, err
	}
	// get the last message

	messages, ok := data["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return HCSMessage{}, errors.New("no messages found")
	}

	lastMessage := data["messages"].([]interface{})[0].(map[string]interface{})["message"]
	timestamp := data["messages"].([]interface{})[0].(map[string]interface{})["consensus_timestamp"]
	message := lastMessage.(string)
	seconds, _ := strconv.ParseInt(strings.Split(timestamp.(string), ".")[0], 10, 64)
	return HCSMessage{Message: message, Timestamp: time.Unix(seconds, 0)}, nil
}

type AccountResponse struct {
	CreatedTimestamp string `json:"created_timestamp"`
}

type TransactionsResponse struct {
	Transactions []Transaction `json:"transactions"`
}

type Transaction struct {
	TransactionID string `json:"transaction_id"`
}

func GetDeviceParent(accountEvnID string) (hedera.AccountID, error) {
	accountURL := fmt.Sprintf("https://testnet.mirrornode.hedera.com/api/v1/accounts/%s", accountEvnID) //TODO: get mirror url from env

	// Step 1: Get account information
	accountResp, err := http.Get(accountURL)
	if err != nil {
		fmt.Println("Error fetching account info:", err)
		return hedera.AccountID{}, err
	}
	defer accountResp.Body.Close()

	if accountResp.StatusCode != http.StatusOK {
		fmt.Println("Failed to get account info:", accountResp.Status)
		return hedera.AccountID{}, err
	}

	accountBody, err := io.ReadAll(accountResp.Body)
	if err != nil {
		fmt.Println("Error reading account response body:", err)
		return hedera.AccountID{}, err
	}

	var accountData AccountResponse
	if err := json.Unmarshal(accountBody, &accountData); err != nil {
		fmt.Println("Error unmarshalling account response:", err)
		return hedera.AccountID{}, err
	}

	// Step 2: Query transactions with the creation timestamp
	timestamp := accountData.CreatedTimestamp
	transactionsURL := fmt.Sprintf("https://testnet.mirrornode.hedera.com/api/v1/transactions/?timestamp=%s", timestamp) // TODO: get mirror url from env

	transactionsResp, err := http.Get(transactionsURL)
	if err != nil {
		fmt.Println("Error fetching transactions:", err)
		return hedera.AccountID{}, err
	}
	defer transactionsResp.Body.Close()

	if transactionsResp.StatusCode != http.StatusOK {
		fmt.Println("Failed to get transactions:", transactionsResp.Status)
		return hedera.AccountID{}, err
	}

	transactionsBody, err := ioutil.ReadAll(transactionsResp.Body)
	if err != nil {
		fmt.Println("Error reading transactions response body:", err)
		return hedera.AccountID{}, err
	}

	var transactionsData TransactionsResponse
	if err := json.Unmarshal(transactionsBody, &transactionsData); err != nil {
		fmt.Println("Error unmarshalling transactions response:", err)
		return hedera.AccountID{}, err
	}

	if len(transactionsData.Transactions) == 0 {
		fmt.Println("No transactions found for the given timestamp")
		return hedera.AccountID{}, err
	}

	// Step 3: Extract the account creator from the transaction ID
	transactionID := transactionsData.Transactions[0].TransactionID
	parts := strings.Split(transactionID, "-")
	if len(parts) > 0 {
		accountCreatorID := parts[0]
		fmt.Printf("The creator of the account %s is: %s\n", accountEvnID, accountCreatorID)
		i, _ := hedera.AccountIDFromString(accountCreatorID)
		return i, nil
	} else {
		fmt.Println("Failed to parse transaction ID for account creator")
		return hedera.AccountID{}, errors.New("failed to parse transaction ID for account creator")
	}

}
