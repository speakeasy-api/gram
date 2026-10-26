package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// validateWakeRuntimeAuthority runs under the assistant selection advisory lock
// in the wake transaction. Only persisted metadata binds an authenticated workload
// to a runtime generation; neither client registrations nor subject prefixes do.
func validateWakeRuntimeAuthority(ctx context.Context, tx pgx.Tx, params CreateWakeInstanceParams) error {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || actor.Type != urn.PrincipalTypeWorkload {
		return nil
	}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.ProjectID == nil || *auth.ProjectID != params.ProjectID || auth.ActiveOrganizationID != params.OrganizationID {
		return fmt.Errorf("%w: wake workload tenancy mismatch", ErrBadRequest)
	}
	issuer, subject, err := actor.Workload()
	if err != nil {
		return fmt.Errorf("%w: resolve wake workload actor: %w", ErrBadRequest, err)
	}
	if auth.SessionID != nil {
		_, err := triggerrepo.New(tx).GetWakeWorkloadSession(ctx, triggerrepo.GetWakeWorkloadSessionParams{
			Jti: *auth.SessionID, SubjectUrn: urn.NewWorkloadSubject(issuer, subject), OrganizationID: conv.ToPGText(params.OrganizationID), ProjectID: uuid.NullUUID{UUID: params.ProjectID, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: wake workload session is no longer active", ErrBadRequest)
		}
		if err != nil {
			return fmt.Errorf("resolve wake workload session: %w", err)
		}
	}
	rows, err := tokenrepo.New(tx).ListKitWorkloadBindings(ctx, tokenrepo.ListKitWorkloadBindingsParams{
		ProjectID: params.ProjectID, OrganizationID: params.OrganizationID, IssuerID: issuer.String(), Subject: subject,
	})
	if err != nil {
		return fmt.Errorf("resolve wake managed workload: %w", err)
	}
	// Reaping can clear runtime metadata after withdrawing its workload. Do not
	// trust the pre-lock authentication snapshot when no metadata remains: fence
	// ordinary admission/assignment against withdrawal before granting a wake.
	if len(rows) == 0 {
		if _, err := workloadrepo.New(tx).LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: params.OrganizationID, ID: issuer}); err != nil {
			return wakeAuthorityReadError("lock wake workload issuer", err)
		}
		admitted, err := workloadidentity.IsAdmitted(ctx, tx, workloadidentity.AdmissionParams{OrganizationID: params.OrganizationID, ProjectID: uuid.NullUUID{UUID: params.ProjectID, Valid: true}, WorkloadIssuerID: issuer, Subject: subject})
		if err != nil {
			return fmt.Errorf("read wake workload admission: %w", err)
		}
		agent, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, tx, workloadidentity.AssignmentParams{OrganizationID: params.OrganizationID, WorkloadIssuerID: issuer, Subject: subject})
		if err != nil {
			return fmt.Errorf("read wake workload assignment: %w", err)
		}
		expected, hasAgent := contextvalues.EffectiveAgent(ctx)
		if !admitted || !assigned || !hasAgent || expected != urn.NewPrincipal(urn.PrincipalTypeAgent, agent.String()) {
			return fmt.Errorf("%w: wake workload authority changed", ErrBadRequest)
		}
		return nil
	}
	if len(rows) != 1 {
		return fmt.Errorf("%w: ambiguous wake runtime authority", ErrBadRequest)
	}
	identity, err := wakeRuntimeIdentity(ctx, rows[0], params, issuer, subject)
	if err != nil {
		return err
	}
	// Use the canonical binding queries and workload admission rules. Importing
	// assistantidentity.Service here would cycle through its trigger writer.
	q := identityrepo.New(tx)
	if _, err := q.LockAssistant(ctx, identityrepo.LockAssistantParams{ProjectID: params.ProjectID, AssistantID: params.AssistantID}); err != nil {
		return wakeAuthorityReadError("lock assistant identity", err)
	}
	binding, err := q.GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{ProjectID: params.ProjectID, AssistantID: params.AssistantID})
	if err != nil {
		return wakeAuthorityReadError("read wake assistant binding", err)
	}
	if binding.OrganizationID != params.OrganizationID || binding.ID != identity.BindingID || binding.Generation != identity.BindingGeneration || binding.OriginalAgentID != identity.AgentID {
		return fmt.Errorf("%w: wake assistant identity changed", ErrBadRequest)
	}
	states, err := q.ListAssistantAgentStates(ctx, identityrepo.ListAssistantAgentStatesParams{ProjectID: params.ProjectID, AssistantIds: []uuid.UUID{params.AssistantID}})
	if err != nil {
		return fmt.Errorf("read wake assistant binding: %w", err)
	}
	if len(states) != 1 || !states[0].AgentActive || states[0].AgentID != identity.AgentID {
		return fmt.Errorf("%w: wake assistant identity changed", ErrBadRequest)
	}
	// Fence the generation-specific workload admission and assignment. Event
	// roots are authorized at delivery, never borrowed by the runtime itself.
	if _, err := workloadrepo.New(tx).LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: params.OrganizationID, ID: issuer}); err != nil {
		return wakeAuthorityReadError("lock wake workload issuer", err)
	}
	for _, workload := range []struct {
		issuer  uuid.UUID
		subject string
	}{{issuer, subject}} {
		admitted, err := workloadidentity.IsAdmitted(ctx, tx, workloadidentity.AdmissionParams{OrganizationID: params.OrganizationID, ProjectID: uuid.NullUUID{UUID: params.ProjectID, Valid: true}, WorkloadIssuerID: workload.issuer, Subject: workload.subject})
		if err != nil {
			return fmt.Errorf("read wake workload admission: %w", err)
		}
		agent, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, tx, workloadidentity.AssignmentParams{OrganizationID: params.OrganizationID, WorkloadIssuerID: workload.issuer, Subject: workload.subject})
		if err != nil {
			return fmt.Errorf("read wake workload assignment: %w", err)
		}
		if !admitted || !assigned || agent != identity.AgentID {
			return fmt.Errorf("%w: wake workload authority changed", ErrBadRequest)
		}
	}
	return nil
}

func wakeAuthorityReadError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s: %w", ErrBadRequest, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// This is a JSON projection of persisted assistantidentity.RuntimeIdentity, not another
// identity registry. Its authority is rechecked with the canonical binding queries.
type wakeIdentitySnapshot struct {
	OrganizationID    string    `json:"organization_id"`
	ProjectID         uuid.UUID `json:"project_id"`
	AssistantID       uuid.UUID `json:"assistant_id"`
	AgentID           uuid.UUID `json:"agent_id"`
	BindingID         uuid.UUID `json:"binding_id"`
	BindingGeneration int64     `json:"binding_generation"`
}

func wakeRuntimeIdentity(ctx context.Context, row tokenrepo.ListKitWorkloadBindingsRow, params CreateWakeInstanceParams, issuer uuid.UUID, subject string) (wakeIdentitySnapshot, error) {
	var metadata struct {
		RuntimeID      uuid.UUID `json:"runtime_id"`
		AssistantID    uuid.UUID `json:"assistant_id"`
		ProjectID      uuid.UUID `json:"project_id"`
		RuntimeVersion int16     `json:"runtime_version"`
		Workload       struct {
			IssuerID uuid.UUID            `json:"issuer_id"`
			Subject  string               `json:"subject"`
			Identity wakeIdentitySnapshot `json:"identity"`
		} `json:"workload"`
	}
	var zero wakeIdentitySnapshot
	if !row.Active {
		return zero, fmt.Errorf("%w: wake runtime is no longer active", ErrBadRequest)
	}
	if err := json.Unmarshal(row.BackendMetadataJson, &metadata); err != nil {
		return zero, fmt.Errorf("decode wake runtime metadata: %w", err)
	}
	identity := metadata.Workload.Identity
	agent, hasAgent := contextvalues.EffectiveAgent(ctx)
	if row.AssistantID != params.AssistantID || metadata.RuntimeID != row.RuntimeID || row.RuntimeID == uuid.Nil ||
		metadata.AssistantID != row.AssistantID || metadata.ProjectID != params.ProjectID || metadata.RuntimeVersion != 3 ||
		metadata.Workload.IssuerID != issuer || metadata.Workload.Subject != subject ||
		identity.BindingID == uuid.Nil || identity.BindingGeneration <= 0 || identity.AgentID == uuid.Nil ||
		identity.OrganizationID != params.OrganizationID || identity.ProjectID != params.ProjectID || identity.AssistantID != params.AssistantID ||
		!hasAgent || agent != urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID.String()) {
		return zero, fmt.Errorf("%w: wake runtime authority mismatch", ErrBadRequest)
	}
	return identity, nil
}
