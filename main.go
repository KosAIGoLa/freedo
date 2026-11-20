// TapeOut-on-X-Layer indexer — public leaderboard, circuit counts and
// new-circuit alerts for the TapeOut factory on X Layer.
//
// Built for X Layer TapeOut Accelerate 2026 (100,000 USDT; deadline
// 2026-12-07 23:59 UTC provisional). The official FAQ explicitly allows
// the Go part to run off-circuit: what must be on X Layer is the
// TapeOut usage itself — circuits, contracts, on-chain data — plus the
// relevant addresses in the submission.
//
// Read-only: only eth_call / eth_blockNumber. No wallet, no signing,
// no funds, no dependencies (Go standard library only).
//
// Usage:
//
//	go run .                                   # sync every 5m, serve :8080
//	go run . --port 9000 --interval 10m        # custom port / interval
//	go run . --block 0x4694380 --once          # reproduce the audit block
//	go run . --rpc https://rpc.xlayer.tech     # explicit RPC endpoint
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	rpcURL := flag.String("rpc", "https://rpc.xlayer.tech", "X Layer JSON-RPC URL")
	block := flag.String("block", "latest", "block tag or hex number, e.g. 0x4694380")
	interval := flag.Duration("interval", 5*time.Minute, "sync interval")
	port := flag.Int("port", 8080, "HTTP port")
	data := flag.String("data", "tapeout-indexer.json", "snapshot file path")
	once := flag.Bool("once", false, "sync once, print a summary and exit (no server)")
	flag.Parse()

	// PaaS hosts (Render, Fly.io, Railway, Koyeb, …) assign the HTTP
	// port via $PORT. Respect it when the --port flag was left at its
	// default; an explicit --port always wins.
	if *port == 8080 {
		if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 {
				*port = n
			} else {
				log.Printf("ignoring invalid PORT=%q, using %d", p, *port)
			}
		}
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	rpcURLForDashboard = *rpcURL

	client := NewRPCClient(*rpcURL)
	st := NewState(factoryAddr, *block)
	if err := st.Load(*data); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("no snapshot at %s, starting fresh", *data)
		} else {
			log.Printf("snapshot load failed (%v), starting fresh", err)
		}
	}

	sy := &Syncer{
		RPC: client, Factory: factoryAddr, Block: *block,
		State: st, DataPath: *data,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		if err := sy.Sync(ctx); err != nil {
			log.Printf("initial sync failed: %v", err)
			os.Exit(1)
		}
		printSummary(st)
		return
	}

	// Server mode: start listening first so the loaded snapshot is
	// served immediately, then sync in the background (a full sync
	// takes ~90 seconds at 430+ processors).
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      NewHandler(st),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()
	log.Printf("tapeout-indexer listening on :%d (rpc=%s block=%s interval=%s data=%s)",
		*port, *rpcURL, *block, *interval, *data)

	if err := sy.Sync(ctx); err != nil {
		log.Printf("initial sync failed: %v", err)
	}

	t := time.NewTicker(*interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
			if err := st.Save(*data); err != nil {
				log.Printf("final snapshot save failed: %v", err)
			}
			log.Printf("shutdown complete")
			return
		case <-t.C:
			if err := sy.Sync(ctx); err != nil {
				log.Printf("sync failed: %v", err)
			}
		}
	}
}

// printSummary prints the aggregate stats after a --once run.
func printSummary(st *State) {
	s := st.Stats()
	fmt.Printf("factory: %s\n", s.Factory)
	fmt.Printf("block: %s (%d)\n", s.Block, s.BlockNumber)
	fmt.Printf("processors: %d\n", s.Processors)
	fmt.Printf("circuits fabricated: %s\n", s.TotalCircuits)
	fmt.Printf("processors with zero circuits: %d\n", s.ZeroCircuits)
	fmt.Printf("median circuits: %s\n", s.MedianCircuits)
	fmt.Printf("top 3 share: %.1f%%\n", s.Top3SharePct)
	fmt.Printf("last sync: %s\n", s.LastSyncAt.Format(time.RFC3339))
	fmt.Println("top processors by circuits:")
	for i, h := range s.TopHolders {
		fmt.Printf("  %2d. %s circuits=%s (%.1f%%)\n", i+1, h.Address, h.Circuits, h.SharePct)
	}
}
