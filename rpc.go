// rpc.go: minimal JSON-RPC client over HTTP (eth_call, eth_blockNumber).
//
// The indexer is read-only: it only ever sends eth_call and
// eth_blockNumber. No transactions, no signing, no funds.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const defaultBatchSize = 10

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// RPCClient talks to one X Layer JSON-RPC endpoint.
type RPCClient struct {
	URL        string
	HTTP       *http.Client
	BatchSize  int
	MaxRetries int
}

func NewRPCClient(url string) *RPCClient {
	return &RPCClient{
		URL:        url,
		HTTP:       &http.Client{Timeout: 45 * time.Second},
		BatchSize:  defaultBatchSize,
		MaxRetries: 3,
	}
}

// post sends one JSON-RPC request body and returns the raw response body.
func (c *RPCClient) post(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc %s: HTTP %d: %.200s", c.URL, resp.StatusCode, raw)
	}
	return raw, nil
}

// parseBatchResponse decodes a batch response (an array, or a single
// object when the endpoint unwraps size-1 batches) into results indexed
// by request ID. It fails if any item carries an error or is missing.
func parseBatchResponse(raw []byte, want int) ([]string, error) {
	results, errs := parseBatchResponseLenient(raw, want)
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// parseBatchResponseLenient decodes a batch response and returns one
// result and one error slot per requested call. A per-item error is
// recorded in errs[i] instead of failing the whole batch.
func parseBatchResponseLenient(raw []byte, want int) ([]string, []error) {
	results := make([]string, want)
	errs := make([]error, want)
	for i := range errs {
		errs[i] = fmt.Errorf("missing result for call %d", i)
	}

	var out []rpcResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		var single rpcResponse
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			for i := range errs {
				errs[i] = fmt.Errorf("unexpected RPC response: %.200s", raw)
			}
			return results, errs
		}
		out = []rpcResponse{single}
	}
	for _, r := range out {
		if r.ID < 0 || r.ID >= want {
			continue
		}
		if r.Error != nil {
			errs[r.ID] = r.Error
			continue
		}
		if len(r.Result) == 0 || string(r.Result) == "null" {
			results[r.ID] = ""
			errs[r.ID] = nil
			continue
		}
		var s string
		if err := json.Unmarshal(r.Result, &s); err != nil {
			errs[r.ID] = fmt.Errorf("bad result for call %d: %w", r.ID, err)
			continue
		}
		results[r.ID] = s
		errs[r.ID] = nil
	}
	return results, errs
}

func (c *RPCClient) callChunk(ctx context.Context, pairs [][2]string, block string) ([]string, []error) {
	reqs := make([]rpcRequest, len(pairs))
	for i, p := range pairs {
		reqs[i] = rpcRequest{JSONRPC: "2.0", ID: i, Method: "eth_call",
			Params: []any{map[string]string{"to": p[0], "data": p[1]}, block}}
	}
	body, err := json.Marshal(reqs)
	if err != nil {
		errs := make([]error, len(pairs))
		for i := range errs {
			errs[i] = err
		}
		return make([]string, len(pairs)), errs
	}
	raw, err := c.post(ctx, body)
	if err != nil {
		errs := make([]error, len(pairs))
		for i := range errs {
			errs[i] = err
		}
		return make([]string, len(pairs)), errs
	}
	return parseBatchResponseLenient(raw, len(pairs))
}

// callBatchItems runs eth_calls in batches of at most BatchSize and
// returns one result and one error slot per requested call. A chunk is
// retried up to MaxRetries times; on persistent failure it falls back
// to single calls so one bad item does not lose the whole chunk.
func (c *RPCClient) callBatchItems(ctx context.Context, pairs [][2]string, block string) ([]string, []error) {
	results := make([]string, len(pairs))
	errs := make([]error, len(pairs))
	for start := 0; start < len(pairs); start += c.BatchSize {
		end := start + c.BatchSize
		if end > len(pairs) {
			end = len(pairs)
		}
		chunk := pairs[start:end]
		var res []string
		var cerrs []error
		for attempt := 0; attempt <= c.MaxRetries; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					// Mark every item from this chunk onward as
					// failed: callers must never see a nil error for
					// a call that was never attempted.
					for i := start; i < len(pairs); i++ {
						errs[i] = ctx.Err()
					}
					return results, errs
				case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
				}
			}
			res, cerrs = c.callChunk(ctx, chunk, block)
			failed := false
			for _, e := range cerrs {
				if e != nil {
					failed = true
					break
				}
			}
			if !failed {
				break
			}
		}
		// Fall back to single calls for the items that still failed.
		for i, e := range cerrs {
			if e == nil {
				results[start+i] = res[i]
				continue
			}
			s, err := c.callSingle(ctx, chunk[i], block)
			if err != nil {
				errs[start+i] = err
				continue
			}
			results[start+i] = s
		}
	}
	return results, errs
}

// callSingle runs one eth_call as a one-element batch, with one retry.
func (c *RPCClient) callSingle(ctx context.Context, pair [2]string, block string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= 1; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		res, errs := c.callChunk(ctx, [][2]string{pair}, block)
		if errs[0] == nil {
			return res[0], nil
		}
		lastErr = errs[0]
	}
	return "", lastErr
}

// CallBatch runs eth_calls and returns their results in order. It fails
// if any call errors.
func (c *RPCClient) CallBatch(ctx context.Context, pairs [][2]string, block string) ([]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	results, errs := c.callBatchItems(ctx, pairs, block)
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// CallBatchLenient runs eth_calls and returns one result and one error
// slot per call, so the caller can tolerate individual failures.
func (c *RPCClient) CallBatchLenient(ctx context.Context, pairs [][2]string, block string) ([]string, []error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	return c.callBatchItems(ctx, pairs, block)
}

// Call is a single eth_call convenience wrapper.
func (c *RPCClient) Call(ctx context.Context, to, data, block string) (string, error) {
	res, err := c.CallBatch(ctx, [][2]string{{to, data}}, block)
	if err != nil {
		return "", err
	}
	return res[0], nil
}

// BlockNumber returns the latest block number (eth_blockNumber).
func (c *RPCClient) BlockNumber(ctx context.Context) (uint64, error) {
	body, err := json.Marshal([]rpcRequest{{JSONRPC: "2.0", ID: 0, Method: "eth_blockNumber", Params: []any{}}})
	if err != nil {
		return 0, err
	}
	raw, err := c.post(ctx, body)
	if err != nil {
		return 0, err
	}
	// The endpoint may answer with a single object or a one-element
	// array; accept both.
	var resp rpcResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		var arr []rpcResponse
		if err2 := json.Unmarshal(raw, &arr); err2 != nil || len(arr) == 0 {
			return 0, fmt.Errorf("unexpected eth_blockNumber response: %.200s", raw)
		}
		resp = arr[0]
	}
	if resp.Error != nil {
		return 0, resp.Error
	}
	var s string
	if err := json.Unmarshal(resp.Result, &s); err != nil {
		return 0, err
	}
	n, ok := ParseHexUint(s)
	if !ok {
		return 0, fmt.Errorf("bad block number %q", s)
	}
	return n, nil
}

// ParseWord parses the i-th 32-byte word of an eth_call return value as a
// big integer. Short or malformed values parse as 0.
func ParseWord(hexStr string, i int) *big.Int {
	s := strings.TrimPrefix(hexStr, "0x")
	start := i * 64
	if len(s) < start+64 {
		return big.NewInt(0)
	}
	n, ok := new(big.Int).SetString(s[start:start+64], 16)
	if !ok {
		return big.NewInt(0)
	}
	return n
}

// ParseAddr parses the first 32-byte word of an eth_call return value as
// an Ethereum address (low 20 bytes).
func ParseAddr(hexStr string) string {
	s := strings.TrimPrefix(hexStr, "0x")
	if len(s) < 64 {
		return "0x0000000000000000000000000000000000000000"
	}
	return "0x" + s[24:64]
}

// ParseHexUint parses a 0x-prefixed hex quantity into a uint64.
func ParseHexUint(s string) (uint64, bool) {
	n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok || !n.IsUint64() {
		return 0, false
	}
	return n.Uint64(), true
}
