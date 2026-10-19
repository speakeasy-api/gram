package assistants

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
)

// KitRuntimeProtocolConfig binds a protocol instance to a provisioned gateway.
// This is internal transport plumbing, not a new Platform MCP capability.
type KitRuntimeProtocolConfig struct {
	Core         *ServiceCore
	Endpoint     *url.URL
	HTTPClient   *http.Client
	Projector    *KitTranscriptProjector
	CWD          string
	GatewayToken string
}
type KitRuntimeProtocol struct{ config KitRuntimeProtocolConfig }

var _ runtimeProtocol = (*KitRuntimeProtocol)(nil)
var _ runtimeSwitchCoordinator = (*KitRuntimeProtocol)(nil)

var ErrKitUpgradeNoticeUnsupported = errors.New("Kit runtime-generated context upgrade notice is not supported by the pinned runtime")

func NewKitRuntimeProtocol(c KitRuntimeProtocolConfig) (*KitRuntimeProtocol, error) {
	if c.Core == nil || c.Projector == nil || c.Endpoint == nil || c.Endpoint.Path != "/acp/v2" || !filepath.IsAbs(c.CWD) {
		return nil, errors.New("incomplete Kit protocol configuration")
	}
	return &KitRuntimeProtocol{config: c}, nil
}

func kitStatusCapability(raw json.RawMessage) bool {
	var v struct {
		Capabilities struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	value, ok := v.Capabilities.Meta["kit/session/status"]
	var object map[string]json.RawMessage
	return ok && json.Unmarshal(value, &object) == nil && object != nil
}
func (p *KitRuntimeProtocol) dial(ctx context.Context, r assistantRuntimeRecord) (*kitacp.Client, error) {
	if r.RuntimeVersion != 3 {
		return nil, ErrKitRuntimeBinding
	}
	token := p.config.GatewayToken
	if token == "" {
		return nil, ErrKitGatewayTokenNotFound
	}
	client, init, err := kitacp.Dial(ctx, p.config.Endpoint.String(), token, p.config.HTTPClient, map[string]any{"protocolVersion": 2, "info": map[string]string{"name": "speakeasy", "version": "1"}, "capabilities": map[string]any{}})
	if err != nil {
		return nil, err
	}
	if !kitStatusCapability(init) {
		closeKitTransport(client)
		return nil, errors.New("Kit authoritative session status capability unavailable")
	}
	return client, nil
}
func closeKitTransport(c *kitacp.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.CloseTransport(ctx)
}
func kitSessionKey(r assistantRuntimeRecord, thread uuid.UUID) kitRuntimeSessionKey {
	return kitRuntimeSessionKey{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, ThreadID: thread}
}

func kitSettled(raw json.RawMessage, session string) (bool, error) {
	var s struct {
		SessionID        string  `json:"sessionId"`
		Settled          *bool   `json:"settled"`
		ForegroundActive *bool   `json:"foregroundActive"`
		BackgroundActive *bool   `json:"backgroundActive"`
		Pending          *bool   `json:"pendingBackgroundTerminals"`
		Subagents        *uint64 `json:"activeSubagents"`
	}
	if json.Unmarshal(raw, &s) != nil || s.SessionID != session || s.Settled == nil || s.ForegroundActive == nil || s.BackgroundActive == nil || s.Pending == nil || s.Subagents == nil {
		return false, errors.New("invalid Kit settlement response")
	}
	return *s.Settled && !*s.ForegroundActive && !*s.BackgroundActive && !*s.Pending && *s.Subagents == 0, nil
}

func (p *KitRuntimeProtocol) RunTurn(ctx context.Context, r assistantRuntimeRecord, req runTurnRequest) (returnErr error) {
	if req.Model == "" {
		return errors.New("Kit turn requires the persisted assistant model")
	}
	key := kitSessionKey(r, req.ThreadID)
	session, found, err := p.config.Core.LoadKitRuntimeSession(ctx, key)
	if err != nil {
		return err
	}
	if session.ChatID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	currentIDs := []uuid.UUID{}
	if req.CanonicalUserMessageID != uuid.Nil {
		currentIDs = append(currentIDs, req.CanonicalUserMessageID)
	}
	if !found && session.HasPriorTranscript {
		prior, err := assistantrepo.New(p.config.Core.db).KitProtocolHasPriorMessages(ctx, assistantrepo.KitProtocolHasPriorMessagesParams{ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, ChatID: session.ChatID, CurrentMessageIds: currentIDs})
		if err != nil {
			return err
		}
		if prior {
			return ErrKitUpgradeNoticeUnsupported
		}
	}
	prompt, err := kitProtocolPrompt(req)
	if err != nil {
		return err
	}
	servers := make([]map[string]any, 0, len(req.MCPServers))
	for _, s := range req.MCPServers {
		headers := make([]map[string]string, 0, len(s.Headers))
		for name, value := range s.Headers {
			headers = append(headers, map[string]string{"name": name, "value": value})
		}
		servers = append(servers, map[string]any{"type": "http", "name": s.ID, "url": s.URL, "headers": headers})
	}
	c, err := p.dial(ctx, r)
	if err != nil {
		return err
	}
	defer closeKitTransport(c)
	params := map[string]any{"cwd": p.config.CWD, "mcpServers": servers, "_meta": map[string]string{"ai.speakeasy/gram-chat-id": session.ChatID.String()}}
	if !found {
		raw, e := c.Call(ctx, "session/new", params, "")
		if e != nil {
			return e
		}
		var created struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &created) != nil || created.SessionID == "" {
			return errors.New("invalid Kit session creation response")
		}
		session, err = p.config.Core.StoreKitRuntimeSession(ctx, key, created.SessionID)
		if err != nil {
			return err
		}
	}
	if err = c.AttachSession(ctx, session.SessionID); err != nil {
		return err
	}
	binding := KitTranscriptBinding{RuntimeID: r.ID, ProjectID: r.ProjectID, AssistantID: r.AssistantID, ThreadID: req.ThreadID, ChatID: session.ChatID, SessionID: session.SessionID}
	echoes, err := p.loadUserEchoes(ctx, key, session.SessionID)
	if err != nil {
		return err
	}
	binding.UserEchoes = echoes
	var echoMu sync.RWMutex
	ackReady := make(chan struct{})
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	projection := make(chan error, 1)
	flush := make(chan chan error)
	go func() {
		err := kitConsumeNotifications(work, c.Notifications(), flush, func(n kitacp.Notification) error {
			if n.SessionID != session.SessionID {
				return nil
			}
			var envelope struct {
				Update struct {
					Kind string `json:"sessionUpdate"`
					ID   string `json:"messageId"`
				} `json:"update"`
			}
			_ = json.Unmarshal(n.Params, &envelope)
			if envelope.Update.Kind == "user_message" && req.CanonicalUserMessageID != uuid.Nil {
				echoMu.RLock()
				_, known := binding.UserEchoes[envelope.Update.ID]
				echoMu.RUnlock()
				if !known {
					select {
					case <-ackReady:
					case <-work.Done():
						return work.Err()
					}
				}
			}
			echoMu.RLock()
			defer echoMu.RUnlock()
			_, err := p.config.Projector.Handle(work, binding, n)
			return err
		})
		projection <- err
		cancel()
	}()
	defer func() {
		cancel()
		if err := <-projection; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			if returnErr == nil || errors.Is(returnErr, context.Canceled) {
				returnErr = err
			}
		}
	}()
	// Replay even a newly persisted session: a concurrent creator may have won.
	params["sessionId"] = session.SessionID
	params["replayFrom"] = map[string]string{"type": "start"}
	resume, err := c.Call(work, "session/resume", params, session.SessionID)
	if err != nil {
		return err
	}
	selection, _, err := kitProtocolModelOption(resume, req.Model)
	if err != nil {
		return err
	}
	configured, err := c.Call(work, "session/set_config_option", map[string]string{"sessionId": session.SessionID, "configId": "model", "type": "id", "value": selection}, session.SessionID)
	if err != nil {
		return err
	}
	_, current, err := kitProtocolModelOption(configured, req.Model)
	if err != nil {
		return err
	}
	if current != selection {
		return errors.New("Kit did not apply the persisted assistant model")
	}
	raw, err := c.Call(work, "session/prompt", map[string]any{"sessionId": session.SessionID, "prompt": prompt}, session.SessionID)
	if err != nil {
		return err
	}
	var ack struct {
		MessageID string `json:"messageId"`
	}
	if json.Unmarshal(raw, &ack) != nil || ack.MessageID == "" {
		return errors.New("invalid Kit prompt acknowledgement")
	}
	if req.CanonicalUserMessageID != uuid.Nil {
		if req.EventID == uuid.Nil {
			return errors.New("Kit echo correlation requires event identity")
		}
		echoMu.RLock()
		existing, known := binding.UserEchoes[ack.MessageID]
		echoMu.RUnlock()
		if known && existing != req.CanonicalUserMessageID {
			return errors.New("conflicting Kit user message acknowledgement")
		}
		echo := kitProtocolUserEcho{RuntimeID: r.ID, SessionID: session.SessionID, ACPMessageID: ack.MessageID, MessageID: req.CanonicalUserMessageID}
		if err := p.storeUserEcho(work, key, req.EventID, echo); err != nil {
			return err
		}
		echoMu.Lock()
		binding.UserEchoes[ack.MessageID] = req.CanonicalUserMessageID
		echoMu.Unlock()
	}
	close(ackReady)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-work.Done():
			return work.Err()
		case <-ticker.C:
			raw, err = c.Call(work, "kit/session/status", map[string]string{"sessionId": session.SessionID}, session.SessionID)
			if err != nil {
				return err
			}
			settled, e := kitSettled(raw, session.SessionID)
			if e != nil {
				return e
			}
			if settled {
				// The status reply follows earlier updates on the same session stream.
				// Flush queued updates and the in-flight projection before detaching.
				ack := make(chan error, 1)
				select {
				case flush <- ack:
				case <-work.Done():
					return work.Err()
				}
				select {
				case err := <-ack:
					return err
				case <-work.Done():
					return work.Err()
				}
			}
		}
	}
}

func (p *KitRuntimeProtocol) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (bool, error) {
	session, found, err := p.config.Core.LoadKitRuntimeSession(ctx, kitSessionKey(r, thread))
	if err != nil || !found {
		return false, err
	}
	c, err := p.dial(ctx, r)
	if err != nil {
		return false, err
	}
	defer closeKitTransport(c)
	if err = c.AttachSession(ctx, session.SessionID); err != nil {
		return false, err
	}
	err = c.Cancel(ctx, session.SessionID)
	return err == nil, err
}
func (p *KitRuntimeProtocol) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	c, err := p.dial(ctx, r)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	defer closeKitTransport(c)
	// Gateway reachability is not an idle duration or all-session settlement.
	return RuntimeBackendStatus{Configured: true}, nil
}

// PrepareRuntimeSwitch requires the caller's shared assistant admission lock.
// The caller has checked queued/claimed events and pending wakeups under that
// lock. Thus session/new cannot race this observation: an in-flight admission
// remains an unsettled event until RunTurn finishes, including persistence.
func (p *KitRuntimeProtocol) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, runtimes []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	result := runtimeSwitchReadiness{Release: func() {}}
	// Destination provisioning is checked by the enclosing backend router.
	if p.config.Core == nil || p.config.Core.db == nil || len(runtimes) != 1 || (destination != 2 && destination != 3) {
		return result, errRuntimeSwitchUnavailable
	}
	r := runtimes[0]
	if r.AssistantID != assistant.ID || r.ProjectID != assistant.ProjectID || r.RuntimeVersion != 3 {
		return result, ErrKitRuntimeBinding
	}
	// Plain SELECT only: the caller holds the advisory and runtime row locks.
	// Do not use the session helpers, which acquire another advisory lock.
	sessions, err := assistantrepo.New(p.config.Core.db).ListKitProtocolSessions(ctx, assistantrepo.ListKitProtocolSessionsParams{ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, AssistantID: uuid.NullUUID{UUID: r.AssistantID, Valid: true}, RuntimeID: uuid.NullUUID{UUID: r.ID, Valid: true}})
	if err != nil {
		return result, fmt.Errorf("list Kit sessions for switch: %w", err)
	}
	c, err := p.dial(ctx, r)
	if err != nil {
		return result, err
	}
	defer closeKitTransport(c)
	// Discard observation-only notifications: projection would reacquire the
	// caller's lock. Normal turn completion has already drained its projector.
	drainCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-drainCtx.Done():
				return
			case _, ok := <-c.Notifications():
				if !ok {
					return
				}
			}
		}
	}()
	for _, session := range sessions {
		if err = c.AttachSession(ctx, session); err != nil {
			return result, err
		}
		raw, e := c.Call(ctx, "kit/session/status", map[string]string{"sessionId": session}, session)
		if e != nil {
			return result, e
		}
		settled, e := kitSettled(raw, session)
		if e != nil {
			return result, e
		}
		if !settled {
			return result, nil
		}
	}
	result.Settled = true
	return result, nil
}

// kitConsumeNotifications serializes projection and acknowledges a flush only
// after all notifications queued before the flush have been applied.
func kitConsumeNotifications(ctx context.Context, notifications <-chan kitacp.Notification, flush <-chan chan error, handle func(kitacp.Notification) error) error {
	apply := func(n kitacp.Notification, ok bool) error {
		if !ok {
			return kitacp.ErrClosed
		}
		return handle(n)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case n, ok := <-notifications:
			if err := apply(n, ok); err != nil {
				return err
			}
		case ack := <-flush:
			draining := true
			for draining {
				select {
				case n, ok := <-notifications:
					if err := apply(n, ok); err != nil {
						ack <- err
						return err
					}
				default:
					draining = false
				}
			}
			ack <- nil
		}
	}
}

// Images must already be materialized by server attachment loading. Never fetch
// untrusted image URLs from the control-plane adapter.
func kitProtocolPrompt(req runTurnRequest) ([]map[string]any, error) {
	prompt := []map[string]any{{"type": "text", "text": req.Prompt}}
	for _, part := range req.InputParts {
		switch part.Type {
		case "text":
			prompt = append(prompt, map[string]any{"type": "text", "text": part.Text})
		case "image_url":
			if part.ImageURL == nil {
				return nil, errors.New("missing Kit image content")
			}
			header, data, ok := strings.Cut(part.ImageURL.URL, ",")
			if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
				return nil, errors.New("Kit images require base64 image data URIs")
			}
			if _, err := base64.StdEncoding.DecodeString(data); err != nil {
				return nil, errors.New("invalid Kit image data")
			}
			prompt = append(prompt, map[string]any{"type": "image", "mimeType": strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), "data": data})
		default:
			return nil, errors.New("unsupported Kit prompt content type")
		}
	}
	return prompt, nil
}

type kitProtocolUserEcho struct {
	RuntimeID    uuid.UUID `json:"runtime_id"`
	SessionID    string    `json:"session_id"`
	ACPMessageID string    `json:"acp_message_id"`
	MessageID    uuid.UUID `json:"message_id"`
}

func (p *KitRuntimeProtocol) loadUserEchoes(ctx context.Context, key kitRuntimeSessionKey, session string) (map[string]uuid.UUID, error) {
	rows, err := assistantrepo.New(p.config.Core.db).ListKitProtocolUserEchoes(ctx, assistantrepo.ListKitProtocolUserEchoesParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, ThreadID: key.ThreadID})
	if err != nil {
		return nil, err
	}
	result := map[string]uuid.UUID{}
	for _, raw := range rows {
		var echoes []kitProtocolUserEcho
		if err = json.Unmarshal(raw, &echoes); err != nil {
			return nil, err
		}
		for _, echo := range echoes {
			if echo.RuntimeID == key.RuntimeID && echo.SessionID == session && echo.ACPMessageID != "" && echo.MessageID != uuid.Nil {
				if previous, ok := result[echo.ACPMessageID]; ok && previous != echo.MessageID {
					return nil, errors.New("conflicting persisted Kit user echo correlation")
				}
				result[echo.ACPMessageID] = echo.MessageID
			}
		}
	}
	return result, nil
}

func (p *KitRuntimeProtocol) storeUserEcho(ctx context.Context, key kitRuntimeSessionKey, eventID uuid.UUID, echo kitProtocolUserEcho) error {
	tx, queries, err := p.config.Core.beginKitRuntimeTransaction(ctx, key.ProjectID, key.AssistantID, key.RuntimeID)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = p.config.Core.ValidateKitRuntimeSessionInTx(ctx, tx, key, echo.SessionID); err != nil {
		return err
	}
	raw, err := json.Marshal([]kitProtocolUserEcho{echo})
	if err != nil {
		return err
	}
	affected, err := queries.StoreKitProtocolUserEcho(ctx, assistantrepo.StoreKitProtocolUserEchoParams{Echo: raw, ProjectID: key.ProjectID, AssistantID: key.AssistantID, ThreadID: key.ThreadID, EventID: eventID, MessageID: echo.MessageID})
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("Kit canonical user message correlation unavailable")
	}
	return tx.Commit(ctx)
}

// Kit ModelSelection::id uses provider:model. Only the Speakeasy provider
// carries this runtime's canonical chat attribution and scoped credentials.
// Names are display-only; selection must exist as an exact advertised value.
func kitProtocolModelOption(raw json.RawMessage, model string) (selection, current string, err error) {
	if model == "" {
		return "", "", errors.New("Kit turn requires the persisted assistant model")
	}
	type choice struct {
		Value   string `json:"value"`
		Options []struct {
			Value string `json:"value"`
		} `json:"options"`
	}
	var result struct {
		ConfigOptions []struct {
			ID      string   `json:"configId"`
			Type    string   `json:"type"`
			Current string   `json:"currentValue"`
			Options []choice `json:"options"`
		} `json:"configOptions"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", "", errors.New("invalid Kit model catalog")
	}
	selection = "speakeasy:" + model
	matches := 0
	configs := 0
	for _, config := range result.ConfigOptions {
		if config.ID != "model" {
			continue
		}
		configs++
		if config.Type != "select" {
			return "", "", errors.New("unsupported Kit model configuration type")
		}
		current = config.Current
		for _, option := range config.Options {
			if option.Value == selection {
				matches++
			}
			for _, grouped := range option.Options {
				if grouped.Value == selection {
					matches++
				}
			}
		}
	}
	if configs != 1 || matches != 1 {
		return "", "", errors.New("persisted assistant model is not uniquely offered by the Kit Speakeasy catalog")
	}
	return selection, current, nil
}
