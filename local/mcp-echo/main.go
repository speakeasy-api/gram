// Tiny MCP "echo" upstream for manual Gram remote-MCP testing.
//
// It speaks just enough streamable-HTTP MCP (initialize, tools/list,
// tools/call) for Gram's remote proxy to talk to it, and it logs EVERY
// inbound request header so you can see exactly which headers Gram forwards
// upstream (e.g. an env-sourced X-Upstream-Api-Key).
//
// Run:   go run ./local/mcp-echo            (defaults to :9977)
//
//	PORT=9000 go run ./local/mcp-echo
//
// Then create a Remote MCP server in Gram pointing at http://localhost:9977
// (transport: streamable-http), add an env-sourced header, attach an
// environment, and watch this process's log.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9977"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &req)

		logRequest(r, req.Method, body)

		// Notifications (no id) and non-POST get an empty 202/200.
		if r.Method != http.MethodPost || len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mcp-echo", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "echo_headers",
				"description": "Returns pong; check the server log for the headers it received.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}}
		case "tools/call":
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "pong"}},
				"isError": false,
			}
		default:
			result = map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	})

	log.Printf("mcp-echo listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func logRequest(r *http.Request, method string, body []byte) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n--- %s  %s %s  (rpc method=%q)\n", time.Now().Format("15:04:05"), r.Method, r.URL.Path, method)

	names := make([]string, 0, len(r.Header))
	for name := range r.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  %s: %s\n", name, strings.Join(r.Header[name], ", "))
	}
	if len(body) > 0 && len(body) < 2000 {
		fmt.Fprintf(&b, "  body: %s\n", body)
	}
	log.Print(b.String())
}
