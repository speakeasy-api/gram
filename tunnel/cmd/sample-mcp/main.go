// Command sample-mcp is a trivial MCP-ish HTTP server used to prove the tunnel
// end to end locally. It answers JSON-RPC initialize / tools/list / tools/call
// (an "echo" tool) over Streamable HTTP POST, and exposes /healthz. It is NOT a
// real MCP server — just enough surface to verify request/response and SSE flow
// through the tunnel.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResp struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := os.Getenv("SAMPLE_MCP_ADDR")
	if addr == "" {
		addr = ":9000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	// SSE demo endpoint to prove streaming survives the tunnel.
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		for i := range 3 {
			fmt.Fprintf(w, "data: tick %d\n\n", i)
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"server": "sample-mcp", "status": "ok"})
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var req rpcReq
		if err := json.Unmarshal(body, &req); err != nil {
			writeRPC(w, rpcResp{JSONRPC: "2.0", ID: nil, Error: &rpcError{Code: -32700, Message: "parse error"}})
			return
		}
		var id any
		_ = json.Unmarshal(req.ID, &id)

		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		switch req.Method {
		case "initialize":
			writeRPC(w, rpcResp{JSONRPC: "2.0", ID: id, Result: map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]string{"name": "sample-mcp", "version": "0.1.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}})
		case "tools/list":
			writeRPC(w, rpcResp{JSONRPC: "2.0", ID: id, Result: map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echoes its input back. Proves the tunnel round-trips.",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
						"text":     map[string]any{"type": "string"},
						"sleep_ms": map[string]any{"type": "number", "minimum": 0, "maximum": 10000},
					}},
				}},
			}})
		case "tools/call":
			if delay := requestedSleep(req.Params); delay > 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(delay):
				}
			}
			writeRPC(w, rpcResp{JSONRPC: "2.0", ID: id, Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo via tunnel: " + string(req.Params)}},
			}})
		default:
			writeRPC(w, rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
		}
	})

	logger.Info("sample-mcp listening", slog.String("addr", addr))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("sample-mcp error", slog.Any("error", err))
		os.Exit(1)
	}
}

func requestedSleep(params json.RawMessage) time.Duration {
	var payload struct {
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return 0
	}
	raw, ok := payload.Arguments["sleep_ms"]
	if !ok {
		return 0
	}
	millis, ok := raw.(float64)
	if !ok || millis <= 0 {
		return 0
	}
	if millis > 10000 {
		millis = 10000
	}
	return time.Duration(millis) * time.Millisecond
}

func writeRPC(w http.ResponseWriter, resp rpcResp) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
