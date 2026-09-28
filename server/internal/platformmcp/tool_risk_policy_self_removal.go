package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"maps"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type removeSelfFromRiskPolicyInput struct {
	ProjectSlug     string `json:"project_slug"`
	PolicyID        string `json:"policy_id"`
	ExpectedVersion string `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
	Confirmed       bool   `json:"confirmed"`
}

func registerRemoveSelfFromRiskPolicy(reg *Registrar, catalogAvailable bool, handlers *RiskMutationHandlers) {
	handler := unavailableRiskMutationTool[UpdateRiskPolicyToolOutput]()
	description := "Remove only the authenticated requesting user's explicit grant from a targeted risk policy. Risk policy mutations are not enabled in this rollout."
	if catalogAvailable && handlers != nil && handlers.Controls != nil && handlers.RemoveSelf != nil {
		handler = handlers.RemoveSelf
		description = "Remove only the authenticated external OAuth requesting user's explicit grant from an exact risk policy. Read get_risk_policy first and obtain confirmation. Requires a targeted user-only audience with at least one other user; refuses everyone, role-containing audiences, missing explicit self grants, and broader inherited evaluation grants. Does not create an exclusion or affect other policies. No caller-supplied user identity is accepted. Reuse the idempotency key only to replay the same confirmed request."
	}
	schema := closedObject(map[string]*jsonschema.Schema{
		"project_slug":     stringSchema("Exact project slug; never defaults.", 1, 0),
		"policy_id":        uuidSchema("Exact policy ID."),
		"expected_version": stringSchema("Opaque version from a fresh get_risk_policy read.", 1, 0),
		"idempotency_key":  stringSchema("Stable replay key for this confirmed self removal.", 1, 128),
		"confirmed":        {Type: "boolean", Enum: []any{true}},
	}, []string{"project_slug", "policy_id", "expected_version", "idempotency_key", "confirmed"})
	addTool(reg, &mcp.Tool{Name: operationRemoveSelfFromRiskPolicy, Title: "Remove Self From Risk Policy", Description: description, InputSchema: schema, Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: []Audience{AudienceExternal}, ProjectScope: ProjectScopeExplicit}, instrumentRiskMutation(reg, operationRemoveSelfFromRiskPolicy, handler))
}

func (s *riskPolicyMutationService) removeSelfFromPolicyTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
	return s.mutatePolicyTool(ctx, raw, true)
}

func selfRemovalRefusal(message string) error {
	return &RiskMutationError{Code: "invalid_request", Message: message, Cause: ErrRiskMutationInvalid}
}

func selfRemovalAudiencePatch(audienceType string, audience []string, userID string) (json.RawMessage, error) {
	if audienceType != "targeted" {
		return nil, selfRemovalRefusal("Self removal requires a targeted user-only audience; everyone-except-one is not supported.")
	}
	self := urn.NewPrincipal(urn.PrincipalTypeUser, userID).String()
	remaining := []string{}
	found := false
	for _, value := range audience {
		principal, err := urn.ParsePrincipal(value)
		if err != nil || principal.Type != urn.PrincipalTypeUser || principal == authz.AllUsersPrincipal() {
			return nil, selfRemovalRefusal("Self removal cannot prove non-membership for role-containing or unsupported audiences. No policy was changed.")
		}
		if value == self {
			found = true
		} else {
			remaining = append(remaining, value)
		}
	}
	if !found {
		return nil, selfRemovalRefusal("The requesting user has no explicit user grant in this policy audience. No policy was changed.")
	}
	if len(remaining) == 0 {
		return nil, selfRemovalRefusal("Self removal would leave an empty targeted audience. No policy was changed.")
	}
	return json.Marshal(riskPolicyAudienceReplacement{RiskPolicyAudience: RiskPolicyAudience{Type: "targeted", PrincipalURNs: canonicalStrings(remaining)}, Confirm: true})
}

// Exact audience projection intentionally omits wildcard/dimensioned grants.
// Refuse any broader matching evaluation or root grant rather than claiming
// that deleting an exact grant proves the caller is no longer affected. This
// deliberately does not resolve role membership, which can change separately.
// The caller must hold lockSelfRemovalAudience through commit: project/policy
// locks alone do not serialize generic grant writers or protect absent rows.
func refuseInheritedPolicyAudience(ctx context.Context, tx pgx.Tx, organizationID, policyID string) error {
	grants, err := accessrepo.New(tx).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{OrganizationID: organizationID, PrincipalUrn: ""})
	if err != nil {
		return riskMutationUnavailableWithCause(err)
	}
	exact := authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)
	for _, grant := range grants {
		if grant.Scope != string(authz.ScopeRiskPolicyEvaluate) && grant.Scope != string(authz.ScopeRoot) {
			continue
		}
		selector, err := authz.SelectorFromRow(grant.Selectors)
		if err != nil {
			return selfRemovalRefusal("The policy has unsupported inherited evaluation grants. No policy was changed.")
		}
		if grant.Scope == string(authz.ScopeRoot) || (selector.Matches(exact) && !maps.Equal(selector, exact)) {
			return selfRemovalRefusal("Self removal cannot prove non-membership while broader evaluation grants exist. No policy was changed.")
		}
	}
	return nil
}

// External descriptors enforce live organization-admin authorization; managed
// assistants must neither receive exact audience identities nor replace them.
func externalRiskAudiencePrincipal(principal Principal) bool {
	return principal.surface() == SurfacePlatformMCP && principal.ConnectionID != "" && principal.Generation != "" && principal.UserID != ""
}

// lockSelfRemovalAudience serializes the absence check with every grant writer,
// including wildcard inserts that do not acquire project or policy locks. Row
// locks cannot protect absent grants. This rare administrator operation takes
// a table-wide lock, held through the receipt transaction's commit, so grant
// writes in other organizations briefly contend too. Acquire it after the
// project lock and before policy/audience reads. NOWAIT refuses an existing
// writer instead of introducing a wait cycle with writers using other orders.
func lockSelfRemovalAudience(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "LOCK TABLE principal_grants IN SHARE ROW EXCLUSIVE MODE NOWAIT")
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return riskMutationConflict("Audience grants are being changed. No policy was changed. Read the policy again and retry self removal with a fresh version.")
	}
	return riskMutationUnavailableWithCause(err)
}
