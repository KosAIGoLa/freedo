# Deploying the TapeOut indexer — get a public access link for submission

Deadline: **2026-12-07 23:59 UTC** (provisional — confirm on the official page).
The submission needs a public URL for the dashboard (`/`) and API
(`/api/stats`, `/api/processors`, `/api/alerts`). The binary has only
ever run on this VM, so the first deploy is also the verification step.

## LIVE — Railway (deployed 2026-11-20, Taipei)

- **URL: https://freedo-production.up.railway.app** — dashboard `/`,
  `/api/stats`, `/healthz` all verified live 2026-11-20 08:59 CST:
  435 processors, 11,991 circuits, block 76,367,212, 0 sync errors.
- Project `calm-charm`, service `freedo`, from `KosAIGoLa/freedo`
  main (Dockerfile). Free plan trial: "30 days or $5.00 left" —
  after the trial the $1/month Free credit will not cover 24/7
  running, so the link may stop working after ~2026-12-20 unless
  upgraded (Hobby $5/month, Pla's call).
- Volume `freedo-volume` mounted at `/data` keeps the snapshot
  across restarts.
- **Railway rejects `VOLUME` in Dockerfiles** — the `VOLUME /data`
  line was removed (commit bdd45888); the Railway Volume replaces it.
- **Auto-deploy is unavailable** on this service: pushes to main do
  NOT deploy automatically — in the Railway dashboard use
  "Check for updates" → "Update" to pull a new commit.

## What is prepared (2026-11-19, Taipei)

- **`Dockerfile`** (multi-stage, `golang:1.27-alpine` → `scratch`,
  static binary, non-root user `65532:65532`, CA bundle copied in so
  the `https://rpc.xlayer.tech` calls verify, `EXPOSE 8080`,
  snapshot at `/data/tapeout-indexer.json`) + **`.dockerignore`**.
- **`fly.toml`** (Fly.io, region `sin`, 1 GB volume `tapeout_data`),
  **`railway.json`** (Railway, Dockerfile builder, `/healthz`
  healthcheck), **`render.yaml`** (Render Blueprint, free plan,
  Docker runtime).
- **`tapeout-indexer.service`** (systemd unit for a plain VPS).
- **`main.go` honours `$PORT`** (Render, Railway and others assign a
  dynamic port; an explicit `--port` wins, otherwise a valid `$PORT`
  is used; an invalid `$PORT` is logged and ignored).
- **Verified on this VM with Go 1.27.4** (2026-11-19):
  `gofmt` / `go vet` / `go build` / `go test ./...` all green, and
  the server was run locally with real live data
  (`/healthz`, `/api/stats`: 430 processors, 11,963 circuits).
- **NOT verified here:** the Docker image was **not** built on this
  VM (no Docker here). The Fly.io / Render configs were validated
  for syntax only (TOML/YAML parse) — no account was created and
  nothing was deployed from here. **Railway is now deployed and
  verified live** (see LIVE above) — the Dockerfile builds fine on
  Railway once the `VOLUME` line is removed.

## What happens without a persistent volume

The state is a JSON snapshot. Without a volume it is lost on every
restart/redeploy; the server still starts immediately, serves the
snapshot only after a sync, and the first full sync rebuilds state
from chain in ~90 s (430+ processors, sequential batches of 10).
History (`first_seen`, per-processor circuit history, alerts) restarts
from scratch. Nothing breaks — the leaderboard just refills.

## Option A — Render free tier ($0, no credit card) — BLOCKED in practice

**2026-11-20 live test: new Render accounts are asked for a credit
card at both the Blueprint and the direct Web Service deploy — the
"no card required" claim does not hold in practice. Render is out
unless a card is added (Pla declined).** The notes below are kept
for reference only.

Free tier (Render docs, re-checked 2026-11-19): Hobby workspace $0,
web service 512 MB RAM, 750 free instance-hours/month per workspace,
no card required, Go/Docker supported.

```sh
# 1. Push this folder to a GitHub repo (it is not a git repo yet)
git init && git add -A && git commit -m "TapeOut indexer" && git push

# 2. Render dashboard → New → Web Service → connect the repo →
#    Runtime: Docker (or New → Blueprint → connect repo, it picks up
#    render.yaml). Leave the plan on Free. Deploy.
#    First URL looks like https://tapeout-indexer.onrender.com
```

Verify: open `/` and `/api/stats` — expect ~430 processors and
~11,963 circuits (both grow; re-check `/api/stats` on the day).

Honest caveats (these matter for the judges' first click):
- **Sleeps after 15 min with no traffic; ~1 min to wake.** A judge's
  first visit sees a Render loading page for about a minute.
- **No persistent disk on free.** The snapshot is lost on every
  spin-down/redeploy, so after a wake the dashboard starts empty and
  the first full sync takes ~90 s before the leaderboard fills in.
- 750 free hours/month is shared across the workspace; one service
  fits, but only because sleeping pauses the clock.
- If the cold start is unacceptable, the paid fallback is Render
  Starter at **$7/month** (0.5 CPU / 512 MB, never sleeps) — a paid
  step, so it is Pla's call, not something to enable silently.

## Option B — Fly.io (~$2/month after trial; always awake)

Fly.io has **no permanent free tier** for new accounts (retired
Oct 2024; verified against multiple Nov 2026 sources). New accounts
get a short trial — 2 VM-hours or 7 days, whichever first — then the
smallest machine (shared-cpu-1x, 256 MB) is ~$2/month
(~$2.79 in Singapore; verify at fly.io/docs/about/pricing). Volumes
are $0.15/GB/month. A card is required after the trial.

```sh
# 1. Install flyctl and sign up / log in (account + card are Pla's)
curl -L https://fly.io/install.sh | sh
fly auth signup        # or: fly auth login

# 2. Create the 1 GB volume the fly.toml expects (region sin = Singapore)
fly volumes create tapeout_data --size 1 --region sin

# 3. Deploy from this folder (fly.toml is picked up automatically)
fly deploy

# 4. Open the URL Fly prints, e.g. https://tapeout-indexer.fly.dev
fly open
```

Verify: `/healthz` returns `{"status":"ok",...}` and `/api/stats`
shows ~430 processors / ~11,963 circuits. The volume keeps the
snapshot across restarts; the machine never sleeps, so the first
click is instant. Cost note: ~$2/month for the machine + ~$0.15/month
for the 1 GB volume is Pla's spend — do not deploy without agreeing
to it.

## Option C — Railway (Free $1 credit/month; realistic floor Hobby $5/month)

Railway pricing (Nov 2026 sources): Free plan $0/month with $1 of
usage credit per month (after a 30-day trial with a one-time $5
credit), 1 vCPU / 0.5 GB RAM, 0.5 GB volume. Hobby is a $5/month
minimum that includes $5 of usage. Rates are ~$20/vCPU/month,
~$10/GB RAM/month, $0.15/GB volume/month, $0.05/GB egress. A tiny
always-on Go service uses little CPU but its allocated memory is
metered, so the $1 Free credit is unlikely to cover a full month of
24/7 running — Hobby $5/month is the realistic floor (Pla's spend).

```sh
# 1. Install the Railway CLI and log in (account is Pla's)
npm i -g @railway/cli
railway login

# 2. From this folder: create a project, link it, deploy.
#    railway.json selects the Dockerfile builder and the /healthz check.
railway init
railway up

# 3. Attach a volume in the Railway dashboard (service → Settings →
#    Volumes → mount path /data, 1 GB). Without it the snapshot is
#    lost on redeploy and the first sync rebuilds it in ~90 s.
# 4. Generate a public domain in the dashboard (service → Settings →
#    Networking → Generate Domain) and open it.
```

Verify: `/healthz` and `/api/stats` as above. Railway sets `$PORT`
and the binary honours it; no env vars are needed.

## Option D — Pla's own Linux machine (Go 1.27.3 already installed)

No sleep, snapshot persists on disk, sync every 5 min. **Unresolved:**
whether that machine has a public IP / domain the judges can reach.
If it is behind NAT with no public address, this option gives no
access link — check before choosing it.

```sh
# 1. Build and install
go build -o tapeout-indexer .
sudo useradd -r -s /usr/sbin/nologin tapeout
sudo mkdir -p /opt/tapeout-indexer /var/lib/tapeout-indexer
sudo cp tapeout-indexer /opt/tapeout-indexer/
sudo chown -R tapeout:tapeout /opt/tapeout-indexer /var/lib/tapeout-indexer
sudo cp tapeout-indexer.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now tapeout-indexer

# 2. Check it (from the machine, or through the reverse proxy)
curl localhost:8080/healthz
curl localhost:8080/api/stats | head -c 400

# 3. Public access: point a reverse proxy at :8080 (nginx/Caddy) with
#    TLS (e.g. Caddy automatic HTTPS, or certbot for nginx) and give
#    that URL as the access link.
```

The unit file (`tapeout-indexer.service`) runs the binary as the
`tapeout` user with `--data
/var/lib/tapeout-indexer/tapeout-indexer.json --port 8080`,
restarts on failure, and logs to the journal
(`journalctl -u tapeout-indexer -f`).

## After the deploy works

- Put the live URL in the submission form as the access link, with
  `/api/stats` as the API proof point.
- Record the demo video against the live URL (script:
  `docs/DEMO_SCRIPT.md`; finished video: `docs/demo-video.mp4`).
- Keep the VM copy only as a backup; the submitted link should be
  the deployed one.
- Numbers move: processors were 430 and circuits 11,963 at
  2026-11-19 ~14:50 Taipei (block 76,301,697). Re-check `/api/stats`
  before submitting or re-recording.
