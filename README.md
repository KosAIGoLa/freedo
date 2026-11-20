# TapeOut on X Layer — Public Indexer

A public indexer for every processor deployed by the TapeOut factory on
X Layer: a processor leaderboard, circuit counts, transistor price /
minted / cap, new-processor and new-circuit alerts, a REST API and a
minimal dashboard. Go, standard library only — no dependencies, no
database, no wallet, no signing, no funds. Read-only `eth_call`.

Built for **X Layer TapeOut Accelerate 2026** (100,000 USDT prize pool:
1st 20,000 ×1, 2nd 10,000 ×5, 3rd 6,000 ×5; deadline 2026-12-07
23:59 UTC provisional; online; solo OK; prizes paid in stages against
milestones; shortlisted teams attend an offline Demo Day). Realistic
target: 3rd place, 6,000 USDT. The official FAQ explicitly allows the
Go part to run off-circuit — what must be on X Layer is the TapeOut
usage itself (circuits, contracts, on-chain data) plus the relevant
addresses in the submission.

## Verified live numbers

Verified 2026-11-19 ~11:20 UTC against the public RPC
`https://rpc.xlayer.tech` (plain JSON-RPC `eth_call`, batches ≤ 10):

| Check | Result |
|---|---|
| Pinned block `0x4694380` (audit block 74,007,424) | **389 processors, 11,006 circuits, 111 zero-circuit, median 2, top-3 share 79.5%** — exact match with the openwitness audit (2026-10-28) |
| Top 3 by circuits at the pinned block | 6,424 / 1,300 / 1,022 |
| Latest block (76,289,792 at verification) | **430 processors, 11,962 circuits, 116 zero-circuit, median 2, top-3 share 79.2%** |
| Top 3 at latest | 7,054 / 1,396 / 1,022 (same three addresses, still growing) |

Factory: `0x1f09daefa827f02cbb40967cc91b259763760761`
(`cpuCount()` `0xa94da8a7`, `cpuAt()` `0x4bc7cbbd`,
`transistors()` `0x6fbd1719`, `nextId()` `0x61b8ce8c`,
price `0x6817c76c`, minted `0x4f02c420`, cap `0x8f770ad0`).

## RPC endpoint notes

- **Primary: `https://rpc.xlayer.tech`** — works from this VM, no key,
  batched `eth_call` up to 10 per request.
- **`https://tapeout.net/rpc-xlayer` returns an HTML bot-challenge
  page** — do not rely on it. The indexer uses only `rpc.xlayer.tech`
  by default (`--rpc` overrides it).
- A full sync takes ~90–100 seconds at 430+ processors (sequential
  batches of 10). The default 5-minute interval leaves ample headroom.
- The server starts listening immediately and serves the last saved
  snapshot while the first sync runs in the background.

## How to run

```sh
go run .                                   # sync every 5m, serve :8080
go run . --port 9000 --interval 10m        # custom port / interval
go run . --block 0x4694380 --once          # reproduce the audit block
go run . --rpc https://rpc.xlayer.tech     # explicit RPC endpoint
go run . --data /var/lib/tapeout.json      # snapshot file location
```

Flags: `--rpc` (default `https://rpc.xlayer.tech`), `--block`
(default `latest`), `--interval` (default `5m`), `--port` (default
`8080`), `--data` (default `tapeout-indexer.json`), `--once` (sync
once, print a summary, exit — no server).

State is kept in memory and persisted as a JSON snapshot after every
successful sync, so restarts keep history and alerts. No database
dependency.

## API

| Endpoint | Description |
|---|---|
| `GET /` | Dashboard: stats, alerts, top 10, leaderboard (auto-refreshes every 60 s) |
| `GET /api/stats` | Processors, total circuits, zero-circuit count, median, top-3 share, top holders, last sync time/block |
| `GET /api/processors?limit=&offset=` | Leaderboard, circuits desc, paginated (default 50, max 200) |
| `GET /api/processors/{address}` | One processor with transistor price/minted/cap and circuit history |
| `GET /api/alerts?since=&limit=` | New processors / new circuits. `since` is RFC3339, or `last` for the latest sync only |
| `GET /healthz` | Liveness: status, processor count, last sync, block number |

Alerts: a sync reports processors that appeared since the previous
sync and processors whose circuit count grew. The very first sync
(bootstrap) generates no alerts — there is no previous sync to
compare against.

## Files

| File | What it does |
|---|---|
| `main.go` | Flags, startup, HTTP server, graceful shutdown |
| `rpc.go` | JSON-RPC client: batched `eth_call`, retries, single-call fallback, `eth_blockNumber`, hex parsing |
| `syncer.go` | Factory enumeration (`cpuCount` → `cpuAt` → `nextId` → `transistors` → price/minted/cap) |
| `state.go` | In-memory state, leaderboard sorting, stats, alerts, JSON snapshot save/load |
| `api.go` | REST API handlers |
| `dashboard.go` | Server-rendered dashboard page (no JS, no external assets) |
| `rpc_test.go`, `state_test.go` | Tests: RPC parsing, batch chunking, single-call fallback, sorting, stats, alerts, snapshot round-trip |

## Build notes

- `gofmt` / `go vet` / `go build` / `go test ./...` all green
  (Go 1.27.4, module `go 1.26.0`, zero dependencies).
- Two real bugs were caught by live verification and fixed:
  1. `eth_blockNumber`: `rpc.xlayer.tech` answers a single call with
     a **one-element JSON array**, not an object — the client now
     accepts both forms.
  2. **Cancelled syncs must not merge partial data.** A SIGTERM
     mid-sync once let the batch client return empty results with nil
     errors for calls that never ran, and the sync merged 1,661
     circuits as if complete. The client now marks every unattempted
     call with the cancellation error, and the syncer aborts (no
     merge, no snapshot save) when the context is cancelled or more
     than 5% of `nextId` reads fail. Covered by
     `TestCallBatchLenientCancelled`.

## Hackathon submission checklist (Pla's steps — do NOT submit from here)

- [ ] Project description: public TapeOut-on-X-Layer indexer —
      leaderboard, circuits, transistor price/minted/cap, new-circuit
      alerts, REST API + dashboard. Uses TapeOut circuits, contracts
      and on-chain data on X Layer; the Go service itself runs
      off-circuit (explicitly allowed by the FAQ).
- [ ] Factory address: `0x1f09daefa827f02cbb40967cc91b259763760761`
      (plus the top processor addresses from the leaderboard).
- [ ] Access link: the running dashboard URL (`/`), plus the API
      (`/api/stats`, `/api/processors`, `/api/alerts`). Deployment is
      prepared in `DEPLOY.md` (Render free tier recommended; `$PORT`
      support + `Dockerfile` + `fly.toml` + `railway.json` +
      `render.yaml` + systemd unit; `gofmt`/`go vet`/`go build`/`go
      test` verified green with Go 1.27.4 on this VM 2026-11-19 —
      Docker image build NOT tested, no Docker on this VM).
- [ ] Demo video (≤ 3 min recommended): problem, core features,
      target users and business model, how it uses TapeOut. Script in
      `DEMO_SCRIPT.md` — record against the live URL, not the VM.
- [ ] Submit through the official form before 2026-12-07 23:59 UTC
      (provisional — confirm on the official page).
- [ ] Self-custodial wallet for prize receipt; winners may need
      identity checks. Never share private keys or seed phrases.

## Sources

- Hackathon: https://web3.okx.com/xlayer/build-x-series
- Audit: https://openwitness.net/p/9897 (2026-10-28, block 74,007,424)
- Feasibility spike this build is based on: `~/workspace/tapeout-spike/`
