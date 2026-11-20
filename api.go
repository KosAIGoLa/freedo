// api.go: the REST API served by the indexer binary.
//
//	GET /api/stats                    aggregate numbers + sync status
//	GET /api/processors?limit&offset  leaderboard, circuits desc, paginated
//	GET /api/processors/{address}     one processor, with history
//	GET /api/alerts?since&limit       new processors / new circuits
//	GET /healthz                      liveness
package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseLimitOffset(r *http.Request) (limit, offset int) {
	limit = 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	offset = 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// handleStats serves GET /api/stats.
func handleStats(st *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, st.Stats())
	}
}

// processorListItem is the leaderboard row (no history — too big).
type processorListItem struct {
	Address    string    `json:"address"`
	Index      int       `json:"index"`
	Circuits   string    `json:"circuits"`
	Transistor string    `json:"transistor,omitempty"`
	Price      string    `json:"price,omitempty"`
	Minted     string    `json:"minted,omitempty"`
	Cap        string    `json:"cap,omitempty"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

type processorListResponse struct {
	Total      int                 `json:"total"`
	Limit      int                 `json:"limit"`
	Offset     int                 `json:"offset"`
	Processors []processorListItem `json:"processors"`
}

// handleProcessors serves GET /api/processors (list, paginated).
func handleProcessors(st *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := parseLimitOffset(r)
		all := st.Leaderboard()
		total := len(all)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		items := make([]processorListItem, 0, end-offset)
		for _, p := range all[offset:end] {
			items = append(items, processorListItem{
				Address: p.Address, Index: p.Index, Circuits: p.Circuits,
				Transistor: p.Transistor, Price: p.Price, Minted: p.Minted,
				Cap: p.Cap, FirstSeen: p.FirstSeen, LastSeen: p.LastSeen,
			})
		}
		writeJSON(w, http.StatusOK, processorListResponse{
			Total: total, Limit: limit, Offset: offset, Processors: items,
		})
	}
}

// handleProcessor serves GET /api/processors/{address}.
func handleProcessor(st *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.PathValue("addr"))
		if addr == "" {
			writeError(w, http.StatusBadRequest, "missing processor address")
			return
		}
		p, ok := st.GetProcessor(addr)
		if !ok {
			writeError(w, http.StatusNotFound, "processor not found: "+addr)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

type alertsResponse struct {
	Total  int     `json:"total"`
	Alerts []Alert `json:"alerts"`
}

// handleAlerts serves GET /api/alerts.
// ?since=RFC3339 filters to alerts at or after that time.
// ?since=last returns only the alerts from the latest sync.
func handleAlerts(st *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := parseLimitOffset(r)
		sinceRaw := strings.TrimSpace(r.URL.Query().Get("since"))
		if sinceRaw == "last" {
			alerts := st.AlertsForSync()
			if len(alerts) > limit {
				alerts = alerts[:limit]
			}
			writeJSON(w, http.StatusOK, alertsResponse{Total: len(alerts), Alerts: alerts})
			return
		}
		var since time.Time
		if sinceRaw != "" {
			t, err := time.Parse(time.RFC3339, sinceRaw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad since value, want RFC3339 or 'last'")
				return
			}
			since = t
		}
		alerts := st.Alerts(since, limit)
		writeJSON(w, http.StatusOK, alertsResponse{Total: len(alerts), Alerts: alerts})
	}
}

// handleHealthz serves GET /healthz.
func handleHealthz(st *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := st.Stats()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "ok",
			"processors":   s.Processors,
			"last_sync_at": s.LastSyncAt,
			"block_number": s.BlockNumber,
			"syncs":        s.Syncs,
			"sync_errors":  s.SyncErrors,
		})
	}
}

// NewHandler wires the API and dashboard routes.
func NewHandler(st *State) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", handleStats(st))
	mux.HandleFunc("GET /api/processors", handleProcessors(st))
	mux.HandleFunc("GET /api/processors/{addr}", handleProcessor(st))
	mux.HandleFunc("GET /api/alerts", handleAlerts(st))
	mux.HandleFunc("GET /healthz", handleHealthz(st))
	mux.HandleFunc("GET /", handleDashboard(st))
	return mux
}
