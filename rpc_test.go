package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseWord(t *testing.T) {
	hex := "0x" + strings.Repeat("0", 62) + "2a" // 42 in the first word
	if got := ParseWord(hex, 0); got.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("word 0 = %s, want 42", got)
	}
	// Second word.
	hex2 := "0x" + strings.Repeat("0", 64) + strings.Repeat("0", 62) + "07"
	if got := ParseWord(hex2, 1); got.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("word 1 = %s, want 7", got)
	}
	// Short input parses as 0, never panics.
	if got := ParseWord("0x1234", 0); got.Sign() != 0 {
		t.Fatalf("short input = %s, want 0", got)
	}
	if got := ParseWord("", 3); got.Sign() != 0 {
		t.Fatalf("empty input = %s, want 0", got)
	}
}

func TestParseAddr(t *testing.T) {
	// Address in the low 20 bytes of the first word.
	hex := "0x000000000000000000000000b55eaf4645da77dda4c054b0e36e104d6338093c"
	want := "0xb55eaf4645da77dda4c054b0e36e104d6338093c"
	if got := ParseAddr(hex); got != want {
		t.Fatalf("addr = %s, want %s", got, want)
	}
	if got := ParseAddr("0x"); got != "0x0000000000000000000000000000000000000000" {
		t.Fatalf("short addr = %s, want zero address", got)
	}
}

func TestParseHexUint(t *testing.T) {
	if n, ok := ParseHexUint("0x4694380"); !ok || n != 74007424 {
		t.Fatalf("0x4694380 = %d, ok=%v, want 74007424", n, ok)
	}
	if _, ok := ParseHexUint("not-hex"); ok {
		t.Fatal("garbage parsed as a number")
	}
}

// TestParseBatchResponse covers array responses, the single-object
// unwrap, per-item errors and missing results.
func TestParseBatchResponse(t *testing.T) {
	raw := []byte(`[{"jsonrpc":"2.0","id":0,"result":"0x01"},{"jsonrpc":"2.0","id":1,"result":"0x02"}]`)
	res, err := parseBatchResponse(raw, 2)
	if err != nil {
		t.Fatalf("array response: %v", err)
	}
	if res[0] != "0x01" || res[1] != "0x02" {
		t.Fatalf("results = %v", res)
	}

	// Single-object response (endpoint unwraps size-1 batches).
	raw = []byte(`{"jsonrpc":"2.0","id":0,"result":"0x2a"}`)
	res, err = parseBatchResponse(raw, 1)
	if err != nil || res[0] != "0x2a" {
		t.Fatalf("single-object response: res=%v err=%v", res, err)
	}

	// Per-item error fails the strict parser...
	raw = []byte(`[{"jsonrpc":"2.0","id":0,"result":"0x01"},{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}]`)
	if _, err := parseBatchResponse(raw, 2); err == nil {
		t.Fatal("strict parser accepted an item error")
	}
	// ...but the lenient parser keeps the good item and reports the bad one.
	res, errs := parseBatchResponseLenient(raw, 2)
	if res[0] != "0x01" || errs[0] != nil {
		t.Fatalf("lenient item 0: res=%q err=%v", res[0], errs[0])
	}
	if errs[1] == nil || !strings.Contains(errs[1].Error(), "execution reverted") {
		t.Fatalf("lenient item 1: err=%v, want execution reverted", errs[1])
	}

	// Missing result fails.
	raw = []byte(`[{"jsonrpc":"2.0","id":0,"result":"0x01"}]`)
	if _, err := parseBatchResponse(raw, 2); err == nil {
		t.Fatal("missing result accepted")
	}
}

// TestCallBatchChunking verifies batches are capped at 10 calls and
// results come back in request order, using a fake RPC server.
func TestCallBatchChunking(t *testing.T) {
	var batchSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqs []rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
			t.Errorf("bad request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		batchSizes = append(batchSizes, len(reqs))
		out := make([]rpcResponse, len(reqs))
		for i, q := range reqs {
			// Echo the call data back as the result, so the test can
			// check global ordering across chunks.
			var data string
			if len(q.Params) > 0 {
				if m, ok := q.Params[0].(map[string]any); ok {
					data, _ = m["data"].(string)
				}
			}
			out[i] = rpcResponse{ID: q.ID, Result: json.RawMessage(fmt.Sprintf(`%q`, data))}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	c := NewRPCClient(srv.URL)
	c.MaxRetries = 0
	pairs := make([][2]string, 25)
	for i := range pairs {
		pairs[i] = [2]string{"0x0000000000000000000000000000000000000001", fmt.Sprintf("0x%04x", i)}
	}
	res, err := c.CallBatch(context.Background(), pairs, "latest")
	if err != nil {
		t.Fatalf("CallBatch: %v", err)
	}
	if len(res) != 25 {
		t.Fatalf("len = %d, want 25", len(res))
	}
	for i, r := range res {
		want := fmt.Sprintf("0x%04x", i)
		if r != want {
			t.Fatalf("res[%d] = %s, want %s", i, r, want)
		}
	}
	for _, n := range batchSizes {
		if n > 10 {
			t.Fatalf("batch of %d calls, want at most 10", n)
		}
	}
	if len(batchSizes) != 3 {
		t.Fatalf("got %d batches, want 3 (10+10+5)", len(batchSizes))
	}
}

// TestCallBatchSingleFallback: the server rejects multi-call batches
// with HTTP 500 but answers single-element batches, so the client must
// fall back to one call at a time.
func TestCallBatchSingleFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqs []rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&reqs)
		if len(reqs) > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("too many RPC calls in batch request"))
			return
		}
		out := []rpcResponse{{ID: reqs[0].ID, Result: json.RawMessage(`"0x05"`)}}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	c := NewRPCClient(srv.URL)
	c.MaxRetries = 0
	pairs := [][2]string{
		{"0x0000000000000000000000000000000000000001", "0xaaaa"},
		{"0x0000000000000000000000000000000000000002", "0xbbbb"},
		{"0x0000000000000000000000000000000000000003", "0xcccc"},
	}
	res, err := c.CallBatch(context.Background(), pairs, "latest")
	if err != nil {
		t.Fatalf("CallBatch with fallback: %v", err)
	}
	for i, r := range res {
		if r != "0x05" {
			t.Fatalf("res[%d] = %s, want 0x05", i, r)
		}
	}
}

func TestBlockNumber(t *testing.T) {
	// Single-object response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":0,"result":"0x4694380"}`))
	}))
	defer srv.Close()

	c := NewRPCClient(srv.URL)
	n, err := c.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("BlockNumber: %v", err)
	}
	if n != 74007424 {
		t.Fatalf("block = %d, want 74007424", n)
	}

	// One-element array response (what rpc.xlayer.tech actually returns).
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","result":"0x48c160a","id":0}]`))
	}))
	defer srv2.Close()

	c2 := NewRPCClient(srv2.URL)
	n, err = c2.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("BlockNumber (array form): %v", err)
	}
	if n != 0x48c160a {
		t.Fatalf("block = %d, want %d", n, 0x48c160a)
	}
}

// TestCallBatchLenientCancelled: with an already-cancelled context every
// item must report an error — a caller must never see a nil error for a
// call that was never attempted (regression test: a cancelled sync once
// merged partial data as if it were complete).
func TestCallBatchLenientCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":0,"result":"0x01"}]`))
	}))
	defer srv.Close()

	c := NewRPCClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pairs := make([][2]string, 25)
	for i := range pairs {
		pairs[i] = [2]string{"0x0000000000000000000000000000000000000001", "0x1234"}
	}
	_, errs := c.CallBatchLenient(ctx, pairs, "latest")
	if len(errs) != 25 {
		t.Fatalf("len(errs) = %d, want 25", len(errs))
	}
	for i, err := range errs {
		if err == nil {
			t.Fatalf("errs[%d] is nil for a call that never ran", i)
		}
	}
}
