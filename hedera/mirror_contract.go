package hedera_helper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// mirrorContractCaller implements go-ethereum's bind.ContractCaller by routing
// read-only (eth_call-style) contract reads through the Hedera mirror node's
// /api/v1/contracts/call endpoint instead of a JSON-RPC relay (hashio). The same
// adaptive HTTP client also serves topic polling (GetJSON), so both contract
// reads and the StdIn topic listener share one transport.
//
// The mirror node already exposes everything we read from the Rendezvous
// contract, so this removes the hard dependency on eth_rpc_url / hashio. The
// generated bindings keep doing the ABI encode/decode; we only replace transport.
//
// DNS-resilient dialing: normally we resolve the mirror host via system DNS. On a
// network with a DNS-based content filter (e.g. a school Smoothwall that returns a
// block-server IP for the hostname), that resolution fails (untrusted cert / block
// page). So every SUCCESSFUL call records the real resolved IP to a small cache
// file; on failure we retry once pinned to that last-known-good IP, still sending
// the real hostname as TLS SNI so the cert is fully verified (no skip-verify). The
// cache only ever updates from successful (clean-DNS) calls, so it never latches
// onto the filter's IP. All cache I/O is best-effort: errors are ignored and we
// fall back to DNS / the seed IP, never blocking.
type mirrorContractCaller struct {
	baseURL   string
	host      string
	cachePath string
	seedIP    string
	client    *http.Client
}

const (
	defaultMirrorAPIURL  = "https://testnet.mirrornode.hedera.com/api/v1"
	defaultMirrorIPCache = "/etc/neuron-sdk/mirror-ip.cache"
	// Ultimate fallback if we've never recorded a good IP (e.g. first run while a
	// DNS filter is already active). Current testnet.mirrornode.hedera.com IP;
	// only consulted when DNS fails and the cache is empty.
	defaultMirrorSeedIP = "35.186.230.203"
)

var (
	sharedMirrorOnce sync.Once
	sharedMirrorInst *mirrorContractCaller
)

// sharedMirror returns the process-wide adaptive mirror client. Contract reads
// and the topic poller share it so they share one IP cache.
func sharedMirror() *mirrorContractCaller {
	sharedMirrorOnce.Do(func() { sharedMirrorInst = newMirrorContractCaller() })
	return sharedMirrorInst
}

func newMirrorContractCaller() *mirrorContractCaller {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("mirror_api_url")), "/")
	if base == "" {
		base = defaultMirrorAPIURL
	}
	cachePath := strings.TrimSpace(os.Getenv("mirror_ip_cache"))
	if cachePath == "" {
		cachePath = defaultMirrorIPCache
	}
	seedIP := strings.TrimSpace(os.Getenv("mirror_seed_ip"))
	if seedIP == "" {
		seedIP = defaultMirrorSeedIP
	}
	return &mirrorContractCaller{
		baseURL:   base,
		host:      mirrorHost(base),
		cachePath: cachePath,
		seedIP:    seedIP,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// mirrorHost extracts the hostname (no port) from the mirror base URL.
func mirrorHost(baseURL string) string {
	if u, err := url.Parse(baseURL); err == nil {
		return u.Hostname()
	}
	return ""
}

// adaptiveDo runs the request via system DNS first and, on failure, retries
// pinned to the last-known-good IP. Returns the body of a 200 JSON response.
func (m *mirrorContractCaller) adaptiveDo(ctx context.Context, method, path string, payload []byte, contentType string) ([]byte, error) {
	out, err := m.attempt(ctx, m.client, method, path, payload, contentType)
	if err == nil {
		m.recordGoodIP() // refresh last-known-good IP from this clean resolution
		return out, nil
	}
	if m.host != "" {
		if ip := m.fallbackIP(); ip != "" {
			log.Printf("mirror %s %s: DNS path failed (%v); retrying pinned to last-known-good IP %s", method, path, err, ip)
			if out2, err2 := m.attempt(ctx, m.pinnedClient(ip), method, path, payload, contentType); err2 == nil {
				return out2, nil
			} else {
				return nil, fmt.Errorf("mirror %s %s: failed via DNS (%v) and via pinned IP %s (%v)", method, path, err, ip, err2)
			}
		}
	}
	return nil, err
}

// attempt performs one request with the given client; success requires HTTP 200
// and a JSON-looking body (an HTML block page is treated as failure so the caller
// can fall back).
func (m *mirrorContractCaller) attempt(ctx context.Context, client *http.Client, method, path string, payload []byte, contentType string) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, snippet(body))
	}
	if t := strings.TrimSpace(string(body)); !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return nil, fmt.Errorf("non-JSON response (likely a block page): %s", snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 140 {
		s = s[:140]
	}
	return s
}

// GetJSON performs an adaptive GET against the mirror node (path relative to the
// base, e.g. "/topics/0.0.X/messages?...") and returns the JSON body.
func (m *mirrorContractCaller) GetJSON(ctx context.Context, path string) ([]byte, error) {
	return m.adaptiveDo(ctx, http.MethodGet, path, nil, "")
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
	body, err := m.adaptiveDo(ctx, http.MethodPost, "/contracts/call", payload, "application/json")
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: %w", err)
	}
	var parsed mirrorCallResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("mirror contract call: decode response: %w (body=%s)", err, snippet(body))
	}
	if parsed.Result == "" {
		return nil, fmt.Errorf("mirror contract call: empty result (body=%s)", snippet(body))
	}
	out, err := hexutil.Decode(parsed.Result)
	if err != nil {
		return nil, fmt.Errorf("mirror contract call: decode result hex %q: %w", parsed.Result, err)
	}
	return out, nil
}

// pinnedClient returns an http.Client that dials ip for the mirror host (any
// port), so the request bypasses DNS while TLS still uses the real hostname.
func (m *mirrorContractCaller) pinnedClient(ip string) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if h, port, err := net.SplitHostPort(addr); err == nil && h == m.host {
					addr = net.JoinHostPort(ip, port)
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}

// fallbackIP returns the cached last-known-good IP, or the seed if none. Errors
// are swallowed — a missing/unreadable cache just yields the seed.
func (m *mirrorContractCaller) fallbackIP() string {
	if b, err := os.ReadFile(m.cachePath); err == nil {
		if s := strings.TrimSpace(string(b)); net.ParseIP(s) != nil {
			return s
		}
	}
	return m.seedIP
}

// recordGoodIP resolves the mirror host (clean DNS, since the call just
// succeeded) and persists the first IPv4 to the cache file. Best-effort: any
// resolution or write error is ignored and never blocks the caller.
func (m *mirrorContractCaller) recordGoodIP() {
	if m.host == "" || m.cachePath == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, m.host)
	if err != nil || len(ips) == 0 {
		return
	}
	ip := ips[0]
	for _, cand := range ips { // prefer IPv4 (the mirror is reachable over v4)
		if p := net.ParseIP(cand); p != nil && p.To4() != nil {
			ip = cand
			break
		}
	}
	if b, err := os.ReadFile(m.cachePath); err == nil && strings.TrimSpace(string(b)) == ip {
		return // unchanged; skip the write
	}
	tmp := m.cachePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(ip+"\n"), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, m.cachePath) // atomic; ignore failure
}
