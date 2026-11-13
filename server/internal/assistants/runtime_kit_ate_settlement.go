package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

// CloseNativeThreadSession runs under the caller's lifecycle admission fence.
// Native close releases the workspace writer before the actor can be suspended.
func (p *KitRuntimeProtocol) CloseNativeThreadSession(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) error {
	if p.config.Endpoint == nil || p.config.Endpoint.Path != "/acp" || r.LifecycleTx == nil ||
		thread == uuid.Nil || r.ID == uuid.Nil || r.ProjectID == uuid.Nil || r.AssistantID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	// Use the existing transaction: the public session loader takes another
	// advisory lock and cannot be used while lifecycle admission owns that lock.
	row, err := assistantrepo.New(r.LifecycleTx).GetKitRuntimeSessionBinding(ctx, assistantrepo.GetKitRuntimeSessionBindingParams{
		RuntimeID: r.ID, ProjectID: r.ProjectID, AssistantID: r.AssistantID, ThreadID: thread,
	})
	session := ""
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read native Kit thread session: %w", err)
	}
	if err == nil {
		if row.RuntimeID != r.ID || row.ThreadID != thread || row.ChatID == uuid.Nil ||
			(row.AcpSessionID.Valid && row.AcpSessionID.String == "") {
			return ErrKitRuntimeBinding
		}
		if row.AcpSessionID.Valid {
			session = row.AcpSessionID.String
		}
	}
	c, err := p.dial(ctx, r)
	if err != nil {
		return err
	}
	defer closeKitTransport(c)
	return p.closeNativeThreadSession(ctx, c, session)
}

// Native session/list is the durable workspace catalog, not a resident-actor
// inventory. Closing a session retains that catalog entry for the next resume.
func nativeThreadCatalog(raw json.RawMessage, cwd, canonical string) error {
	var catalog struct {
		Sessions *[]struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
		} `json:"sessions"`
		NextCursor json.RawMessage `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &catalog) != nil || catalog.Sessions == nil ||
		(len(catalog.NextCursor) != 0 && string(catalog.NextCursor) != "null") {
		return errors.New("invalid or paginated native Kit session catalog")
	}
	sessions := *catalog.Sessions
	if canonical == "" {
		if len(sessions) != 0 {
			return errors.New("unbound native Kit session catalog")
		}
		return nil
	}
	if len(sessions) != 1 || sessions[0].SessionID != canonical || sessions[0].CWD != cwd {
		return errors.New("native Kit session catalog differs from thread binding")
	}
	return nil
}

func (p *KitRuntimeProtocol) closeNativeThreadSession(ctx context.Context, c *kitacp.Client, session string) error {
	// Resume may emit configuration notifications even without replay. Consume
	// both streams without projecting or reacquiring lifecycle admission locks.
	drain, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-c.Notifications():
			case <-drain.Done():
				return
			}
		}
	}()
	inventory := func() error {
		raw, err := c.Call(ctx, "session/list", map[string]string{"cwd": p.config.CWD}, "")
		if err != nil {
			return fmt.Errorf("list native Kit thread sessions: %w", err)
		}
		return nativeThreadCatalog(raw, p.config.CWD, session)
	}
	if err := inventory(); err != nil {
		return err
	}
	if session == "" {
		return nil
	}
	if err := c.AttachSession(ctx, session); err != nil {
		return fmt.Errorf("attach native Kit thread session: %w", err)
	}
	if _, err := c.Call(ctx, "session/resume", map[string]string{"sessionId": session, "cwd": p.config.CWD}, session); err != nil {
		return fmt.Errorf("resume native Kit thread session: %w", err)
	}
	raw, err := c.Call(ctx, "kit/session/status", map[string]string{"sessionId": session}, session)
	if err != nil {
		return fmt.Errorf("observe native Kit thread settlement: %w", err)
	}
	settled, err := kitSettled(raw, session)
	if err != nil {
		return err
	}
	if !settled {
		return errors.New("native Kit thread session is not settled")
	}
	if _, err := c.Call(ctx, "session/close", map[string]string{"sessionId": session}, session); err != nil {
		return fmt.Errorf("close native Kit thread session: %w", err)
	}
	return inventory()
}
