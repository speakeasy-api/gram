package triggers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// validateWakeRuntimeAuthority runs only while holding the assistant selection
// advisory lock in the wake transaction. Authentication has already established
// the actor; runtime authority comes from its persisted session, not parameters.
func validateWakeRuntimeAuthority(ctx context.Context, db triggerrepo.DBTX, params CreateWakeInstanceParams) error {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || actor.Type != urn.PrincipalTypeWorkload {
		return nil
	}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.SessionID == nil {
		// Legacy principal credentials do not carry an OAuth session.
		return nil
	}
	if *auth.SessionID == "" || auth.ProjectID == nil || *auth.ProjectID != params.ProjectID || auth.ActiveOrganizationID != params.OrganizationID {
		return fmt.Errorf("%w: wake workload session tenancy mismatch", ErrBadRequest)
	}
	issuer, subject, err := actor.Workload()
	if err != nil {
		return fmt.Errorf("%w: resolve wake workload actor: %w", ErrBadRequest, err)
	}
	subjectURN := urn.NewWorkloadSubject(issuer, subject)
	clientID, err := triggerrepo.New(db).GetWakeWorkloadSessionClient(ctx, triggerrepo.GetWakeWorkloadSessionClientParams{
		Jti: *auth.SessionID, SubjectUrn: subjectURN, OrganizationID: conv.ToPGText(params.OrganizationID), ProjectID: uuid.NullUUID{UUID: params.ProjectID, Valid: true},
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("resolve wake workload session: %w", err)
		}
		return fmt.Errorf("%w: resolve wake workload session: %w", ErrBadRequest, err)
	}
	if !clientID.Valid {
		// Existing assertion-based workload sessions have no registration.
		return nil
	}
	binding, err := usersessionsrepo.New(db).GetAssistantRuntimeForWorkloadSession(ctx, usersessionsrepo.GetAssistantRuntimeForWorkloadSessionParams{
		Jti: *auth.SessionID, SubjectUrn: subjectURN, OrganizationID: conv.ToPGText(params.OrganizationID), ProjectID: params.ProjectID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("wake runtime authority is no longer active: %w", err)
		}
		return fmt.Errorf("%w: wake runtime authority is no longer active: %w", ErrBadRequest, err)
	}
	if binding.AssistantID != params.AssistantID || binding.ClientRegistrationID != clientID.UUID {
		return fmt.Errorf("%w: wake runtime authority mismatch", ErrBadRequest)
	}
	return nil
}
