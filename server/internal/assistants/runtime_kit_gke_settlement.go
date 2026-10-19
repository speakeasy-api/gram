package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

// VerifyCheckpointSettled observes all gateway sessions using the canonical
// authenticated ACP transport. The caller must hold assistant admission and
// exclude queued events until the guest is fenced and checkpointed. This is
// deliberately separate from DB-only runtime-switch readiness.
func (p *KitRuntimeProtocol) VerifyCheckpointSettled(ctx context.Context, r assistantRuntimeRecord) error {
	if p.config.Core == nil || p.config.Core.db == nil || r.RuntimeVersion != 3 {
		return ErrKitRuntimeBinding
	}
	sessions, err := runtimeSwitchQueries(ctx, p.config.Core.db).ListKitProtocolSessions(ctx, assistantrepo.ListKitProtocolSessionsParams{ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, AssistantID: uuid.NullUUID{UUID: r.AssistantID, Valid: true}, RuntimeID: uuid.NullUUID{UUID: r.ID, Valid: true}})
	if err != nil {
		return fmt.Errorf("list canonical checkpoint sessions: %w", err)
	}
	bound := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		if session.AcpSessionID == "" || !session.ChatID.Valid || session.ChatID.UUID == uuid.Nil || bound[session.AcpSessionID] {
			return ErrKitRuntimeBinding
		}
		bound[session.AcpSessionID] = true
	}
	c, err := p.dial(ctx, r)
	if err != nil {
		return err
	}
	defer closeKitTransport(c)
	// Observation does not replay or project transcripts while admission is held.
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
	return verifyKitCheckpointSessions(ctx, c, p.config.CWD, bound)
}

type kitCheckpointTransport interface {
	Call(context.Context, string, any, string) (json.RawMessage, error)
	AttachSession(context.Context, string) error
}

type kitCheckpointSession struct {
	CWD   string
	State string
}

func kitCheckpointInventory(raw json.RawMessage) (map[string]kitCheckpointSession, error) {
	var response struct {
		Sessions *[]struct {
			ID   string                     `json:"sessionId"`
			CWD  string                     `json:"cwd"`
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"sessions"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Sessions == nil || response.NextCursor != nil {
		return nil, errors.New("incomplete Kit gateway session inventory")
	}
	inventory := make(map[string]kitCheckpointSession, len(*response.Sessions))
	for _, row := range *response.Sessions {
		var state string
		if err := json.Unmarshal(row.Meta["kit.gateway.state"], &state); err != nil || row.ID == "" || row.CWD == "" {
			return nil, errors.New("invalid Kit gateway session inventory")
		}
		if _, duplicate := inventory[row.ID]; duplicate {
			return nil, errors.New("duplicate Kit gateway session")
		}
		// These are native gateway catalog states, not foreground turn states.
		// Exited sessions without a durable catalog entry cannot be checkpointed.
		if state != "resident" && state != "restorable" {
			return nil, errors.New("kit gateway session is not safely checkpointable")
		}
		inventory[row.ID] = kitCheckpointSession{CWD: row.CWD, State: state}
	}
	return inventory, nil
}

func verifyKitCheckpointSessions(ctx context.Context, c kitCheckpointTransport, cwd string, bound map[string]bool) error {
	list := func() (map[string]kitCheckpointSession, error) {
		// Do not filter by cwd: unbound sessions in any approved root must be seen.
		raw, err := c.Call(ctx, "session/list", map[string]any{}, "")
		if err != nil {
			return nil, fmt.Errorf("list Kit gateway sessions: %w", err)
		}
		return kitCheckpointInventory(raw)
	}
	before, err := list()
	if err != nil {
		return err
	}
	for id := range bound {
		row, found := before[id]
		if !found || row.CWD != cwd {
			return ErrKitRuntimeBinding
		}
	}
	for id, row := range before {
		if row.State == "restorable" {
			continue
		}
		// An unbound process has no canonical projector owner to certify durable
		// completion. Even foreground-idle unbound sessions fail closed.
		if !bound[id] {
			return errors.New("unbound resident Kit session prevents checkpoint")
		}
		if err := c.AttachSession(ctx, id); err != nil {
			return fmt.Errorf("attach checkpoint session: %w", err)
		}
		if _, err := c.Call(ctx, "session/resume", map[string]string{"sessionId": id, "cwd": cwd}, id); err != nil {
			return fmt.Errorf("resume checkpoint session: %w", err)
		}
		raw, err := c.Call(ctx, "kit/session/status", map[string]string{"sessionId": id}, id)
		if err != nil {
			return fmt.Errorf("observe checkpoint session: %w", err)
		}
		settled, err := kitSettled(raw, id)
		if err != nil {
			return err
		}
		if !settled {
			return errors.New("kit session has unsettled foreground or background work")
		}
	}
	after, err := list()
	if err != nil {
		return err
	}
	if !maps.Equal(before, after) {
		return errors.New("kit gateway session inventory changed during settlement")
	}
	return nil
}
