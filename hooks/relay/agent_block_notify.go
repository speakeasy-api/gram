package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/wire"
)

// agentBlockNotifyTimeout bounds the whole best-effort notify. It runs only
// after a deny — the user's call is already blocked and already paid a WAN
// round trip, so 300ms is imperceptible — and only a *hung* daemon ever pays
// it: a missing socket fails the dial instantly (same reasoning as
// socketAgentTimeout).
const agentBlockNotifyTimeout = 300 * time.Millisecond

const agentBlockReportPath = "/v1/blocks/report"

// agentBlockReport is the hook→daemon contract for POST /v1/blocks/report
// (the device agent's api.BlockReportRequest, BlocksContractVersion 1 — keep
// in sync). The daemon answers 202 when recorded; 404 means it predates the
// contract; every response is ignored either way.
type agentBlockReport struct {
	V                int    `json:"v"`
	Category         string `json:"category"`
	Requestable      bool   `json:"requestable"`
	RequestToken     string `json:"request_token"`
	RequestURL       string `json:"request_url,omitempty"`
	RequestExpiresAt string `json:"request_expires_at,omitempty"`
	ServerName       string `json:"server_name,omitempty"`
	ServerURL        string `json:"server_url,omitempty"`
	PolicyName       string `json:"policy_name,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	BlockURL         string `json:"block_url,omitempty"`
	Provider         string `json:"provider,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	BlockedAt        string `json:"blocked_at"`
}

// notifyAgentBlock best-effort forwards a requestable deny to the device
// agent's socket so it can surface a native "request access" notification.
// Synchronous on purpose: hook processes are one-shot, so a detached goroutine
// would die at process exit before the write lands. Strictly off the allow
// path, never a new failure mode — all errors are swallowed, and machines
// without the agent fail the dial instantly. Spooled/drained replays never
// reach the gating handlers that call this, so a stale deny cannot re-notify.
func (r *Relay) notifyAgentBlock(ctx context.Context, effect *wire.BlockEffect, typed any) {
	if effect == nil || !effect.Requestable || effect.V > wire.BlockEffectVersion || strings.TrimSpace(effect.RequestToken) == "" {
		return
	}
	socket := agentSocketPath()
	if socket == "" {
		return
	}
	base := agenthooks.EventOf(typed)
	report := agentBlockReport{
		V:                1,
		Category:         effect.Category,
		Requestable:      true,
		RequestToken:     effect.RequestToken,
		RequestURL:       effect.RequestURL,
		RequestExpiresAt: effect.RequestExpiresAt,
		ServerName:       effect.ServerName,
		ServerURL:        effect.ServerURL,
		PolicyName:       effect.PolicyName,
		ToolName:         effect.ToolName,
		BlockURL:         effect.BlockURL,
		Provider:         string(base.Provider),
		SessionID:        base.Session.ID,
		BlockedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(report)
	if err != nil {
		return
	}
	// Detached from the event's context: the deny is already resolved, and a
	// gating budget exhausted by the server exchange must not starve the
	// notify of its own 300ms.
	requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentBlockNotifyTimeout)
	defer cancel()
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
				return dialAgentSocket(dialCtx, socket)
			},
			// One-shot client, same as socketIdentityEmail: without this the
			// transport pools the socket connection for the process lifetime.
			DisableKeepAlives: true,
		},
		// One attempt means one request: a redirect answer is terminal, not a
		// license to re-send the report at another path.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://speakeasy-agent"+agentBlockReportPath, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		r.debugf("agent block notify skipped: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusAccepted {
		r.debugf("agent block notify status=%d", resp.StatusCode)
	}
}

// decodeBlockEffect reads the response's "block" effect into its wire type,
// asserting fields straight off the decoded map like the org_settings /
// skill_capture readers in client.go (the SDK has already decoded Effects
// into map[string]any, so an unmarshal into the struct would need a marshal
// round-trip). The keys are pinned to wire.BlockEffect's JSON tags by
// TestDecodeBlockEffectMatchesWireContract. Returns nil for anything that
// isn't the object shape this version understands: a non-object, or any
// present field of the wrong type (including a fractional "v") — a garbled
// effect suppresses the notify entirely rather than posting a degraded
// report, matching what an unmarshal into the struct would do. Absent fields
// are fine; they decode to zero values the consumer's guards treat as absent.
func decodeBlockEffect(raw any) *wire.BlockEffect {
	object, isObject := raw.(map[string]any)
	if !isObject {
		return nil
	}
	valid := true
	intField := func(key string) int {
		value, present := object[key]
		if !present {
			return 0
		}
		f, ok := value.(float64) // JSON numbers decode as float64
		if !ok || f != math.Trunc(f) {
			valid = false
			return 0
		}
		return int(f)
	}
	boolField := func(key string) bool {
		value, present := object[key]
		if !present {
			return false
		}
		b, ok := value.(bool)
		if !ok {
			valid = false
			return false
		}
		return b
	}
	strField := func(key string) string {
		value, present := object[key]
		if !present {
			return ""
		}
		s, ok := value.(string)
		if !ok {
			valid = false
			return ""
		}
		return s
	}
	effect := wire.BlockEffect{
		V:                intField("v"),
		Category:         strField("category"),
		Requestable:      boolField("requestable"),
		RequestToken:     strField("request_token"),
		RequestURL:       strField("request_url"),
		RequestExpiresAt: strField("request_expires_at"),
		ServerName:       strField("server_name"),
		ServerURL:        strField("server_url"),
		PolicyName:       strField("policy_name"),
		ToolName:         strField("tool_name"),
		BlockURL:         strField("block_url"),
	}
	if !valid {
		return nil
	}
	return &effect
}
