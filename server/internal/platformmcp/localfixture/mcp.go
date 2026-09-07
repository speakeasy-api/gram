package localfixture

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	fixtureMCPSessionHeader   = "Mcp-Session-Id"
	fixtureMCPSessionLifetime = 15 * time.Minute
	maxFixtureMCPSessions     = 128
)

type MCPHTTP struct {
	oauth    *OAuthHTTP
	sessions map[string]time.Time
	mu       sync.Mutex
}

func NewMCPHTTP(oauth *OAuthHTTP) *MCPHTTP {
	return &MCPHTTP{
		oauth:    oauth,
		sessions: make(map[string]time.Time),
		mu:       sync.Mutex{},
	}
}

func (s *MCPHTTP) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.oauth == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			w.Header().Set("Allow", http.MethodPost+", "+http.MethodDelete)
			http.Error(w, "streamable HTTP fixture only supports POST and DELETE", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") == "" || !s.oauth.HasLiveAccessToken(bearerToken(r.Header.Get("Authorization"))) {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.oauth.config.OAuthAuthorizationServerMetadataURL()+`"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if r.Method == http.MethodDelete {
			s.handleDelete(w, r)
			return
		}
		s.handlePost(w, r)
	})
}

func (s *MCPHTTP) handlePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, oauthRequestMaxBytes)
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.JSONRPC != "2.0" {
		writeFixtureMCPError(w, request.ID, -32600, "invalid request")
		return
	}

	sessionID := r.Header.Get(fixtureMCPSessionHeader)
	if request.Method == "notifications/initialized" && len(request.ID) == 0 && s.hasSession(sessionID) {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(request.ID) == 0 {
		writeFixtureMCPError(w, request.ID, -32600, "request id is required")
		return
	}
	switch request.Method {
	case "initialize":
		if sessionID != "" {
			writeFixtureMCPError(w, request.ID, -32600, "initialize must not include a session")
			return
		}
		var err error
		sessionID, err = fixtureSessionID()
		if err != nil {
			writeFixtureMCPError(w, request.ID, -32603, "could not create session")
			return
		}
		if !s.createSession(sessionID) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many active sessions", http.StatusTooManyRequests)
			return
		}
		w.Header().Set(fixtureMCPSessionHeader, sessionID)
		writeFixtureMCPResult(w, request.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": CanonicalRef, "version": fixtureVersion},
		})
	case "tools/list":
		if !s.hasSession(sessionID) {
			writeFixtureMCPError(w, request.ID, -32600, "unknown session")
			return
		}
		writeFixtureMCPResult(w, request.ID, map[string]any{"tools": []map[string]any{{
			"name":        fixtureToolName,
			"description": fixtureToolSummary,
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"annotations": map[string]any{"readOnlyHint": true},
		}}})
	case "tools/call":
		if !s.hasSession(sessionID) {
			writeFixtureMCPError(w, request.ID, -32600, "unknown session")
			return
		}
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil || params.Name != fixtureToolName {
			writeFixtureMCPError(w, request.ID, -32602, "unknown tool")
			return
		}
		writeFixtureMCPResult(w, request.ID, map[string]any{
			"content": []map[string]string{{
				"type": "text",
				"text": "local fixture status: ready",
			}},
		})
	default:
		writeFixtureMCPError(w, request.ID, -32601, "method not found")
	}
}

func (s *MCPHTTP) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(fixtureMCPSessionHeader)
	if sessionID == "" {
		http.Error(w, "missing session", http.StatusBadRequest)
		return
	}
	s.deleteSession(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *MCPHTTP) createSession(sessionID string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessions(now)
	if len(s.sessions) >= maxFixtureMCPSessions {
		return false
	}
	s.sessions[sessionID] = now
	return true
}

func (s *MCPHTTP) deleteSession(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessions(time.Now())
	delete(s.sessions, sessionID)
}

func (s *MCPHTTP) hasSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessions(time.Now())
	_, ok := s.sessions[sessionID]
	return ok
}

func (s *MCPHTTP) pruneSessions(now time.Time) {
	for sessionID, createdAt := range s.sessions {
		if !createdAt.Add(fixtureMCPSessionLifetime).After(now) {
			delete(s.sessions, sessionID)
		}
	}
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func fixtureSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate fixture MCP session ID: %w", err)
	}
	return "local_fixture_" + hex.EncodeToString(bytes), nil
}

func writeFixtureMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeFixtureMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
