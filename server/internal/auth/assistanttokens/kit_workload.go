package assistanttokens

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type inferenceWorkloadContextKey struct{}

// AuthorizeKitWorkload adds attribution after workload verification, live
// admission, agent assignment and inference authorization. It preserves that
// authorization context, including the owner's and agent's live policy.
// Only authoritative runtime metadata can establish transcript ownership.
func (m *Manager) AuthorizeKitWorkload(ctx context.Context) (context.Context, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if !ok || ac == nil || ac.ProjectID == nil || !hasActor || actor.Type != urn.PrincipalTypeWorkload {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	issuerID, subject, err := actor.Workload()
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	rows, err := m.tokens.ListKitWorkloadBindings(ctx, tokenrepo.ListKitWorkloadBindingsParams{
		ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID, IssuerID: issuerID.String(), Subject: subject,
	})
	if err != nil {
		return ctx, fmt.Errorf("read managed workload binding: %w", err)
	}
	// Ordinary workloads have no assistant authority.
	if len(rows) == 0 {
		return ctx, nil
	}
	if len(rows) != 1 || !rows[0].Active {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	row := rows[0]
	var metadata struct {
		Workload struct {
			IssuerID uuid.UUID                  `json:"issuer_id"`
			Subject  string                     `json:"subject"`
			Identity assistantidentity.Identity `json:"identity"`
		} `json:"workload"`
	}
	if json.Unmarshal(row.BackendMetadataJson, &metadata) != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	identity := metadata.Workload.Identity
	agent, hasAgent := contextvalues.EffectiveAgent(ctx)
	if metadata.Workload.IssuerID != issuerID || metadata.Workload.Subject != subject ||
		identity.ProjectID != *ac.ProjectID || identity.OrganizationID != ac.ActiveOrganizationID || identity.AssistantID != row.AssistantID ||
		!hasAgent || agent != urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID.String()) {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	// Validate re-resolves the current assistant, trigger, owner, agent and
	// identity subject. A replaced identity or disabled trigger cannot bind.
	if err := new(assistantidentity.Service).Validate(ctx, m.db, identity); err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	var snapshot struct {
		Workload json.RawMessage `json:"workload"`
	}
	if err := json.Unmarshal(row.BackendMetadataJson, &snapshot); err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	ctx = context.WithValue(ctx, inferenceWorkloadContextKey{}, []byte(snapshot.Workload))
	ctx = context.WithValue(ctx, inferenceContextKey{}, identity)
	return context.WithValue(ctx, inferenceRuntimeContextKey{}, row.RuntimeID), nil
}
