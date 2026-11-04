package executionprofiles

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// OAuthResume identifies an origin signed into the assistant OAuth state, not
// a target supplied by the browser. The session must be verified independently.
type OAuthResume struct {
	Origin   GetOAuthResumeOriginParams
	UserID   string
	ClientID string
	IssuerID uuid.UUID
	ServerID uuid.UUID
}

// AdoptOAuthSelection copies only the authenticated session's exact choice.
// The old version remains immutable; a stale callback cannot overwrite a newer
// choice. No userless profile is eligible, even if a browser authenticates.
func AdoptOAuthSelection(ctx context.Context, db interface {
	DBTX
	TxStarter
}, in OAuthResume, session sessiontokens.ValidatedSession) (uuid.UUID, error) {
	if !session.Valid() || !session.ExpiresAt().After(time.Now()) || in.UserID == "" || session.Subject().Kind != urn.SessionSubjectKindUser || session.Subject().ID != in.UserID || session.ClientID() != in.ClientID {
		return uuid.Nil, errors.New("OAuth session does not match the originating user and client")
	}
	q := New(db)
	origin, err := q.GetOAuthResumeOrigin(ctx, in.Origin)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve OAuth execution origin: %w", err)
	}
	params := GetOAuthResumeSelectionParams{Jti: session.JTI(), SubjectUrn: session.Subject(), ClientID: in.ClientID, IssuerID: in.IssuerID, McpServerID: in.ServerID, ProjectID: in.Origin.ProjectID, OrganizationID: in.Origin.OrganizationID}
	choice, err := q.GetOAuthResumeSelection(ctx, params)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve authenticated OAuth selection: %w", err)
	}
	if !choice.AuthClientID.Valid && !choice.NoAuth {
		return uuid.Nil, errors.New("OAuth session has no typed upstream choice")
	}
	target := Target{OrganizationID: in.Origin.OrganizationID, ProjectID: in.Origin.ProjectID, AssistantID: in.Origin.AssistantID, TriggerID: origin.TriggerInstanceID, Context: ContextUserBacked}
	err = setChoice(ctx, db, target, in.ServerID, ChoiceInput{AuthClientID: choice.AuthClientID, NoAuth: choice.NoAuth, ExpectedVersion: origin.Version}, SourceIssuance, func(ctx context.Context, tx pgx.Tx, _ *Choice) error {
		// Recheck durable grant evidence in the transaction that publishes the
		// version. A concurrent session revocation or selection edit fails closed.
		current, err := New(tx).GetOAuthResumeSelection(ctx, params)
		if err != nil {
			return err
		}
		if current != choice {
			return ErrChoiceChanged
		}
		_, err = New(tx).GetOAuthResumeOrigin(ctx, in.Origin)
		return err
	})
	if errors.Is(err, ErrVersionChanged) {
		// Identical retries are harmless; never replace a different newer choice.
		view, readErr := Describe(ctx, db, target)
		if readErr != nil {
			return uuid.Nil, readErr
		}
		for _, server := range view.Servers {
			if server.MCPServerID == in.ServerID && server.Choice != nil && server.Choice.AuthClientID == choice.AuthClientID {
				return origin.TriggerInstanceID, nil
			}
		}
	}
	if err != nil {
		return uuid.Nil, err
	}
	return origin.TriggerInstanceID, nil
}
