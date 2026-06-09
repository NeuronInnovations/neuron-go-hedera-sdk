package hedera_helper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// mirrorContractCaller implements go-ethereum's bind.ContractCaller by routing
// read-only (eth_call-style) contract reads through the Hedera mirror node's
// /api/v1/contracts/call endpoint instead of a JSON-RPC relay (hashio).
//
// The mirror node already exposes everything we read from the Rendezvous
// contract, so this removes the hard dependency on eth_rpc_url / hashio. The
// generated bindings in the hederacontract package keep doing all the ABI
// encode/decode work; we only replace the transport. See the 4dsky-explorer-api
// (src/services/mirrorapi.ts, src/helpers/contractCodec.ts) for the same
// approach in TypeScript.
type mirrorContractCaller struct {
	baseURL string
	http    *http.Client
}

const defaultMirrorAPIURL = "https://testnet.mirrornode.hedera.com/api/v1"

func newMirrorContractCaller() *mirrorContractCaller {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("mirror_api_url")), "/")
	if base == "" {
		base = defaultMirrorAPIURL
	}
	return &mirrorContractCaller{
		baseURL: base,
		// Upper bound for calls made with a context-less bind.CallOpts. Calls
		// that do pass a context (e.g. fetchPeerInfoWithRetry) get their own,
		// tighter per-attempt deadline which fires first.
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// CodeAt is only consulted by bind.BoundContract.Call when a call returns empty
// output, to tell a missing contract apart from an out-of-sync chain. Our view
// calls always return non-empty ABI-encoded data, so returning a non-empty stub
// is sufficient and avoids a spurious mirror round-trip.
func (m *mirrorContractCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	return []byte{0x01}, nil
}

type mirrorCallRequest struct {
	Data     string `json:"data"`
	To       string `json:"to"`
	Estimate bool   `json:"estimate"`
}

type mirrorCallResponse struct {
	Result string `json:"result"`
}

// CallContract POSTs an ABI-encoded read call to the mirror node and returns the
// raw result bytes for the bindings to decode.
func (m *mirrorContractCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if call.To == nil {
		return nil, fmt.Errorf("mirror contract call: missing destination contract address")
	}

	reqBody := mirrorCallRequest{
		Data:     hexutil.Encode(call.Data),
		To:       call.To.Hex(),
		Estimate: false,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: marshal request: %w", err)
	}

	url := m.baseURL + "/contracts/call"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mirror contract call: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed mirrorCallResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("mirror contract call: decode response: %w (body=%s)", err, strings.TrimSpace(string(body)))
	}
	if parsed.Result == "" {
		return nil, fmt.Errorf("mirror contract call: empty result (body=%s)", strings.TrimSpace(string(body)))
	}

	out, err := hexutil.Decode(parsed.Result)
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: decode result hex %q: %w", parsed.Result, err)
	}
	return out, nil
}
