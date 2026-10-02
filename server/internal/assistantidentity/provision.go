package assistantidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// Provision creates one dedicated identity, or returns the existing eligible
// binding. The caller owns the transaction, including assistant configuration.
// A failed call must roll back the transaction, never commit its partial writes.
func (s *Service) Provision(ctx context.Context, tx pgx.Tx, p ProvisionParams) (Binding, error) {
	if s == nil {
		return Binding{}, ErrInvalidIdentity
	}
	q := repo.New(tx)
	if p.ActorUserID == "" || p.ActorUserID == urn.AllUsersPrincipalID {
		return Binding{}, ErrActorIneligible
	}
	if err := lockProject(ctx, q, p.OrganizationID, p.ProjectID); err != nil {
		return Binding{}, err
	}
	assistant, err := q.GetAssistant(ctx, repo.GetAssistantParams{OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, AssistantID: p.AssistantID})
	if err != nil {
		return Binding{}, resourceError("load assistant", err)
	}
	if assistant.Deleted || !assistant.ProjectLive {
		return Binding{}, ErrTombstoned
	}
	owner := p.ActorUserID
	if assistant.CreatedByUserID.Valid && assistant.CreatedByUserID.String != "" {
		owner = assistant.CreatedByUserID.String
	}
	members := []string{p.ActorUserID, owner}
	slices.Sort(members)
	for _, member := range slices.Compact(members) {
		if _, err := q.LockActor(ctx, repo.LockActorParams{OrganizationID: p.OrganizationID, UserID: member}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Binding{}, ErrActorIneligible
			}
			return Binding{}, fmt.Errorf("lock provisioning membership: %w", err)
		}
	}
	old, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, AssistantID: p.AssistantID})
	if err == nil {
		if !old.Eligible {
			return Binding{}, ErrTombstoned
		}
		if err := s.bindExistingRoots(ctx, tx, p); err != nil {
			return Binding{}, err
		}
		return bindingFromRow(p.OrganizationID, p.ProjectID, old), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Binding{}, fmt.Errorf("read assistant history: %w", err)
	}
	capabilities, err := ConfiguredCapabilities(ctx, tx, p.OrganizationID, p.ProjectID, p.AssistantID)
	if err != nil {
		return Binding{}, err
	}
	principals, err := authz.ResolveUserPrincipals(ctx, tx, p.OrganizationID, p.ActorUserID)
	if err != nil {
		return Binding{}, fmt.Errorf("resolve provisioning actor policy: %w", err)
	}
	actorPolicy, err := authz.LoadGrants(ctx, tx, p.OrganizationID, principals)
	if err != nil {
		return Binding{}, fmt.Errorf("load provisioning actor policy: %w", err)
	}
	grants, err := runtimepolicy.DelegableGrants(capabilities, actorPolicy, actorPolicy)
	if err != nil {
		return Binding{}, fmt.Errorf("bound assistant grants: %w", err)
	}
	agent, err := agentrepo.New(tx).CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: p.OrganizationID, ProjectID: uuid.NullUUID{UUID: p.ProjectID, Valid: true},
		OwnerUserID: owner, Name: "Assistant " + p.AssistantID.String(),
	})
	if err != nil {
		return Binding{}, fmt.Errorf("create dedicated assistant agent: %w", err)
	}
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	for _, grant := range grants {
		selector, err := json.Marshal(grant.Selector)
		if err != nil {
			return Binding{}, fmt.Errorf("encode assistant grant: %w", err)
		}
		if _, err := accessrepo.New(tx).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{
			OrganizationID: p.OrganizationID, PrincipalUrn: principal, Scope: string(grant.Scope), Selectors: selector,
		}); err != nil {
			return Binding{}, fmt.Errorf("write assistant grant: %w", err)
		}
	}
	if _, err := q.CreateAssistantBinding(ctx, repo.CreateAssistantBindingParams{
		OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, AssistantID: p.AssistantID, AgentID: agent.ID,
	}); err != nil {
		return Binding{}, fmt.Errorf("create assistant binding: %w", err)
	}
	metadata, err := json.Marshal(struct {
		CreatorUserID           *string   `json:"creator_user_id"`
		ConsentUserID           string    `json:"consent_user_id"`
		ProvisioningActorUserID string    `json:"provisioning_actor_user_id"`
		AgentID                 uuid.UUID `json:"agent_id"`
		Generation              int64     `json:"generation"`
	}{CreatorUserID: nullableString(assistant.CreatedByUserID.String, assistant.CreatedByUserID.Valid), ConsentUserID: p.ActorUserID, ProvisioningActorUserID: p.ActorUserID, AgentID: agent.ID, Generation: 1})
	if err != nil {
		return Binding{}, fmt.Errorf("encode provisioning provenance: %w", err)
	}
	if err := q.RecordProvisioning(ctx, repo.RecordProvisioningParams{
		OrganizationID: p.OrganizationID, ProjectID: uuid.NullUUID{UUID: p.ProjectID, Valid: true},
		ActorUserID: p.ActorUserID, AssistantID: p.AssistantID.String(), Metadata: metadata,
	}); err != nil {
		return Binding{}, fmt.Errorf("record provisioning provenance: %w", err)
	}
	if err := s.bindExistingRoots(ctx, tx, p); err != nil {
		return Binding{}, err
	}
	return Binding{OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, AssistantID: p.AssistantID, AgentID: agent.ID, Generation: 1}, nil
}

// Upgrade is explicit consent to provision a legacy assistant. Management
// authorization is checked by the caller; permanent tombstones are never reused.
func (s *Service) Upgrade(ctx context.Context, tx pgx.Tx, p ProvisionParams) (Binding, error) {
	return s.Provision(ctx, tx, p)
}

// ConfiguredCapabilities derives only concrete runtime capabilities from the
// persisted configuration. Empty configuration produces an empty policy.
func ConfiguredCapabilities(ctx context.Context, tx pgx.Tx, org string, project, assistant uuid.UUID) ([]authz.Grant, error) {
	q := repo.New(tx)
	a, err := q.GetAssistant(ctx, repo.GetAssistantParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if err != nil {
		return nil, resourceError("load configured assistant", err)
	}
	if a.Deleted || !a.ProjectLive {
		return nil, ErrTombstoned
	}
	servers, err := q.ListConfiguredMCPServers(ctx, repo.ListConfiguredMCPServersParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if err != nil {
		return nil, fmt.Errorf("load configured MCP servers: %w", err)
	}
	skills, err := q.ListConfiguredSkills(ctx, repo.ListConfiguredSkillsParams{OrganizationID: org, ProjectID: project, AssistantID: uuid.NullUUID{UUID: assistant, Valid: true}})
	if err != nil {
		return nil, fmt.Errorf("load configured skills: %w", err)
	}
	grants := make([]authz.Grant, 0, len(servers)*2+len(skills))
	for _, id := range servers {
		grants = append(grants, authz.NewGrant(authz.ScopeMCPConnect, id.String()), authz.NewGrant(authz.ScopeMCPRead, id.String()))
	}
	if len(skills) > 0 {
		grants = append(grants, authz.NewGrant(authz.ScopeSkillRead, project.String()))
	}
	for _, id := range skills {
		grants = append(grants, authz.NewGrant(authz.ScopeSkillRead, id.String()))
	}
	return grants, nil
}

func (s *Service) bindExistingRoots(ctx context.Context, tx pgx.Tx, p ProvisionParams) error {
	roots, err := repo.New(tx).ListAssistantRoots(ctx, repo.ListAssistantRootsParams{OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, AssistantID: p.AssistantID.String()})
	if err != nil {
		return fmt.Errorf("list assistant roots: %w", err)
	}
	for _, root := range roots {
		if err := s.BindRootTrigger(ctx, tx, p.OrganizationID, p.ProjectID, root); err != nil {
			return err
		}
	}
	return nil
}

func lockProject(ctx context.Context, q *repo.Queries, org string, project uuid.UUID) error {
	if org == "" || project == uuid.Nil {
		return ErrInvalidIdentity
	}
	if _, err := q.LockProject(ctx, repo.LockProjectParams{OrganizationID: org, ProjectID: project}); err != nil {
		return resourceError("lock identity project", err)
	}
	return nil
}

func resourceError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func nullableString(value string, valid bool) *string {
	if !valid {
		return nil
	}
	return &value
}

func bindingFromRow(org string, project uuid.UUID, b repo.GetAssistantBindingRow) Binding {
	return Binding{OrganizationID: org, ProjectID: project, AssistantID: b.OriginalAssistantID, AgentID: b.OriginalAgentID, Generation: b.Generation}
}

// RevokeIfCapabilitiesChanged retires sessions whose captured capability ceiling
// may no longer be valid. Direct policy grants are edited through policy APIs;
// SnapshotCeiling intersects them with the current configured capabilities.
// A no-op configuration update does not revoke sessions or change generations.
func RevokeIfCapabilitiesChanged(ctx context.Context, tx pgx.Tx, org string, project, assistant uuid.UUID, before []authz.Grant) error {
	after, err := ConfiguredCapabilities(ctx, tx, org, project, assistant)
	if err != nil {
		return err
	}
	if slices.EqualFunc(before, after, func(a, b authz.Grant) bool { return a.Scope == b.Scope && maps.Equal(a.Selector, b.Selector) }) {
		return nil
	}
	binding, err := repo.New(tx).GetAssistantBinding(ctx, repo.GetAssistantBindingParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load changed capability binding: %w", err)
	}
	if err := workloadidentity.RevokeAgentWorkloadSessionsTx(ctx, tx, org, binding.OriginalAgentID); err != nil {
		return fmt.Errorf("retire changed capability sessions: %w", err)
	}
	return nil
}
