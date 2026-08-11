package localfixture

import (
	"encoding/json"
	"net/http"
)

const (
	registryListPath    = "/v0.1/servers"
	fixtureVersion      = "1.0.0"
	fixtureToolName     = "local_fixture_status"
	fixtureToolSummary  = "Returns deterministic local fixture status."
	fixtureServerName   = "Local reviewed MCP fixture"
	fixtureDescription  = "Synthetic reviewed Streamable HTTP MCP for local Platform MCP validation."
	fixtureRegistryMeta = "com.pulsemcp/server-version"
)

type RegistryHTTP struct {
	config *Config
}

func NewRegistryHTTP(config *Config) *RegistryHTTP {
	return &RegistryHTTP{config: config}
}

func (s *RegistryHTTP) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.config == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == registryListPath:
			s.handleList(w, r)
		case r.Method == http.MethodGet && (r.URL.Path == decodedDetailsPath() || r.URL.EscapedPath() == s.config.RegistryDetailsPath()):
			s.handleDetails(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *RegistryHTTP) handleList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("version") != "latest" || query.Get("limit") != "50" || query.Get("cursor") != "" || len(query) != 2 {
		http.Error(w, "invalid registry list request", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"servers":  []any{s.entry()},
		"metadata": map[string]any{"nextCursor": nil},
	})
}

func (s *RegistryHTTP) handleDetails(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.entry())
}

func (s *RegistryHTTP) entry() map[string]any {
	remote := map[string]any{
		"url":     s.config.RemoteURL(),
		"type":    "streamable-http",
		"headers": []any{},
	}
	tool := map[string]any{
		"name":        fixtureToolName,
		"description": fixtureToolSummary,
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		"annotations": map[string]any{"readOnlyHint": true},
	}
	return map[string]any{
		"server": map[string]any{
			"name":        CanonicalRef,
			"title":       fixtureServerName,
			"description": fixtureDescription,
			"version":     fixtureVersion,
			"remotes":     []any{remote},
		},
		"_meta": map[string]any{
			fixtureRegistryMeta: map[string]any{
				"status":   "active",
				"isLatest": true,
				"remotes[0]": map[string]any{
					"tools": []any{tool},
				},
			},
		},
	}
}

func decodedDetailsPath() string {
	return "/v0.1/servers/" + CanonicalRef + "/versions/latest"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
