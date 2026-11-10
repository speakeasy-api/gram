package mcpfixture

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiStub answers the operations of the demo seed's "Acme Internal API"
// (server/internal/demoseed/openapi.yaml) with small fixed payloads, so the
// generated servers' tool calls return data instead of failing on a fake host.
// When a caller sends a bearer token, the stub echoes whose it is, which shows
// that a forwarded token arrived.
func apiStub() http.Handler {
	mux := http.NewServeMux()
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
	reply := func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if tok := bearer(r); tok != "" {
			body["caller"] = describeCaller(tok)
		}
		writeJSON(w, http.StatusOK, body)
	}
	param := func(r *http.Request, name, def string) string {
		if v := r.URL.Query().Get(name); v != "" {
			return v
		}
		return def
	}

	mux.HandleFunc("GET /logs/search", func(w http.ResponseWriter, r *http.Request) {
		svc := param(r, "service", "checkout")
		level := param(r, "level", "error")
		reply(w, r, map[string]any{"service": svc, "level": level, "lines": []map[string]string{
			{"at": now(), "level": level, "message": svc + ": payment provider timed out after 30s"},
			{"at": now(), "level": level, "message": svc + ": retry 2 of 3 succeeded"},
		}})
	})
	mux.HandleFunc("GET /metrics/get", func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, map[string]any{"service": param(r, "service", "checkout"), "p50_ms": 42, "p99_ms": 310, "error_rate": 0.012, "rps": 184})
	})
	mux.HandleFunc("GET /db/query", func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, map[string]any{"report": param(r, "report", "weekly_orders"), "rows": []map[string]any{
			{"week": "2026-W44", "orders": 1820, "revenue": 91250.40},
			{"week": "2026-W45", "orders": 1954, "revenue": 97010.15},
		}})
	})
	mux.HandleFunc("GET /customers/get", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		reply(w, r, map[string]any{"id": id, "name": "Jordan Example", "plan": "business", "status": "active", "open_tickets": 2})
	})
	mux.HandleFunc("GET /deploys/list", func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, map[string]any{"deploys": []map[string]string{
			{"service": "checkout", "version": "v2.31.0", "status": "succeeded", "at": now()},
			{"service": "search", "version": "v1.9.4", "status": "rolled_back", "at": now()},
		}})
	})
	mux.HandleFunc("POST /refunds/process", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			OrderID  string  `json:"orderId"`
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || in.OrderID == "" || in.Amount <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "orderId and a positive amount are required"})
			return
		}
		if in.Currency == "" {
			in.Currency = "USD"
		}
		reply(w, r, map[string]any{"refund_id": "rf_" + strings.ToLower(in.OrderID), "order_id": in.OrderID, "amount": in.Amount, "currency": in.Currency, "status": "processed"})
	})
	mux.HandleFunc("GET /traces/fetch", func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, map[string]any{"service": param(r, "service", "checkout"), "window": param(r, "window", "15m"), "traces": []map[string]any{
			{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "duration_ms": 812, "spans": 14, "error": false},
			{"trace_id": "00f067aa0ba902b7a3ce929d0e0e4736", "duration_ms": 30121, "spans": 9, "error": true},
		}})
	})
	mux.HandleFunc("GET /health/check", func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, map[string]any{"status": "ok", "checked_at": now(), "dependencies": map[string]string{"database": "ok", "queue": "ok", "payments": "degraded"}})
	})
	return mux
}

// describeCaller names the holder of a fixture token without verifying it
// against an issuer: the stub only reports what arrived.
func describeCaller(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "bearer token (not a fixture token)"
	}
	raw, err := decodeSegment(parts[1])
	if err != nil {
		return "bearer token (unreadable)"
	}
	var c struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Sub == "" {
		return "bearer token (no subject)"
	}
	return c.Sub
}
