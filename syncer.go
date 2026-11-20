// syncer.go: poll the TapeOut factory and merge the results into state.
//
// Enumeration mirrors the published audit (openwitness, block 74,007,424)
// and the verified spike in ~/workspace/tapeout-spike/:
//   - cpuCount()  0xa94da8a7 -> number of processors deployed
//   - cpuAt(i)    0x4bc7cbbd -> processor contract address #i
//   - transistors() 0x6fbd1719 -> ERC-1155 transistor contract of a processor
//   - nextId()    0x61b8ce8c -> circuits fabricated by a processor
//   - on a transistor contract: price 0x6817c76c, minted 0x4f02c420,
//     cap 0x8f770ad0
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"
)

const (
	factoryAddr = "0x1f09daefa827f02cbb40967cc91b259763760761"
	selCount    = "0xa94da8a7" // cpuCount()
	selAt       = "0x4bc7cbbd" // cpuAt(uint256)
	selTrans    = "0x6fbd1719" // transistors()
	selNext     = "0x61b8ce8c" // nextId()
	selPrice    = "0x6817c76c" // price() on a transistor contract
	selMint     = "0x4f02c420" // minted()
	selCap      = "0x8f770ad0" // cap()
)

const zeroAddress = "0x0000000000000000000000000000000000000000"

// Syncer polls the factory on an interval and persists snapshots.
type Syncer struct {
	RPC      *RPCClient
	Factory  string
	Block    string // "latest" or hex block number
	State    *State
	DataPath string
}

// Sync runs one full enumeration and merges it into the state.
// Enumeration failures (cpuCount / cpuAt) abort the sync; per-processor
// detail failures are tolerated and recorded as sync errors.
func (sy *Syncer) Sync(ctx context.Context) error {
	start := time.Now()
	sy.State.mu.Lock()
	sy.State.Syncs++
	sy.State.mu.Unlock()

	err := sy.sync(ctx, start)
	took := time.Since(start)
	sy.State.mu.Lock()
	sy.State.LastSyncTook = took
	if err != nil {
		sy.State.SyncErrors++
		sy.State.LastSyncErr = err.Error()
	} else {
		sy.State.LastSyncErr = ""
	}
	sy.State.mu.Unlock()
	if err != nil {
		return err
	}
	if sy.DataPath != "" {
		if err := sy.State.Save(sy.DataPath); err != nil {
			log.Printf("snapshot save failed: %v", err)
		}
	}
	return nil
}

func (sy *Syncer) sync(ctx context.Context, start time.Time) error {
	// Resolve the block number for the snapshot.
	var blockNum uint64
	if sy.Block == "latest" {
		n, err := sy.RPC.BlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("eth_blockNumber: %w", err)
		}
		blockNum = n
	} else if n, ok := ParseHexUint(sy.Block); ok {
		blockNum = n
	}

	// cpuCount().
	countHex, err := sy.RPC.Call(ctx, sy.Factory, selCount, sy.Block)
	if err != nil {
		return fmt.Errorf("cpuCount: %w", err)
	}
	n := int(ParseWord(countHex, 0).Int64())
	if n < 0 {
		return fmt.Errorf("cpuCount returned negative %d", n)
	}

	// cpuAt(i) in batches of 10.
	addrs := make([]string, 0, n)
	for i := 0; i < n; i += 10 {
		end := i + 10
		if end > n {
			end = n
		}
		pairs := make([][2]string, 0, end-i)
		for j := i; j < end; j++ {
			pairs = append(pairs, [2]string{sy.Factory, selAt + fmt.Sprintf("%064x", j)})
		}
		res, err := sy.RPC.CallBatch(ctx, pairs, sy.Block)
		if err != nil {
			return fmt.Errorf("cpuAt batch %d-%d: %w", i, end, err)
		}
		for _, r := range res {
			addrs = append(addrs, ParseAddr(r))
		}
	}

	// nextId() per processor (circuits fabricated). Tolerate individual
	// failures: a failed read counts as 0 circuits for that processor.
	circuits := make([]string, n)
	{
		pairs := make([][2]string, 0, n)
		for j := 0; j < n; j++ {
			pairs = append(pairs, [2]string{addrs[j], selNext})
		}
		res, errs := sy.RPC.CallBatchLenient(ctx, pairs, sy.Block)
		failed := 0
		for j := range res {
			if errs[j] != nil {
				failed++
				circuits[j] = "0"
				continue
			}
			circuits[j] = ParseWord(res[j], 0).String()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if failed > n/20 { // more than 5% failed: do not merge partial data
			return fmt.Errorf("nextId: %d/%d reads failed, aborting sync", failed, n)
		}
		if failed > 0 {
			log.Printf("nextId: %d/%d reads failed, counted as 0 circuits", failed, n)
		}
	}

	// transistors() per processor.
	transistors := make([]string, n)
	{
		pairs := make([][2]string, 0, n)
		for j := 0; j < n; j++ {
			pairs = append(pairs, [2]string{addrs[j], selTrans})
		}
		res, errs := sy.RPC.CallBatchLenient(ctx, pairs, sy.Block)
		for j := range res {
			if errs[j] != nil {
				transistors[j] = ""
				continue
			}
			a := ParseAddr(res[j])
			if a == zeroAddress {
				transistors[j] = ""
			} else {
				transistors[j] = a
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// price / minted / cap on each transistor contract (3 calls each).
	// Processors without a transistor contract get empty values.
	details := make([][3]string, n)
	{
		pairs := make([][2]string, 0, 3*n)
		owner := make([]int, 0, 3*n)
		for j := 0; j < n; j++ {
			if transistors[j] == "" {
				continue
			}
			pairs = append(pairs,
				[2]string{transistors[j], selPrice},
				[2]string{transistors[j], selMint},
				[2]string{transistors[j], selCap},
			)
			owner = append(owner, j, j, j)
		}
		if len(pairs) > 0 {
			res, errs := sy.RPC.CallBatchLenient(ctx, pairs, sy.Block)
			for k := range res {
				if errs[k] != nil {
					continue
				}
				slot := k % 3
				details[owner[k]][slot] = ParseWord(res[k], 0).String()
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	fetched := make([]*Processor, 0, n)
	for j := 0; j < n; j++ {
		p := &Processor{
			Address:    addrs[j],
			Index:      j,
			Circuits:   circuits[j],
			Transistor: transistors[j],
			Price:      details[j][0],
			Minted:     details[j][1],
			Cap:        details[j][2],
		}
		// Normalize "0" price/minted/cap to empty when the contract has
		// no values recorded, so the dashboard shows a dash instead of 0.
		if p.Price == "0" && p.Minted == "0" && p.Cap == "0" {
			p.Price, p.Minted, p.Cap = "", "", ""
		}
		fetched = append(fetched, p)
	}

	generated := sy.State.Merge(fetched, blockNum, start)
	total := big.NewInt(0)
	for _, p := range fetched {
		if c, ok := new(big.Int).SetString(p.Circuits, 10); ok {
			total.Add(total, c)
		}
	}
	log.Printf("sync #%d: %d processors, %s circuits, %d new alerts, block %d, took %s",
		sy.State.Seq(), n, total, len(generated), blockNum, time.Since(start))
	return nil
}
