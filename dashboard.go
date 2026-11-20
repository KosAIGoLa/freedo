// dashboard.go: the single-page dashboard served at /.
//
// Server-rendered HTML, no JavaScript, no external assets; the page
// refreshes itself every 60 seconds.
package main

import (
	"fmt"
	"html/template"
	"net/http"
	"time"
)

const dashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="60">
<title>TapeOut on X Layer — Public Indexer</title>
<style>
  * { box-sizing: border-box; }
  body { font-family: -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
         max-width: 1080px; margin: 0 auto; padding: 24px 16px 48px; color: #1a1a2e;
         background: #f7f7fa; line-height: 1.5; }
  h1 { font-size: 1.6rem; margin: 0 0 4px; }
  h2 { font-size: 1.15rem; margin: 28px 0 10px; border-bottom: 2px solid #e0e0ea; padding-bottom: 4px; }
  p.sub { color: #555; margin: 0 0 20px; }
  .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; }
  .card { background: #fff; border: 1px solid #e0e0ea; border-radius: 10px; padding: 14px 16px; }
  .card .label { font-size: .8rem; color: #777; text-transform: uppercase; letter-spacing: .04em; }
  .card .value { font-size: 1.5rem; font-weight: 700; margin-top: 2px; }
  .card .note { font-size: .8rem; color: #888; }
  table { width: 100%; border-collapse: collapse; background: #fff; font-size: .9rem; }
  th, td { padding: 8px 10px; border-bottom: 1px solid #eee; text-align: left; }
  th { background: #f0f0f5; font-weight: 600; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  code { background: #eee; padding: 1px 5px; border-radius: 4px; font-size: .85em;
         word-break: break-all; }
  .mono { font-family: ui-monospace, "SF Mono", Menlo, Consolas, monospace; font-size: .85rem; word-break: break-all; }
  .alert { background: #fff; border-left: 4px solid #2a9d8f; padding: 8px 12px;
          margin: 6px 0; border-radius: 0 6px 6px 0; }
  .alert.new_processor { border-left-color: #4361ee; }
  .muted { color: #888; font-size: .85rem; }
  footer { margin-top: 32px; color: #777; font-size: .85rem; }
  a { color: #4361ee; }
</style>
</head>
<body>
<h1>TapeOut on X Layer — Public Indexer</h1>
<p class="sub">Live leaderboard of every processor deployed by the TapeOut factory on X Layer.
Read-only: this service only makes <code>eth_call</code> requests — no wallet, no signing, no funds.</p>

<div class="cards">
  <div class="card"><div class="label">Processors</div><div class="value">{{.Stats.Processors}}</div></div>
  <div class="card"><div class="label">Circuits fabricated</div><div class="value">{{.Stats.TotalCircuits}}</div></div>
  <div class="card"><div class="label">Zero-circuit processors</div><div class="value">{{.Stats.ZeroCircuits}}</div></div>
  <div class="card"><div class="label">Median circuits</div><div class="value">{{.Stats.MedianCircuits}}</div></div>
  <div class="card"><div class="label">Top 3 share</div><div class="value">{{printf "%.1f" .Stats.Top3SharePct}}%</div></div>
  <div class="card"><div class="label">Last sync</div><div class="value" style="font-size:1rem">{{.LastSync}}</div><div class="note">block {{.Stats.BlockNumber}} · sync #{{.Stats.Syncs}}{{if .Stats.LastSyncErr}} · <span style="color:#c0392b">error: {{.Stats.LastSyncErr}}</span>{{end}}</div></div>
</div>

<h2>Recent alerts</h2>
{{if .Alerts}}
  {{range .Alerts}}
  <div class="alert {{.Kind}}"><strong>{{if eq .Kind "new_processor"}}New processor{{else}}New circuits{{end}}</strong>
    <span class="mono">{{.Address}}</span> — {{.Detail}}
    <span class="muted">{{.Time.Format "2006-01-02 15:04:05 MST"}} · block {{.Block}}</span></div>
  {{end}}
{{else}}
  <p class="muted">No new processors or circuits since the last sync.</p>
{{end}}

<h2>Top 10 by circuits</h2>
<table>
  <tr><th>#</th><th>Processor</th><th class="num">Circuits</th><th>Transistor</th><th class="num">Price</th><th class="num">Minted</th><th class="num">Cap</th></tr>
  {{range $i, $p := .Top10}}
  <tr><td>{{add $i 1}}</td><td class="mono">{{$p.Address}}</td><td class="num"><strong>{{$p.Circuits}}</strong></td>
      <td class="mono">{{or $p.Transistor "—"}}</td><td class="num">{{or $p.Price "—"}}</td>
      <td class="num">{{or $p.Minted "—"}}</td><td class="num">{{or $p.Cap "—"}}</td></tr>
  {{end}}
</table>

<h2>Leaderboard — top 50</h2>
<table>
  <tr><th>#</th><th>Processor</th><th class="num">Circuits</th><th>First seen</th></tr>
  {{range $i, $p := .Leaderboard}}
  <tr><td>{{add $i 1}}</td><td class="mono">{{$p.Address}}</td><td class="num">{{$p.Circuits}}</td>
      <td class="muted">{{$p.FirstSeen.Format "2006-01-02 15:04"}}</td></tr>
  {{end}}
</table>

<footer>
  <p>Factory: <code>{{.Stats.Factory}}</code> · RPC: <code>{{.RPC}}</code> · Block tag: <code>{{.Stats.Block}}</code></p>
  <p>Audit reference (openwitness, 2026-10-28, block 74,007,424): 389 processors, 11,006 circuits, 111 zero-circuit, top 3 by circuits 6,424 / 1,300 / 1,022.</p>
  <p>API: <a href="/api/stats">/api/stats</a> · <a href="/api/processors">/api/processors</a> · <a href="/api/alerts">/api/alerts</a> · <a href="/healthz">/healthz</a></p>
  <p>Built for X Layer TapeOut Accelerate 2026. Source: TapeOut-on-X-Layer indexer (Go, stdlib only).</p>
</footer>
</body>
</html>`

type dashboardData struct {
	Stats       Stats
	Top10       []*Processor
	Leaderboard []*Processor
	Alerts      []Alert
	LastSync    string
	RPC         string
}

var dashboardTmpl = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"or": func(s, fallback string) string {
		if s == "" {
			return fallback
		}
		return s
	},
}).Parse(dashboardTemplate))

// handleDashboard serves GET /.
func handleDashboard(st *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		stats := st.Stats()
		all := st.Leaderboard()
		top10 := all
		if len(top10) > 10 {
			top10 = top10[:10]
		}
		top50 := all
		if len(top50) > 50 {
			top50 = top50[:50]
		}
		lastSync := "never"
		if !stats.LastSyncAt.IsZero() {
			lastSync = stats.LastSyncAt.Format("15:04:05")
		}
		data := dashboardData{
			Stats: stats, Top10: top10, Leaderboard: top50,
			Alerts: st.Alerts(time.Time{}, 20), LastSync: lastSync,
			RPC: rpcURLForDashboard,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboardTmpl.Execute(w, data); err != nil {
			fmt.Fprintf(w, "dashboard render error: %v", err)
		}
	}
}

// rpcURLForDashboard is set by main so the footer shows the active RPC.
var rpcURLForDashboard string
