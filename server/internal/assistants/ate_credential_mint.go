package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/atecred"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgserving"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ atecred.ActorCredentialMinter = (*ServiceCore)(nil)

// MintATEActorCredential mints a short-lived Speakeasy principal turn
// credential for a live running ATE thread actor. The minted credential uses
// the same runtime-bound format admitted by the Project Gateway and
// /chat/completions, with TranscriptOwned set so Kit's ACP projector remains
// the sole transcript writer.
func (s *ServiceCore) MintATEActorCredential(ctx context.Context, atespace, actorName, actorUID string) (string, time.Time, error) {
	atespace = strings.TrimSpace(atespace)
	actorName = strings.TrimSpace(actorName)
	actorUID = strings.TrimSpace(actorUID)
	if atespace == "" || actorName == "" || actorUID == "" {
		return "", time.Time{}, fmt.Errorf("%w: missing actor coordinates", atecred.ErrActorCredentialDenied)
	}
	if s.ateAtespace == "" || atespace != s.ateAtespace {
		return "", time.Time{}, fmt.Errorf("%w: atespace mismatch", atecred.ErrActorCredentialDenied)
	}
	if s.assistantTokens == nil || s.identities == nil {
		return "", time.Time{}, errors.New("ATE credential minting requires assistant token and identity services")
	}

	row, err := assistantrepo.New(s.db).GetLiveAssistantATEActorByUID(ctx, assistantrepo.GetLiveAssistantATEActorByUIDParams{
		ActorUid:              actorUID,
		RunningState:          ateActorStateRunning,
		RuntimeStartingState:  runtimeStateStarting,
		RuntimeActiveState:    runtimeStateActive,
		AssistantActiveStatus: StatusActive,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("%w: actor not live", atecred.ErrActorCredentialDenied)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("load live ATE actor: %w", err)
	}
	if row.ActorName != actorName || row.ActorUid != actorUID {
		return "", time.Time{}, fmt.Errorf("%w: actor name mismatch", atecred.ErrActorCredentialDenied)
	}

	var meta kitATEMetadata
	if err := json.Unmarshal(row.BackendMetadataJson, &meta); err != nil ||
		meta.Atespace != atespace ||
		meta.RuntimeID != row.RuntimeID ||
		meta.AssistantID != row.AssistantID ||
		meta.ProjectID != row.ProjectID {
		return "", time.Time{}, fmt.Errorf("%w: runtime metadata mismatch", atecred.ErrActorCredentialDenied)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, row.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("%w: organization not found", atecred.ErrActorCredentialDenied)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("load ATE actor organization: %w", err)
	}
	if refusal := orgserving.Check(org); refusal != orgserving.RefusalNone {
		return "", time.Time{}, fmt.Errorf("%w: organization %s", atecred.ErrActorCredentialDenied, refusal)
	}

	var triggerID uuid.UUID
	if row.TriggerInstanceID.Valid {
		triggerID = row.TriggerInstanceID.UUID
	} else {
		triggerID, err = s.resolveDashboardTriggerInstance(ctx, row.OrganizationID, row.ProjectID, row.AssistantID, row.AssistantName)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("resolve ATE actor execution trigger: %w", err)
		}
	}

	resolution, err := s.identities.Resolve(ctx, s.db, row.OrganizationID, row.ProjectID, row.AssistantID, triggerID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("resolve ATE actor identity: %w", err)
	}
	if resolution.State != assistantidentity.Active || resolution.Identity == nil {
		return "", time.Time{}, fmt.Errorf("%w: trigger workload identity is %s", atecred.ErrActorCredentialDenied, resolution.State)
	}
	workload := *resolution.Identity

	// Check live agent and owner eligibility up front so revoked/suspended
	// agents or removed owners fail closed at mint time as well as on every
	// gateway/inference request.
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, workload.AgentID.String())
	agentRec, err := agents.ResolvePrincipal(ctx, s.db, row.OrganizationID, agentPrincipal)
	if errors.Is(err, agents.ErrPrincipalNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("%w: agent not found", atecred.ErrActorCredentialDenied)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("resolve ATE actor agent: %w", err)
	}
	if agents.DeriveLifecycle(agentRec) != agents.LifecycleActive || agentRec.OwnerReassignmentRequiredAt.Valid {
		return "", time.Time{}, fmt.Errorf("%w: agent not active", atecred.ErrActorCredentialDenied)
	}
	_, ownerEligible, err := runtimepolicy.ResolveEligibleUser(ctx, s.db, row.OrganizationID, agentRec.OwnerUserID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("check ATE actor owner eligibility: %w", err)
	}
	if !ownerEligible {
		return "", time.Time{}, fmt.Errorf("%w: agent owner ineligible", atecred.ErrActorCredentialDenied)
	}

	versionID, err := s.bindExecutionProfile(ctx, executionprofiles.Target{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		AssistantID:    row.AssistantID,
		TriggerID:      triggerID,
		Context:        executionprofiles.ContextUserless,
	})
	if err != nil {
		return "", time.Time{}, err
	}

	cred := principalcredential.Credential{
		OrganizationID:            row.OrganizationID,
		ProjectID:                 row.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(workload.IssuerID, workload.Subject),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: versionID, Valid: true},
	}

	token, expiresAt, err := s.assistantTokens.MintTurnCredentialWithLifetime(ctx, cred, assistanttokens.RuntimeBinding{
		OrganizationID:  row.OrganizationID,
		ProjectID:       row.ProjectID,
		AssistantID:     row.AssistantID,
		ThreadID:        row.AssistantThreadID,
		ChatID:          row.ChatID,
		TranscriptOwned: true,
	}, atecred.MintedCredentialLifetime)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("mint ATE actor turn credential: %w", err)
	}
	return token, expiresAt, nil
}
