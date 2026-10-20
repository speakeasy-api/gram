package assistanttokens

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MaxInferenceLifetime bounds leaked startup credentials even if explicit revocation fails.
const MaxInferenceLifetime = 30 * 24 * time.Hour

type InferenceIssueInput struct {
	Identity  assistantidentity.Identity
	RuntimeID uuid.UUID
	Lifetime  time.Duration
}
type inferenceContextKey struct{}
type inferenceRuntimeContextKey struct{}

// InferenceTranscriptOwned reports a trusted runtime whose ACP transcript owns persistence.
func InferenceTranscriptOwned(ctx context.Context) bool {
	_, ok := ctx.Value(inferenceContextKey{}).(assistantidentity.Identity)
	return ok
}

func (m *Manager) RevokeInferenceCredential(ctx context.Context, projectID, credentialID uuid.UUID) error {
	if err := m.tokens.RevokeInferenceCredential(ctx, tokenrepo.RevokeInferenceCredentialParams{ProjectID: projectID, ApiKeyID: credentialID}); err != nil {
		return fmt.Errorf("revoke inference credential: %w", err)
	}
	return nil
}

// RevokeRuntimeInferenceCredentials invalidates every startup credential for a
// generation, including before restore of a retained runtime row. The caller
// must hold runtime startup admission while revoking and issuing its replacement.
func (m *Manager) RevokeRuntimeInferenceCredentials(ctx context.Context, projectID, runtimeID uuid.UUID) error {
	if err := m.tokens.RevokeRuntimeInferenceCredentials(ctx, tokenrepo.RevokeRuntimeInferenceCredentialsParams{ProjectID: projectID, RuntimeID: runtimeID}); err != nil {
		return fmt.Errorf("revoke runtime inference credentials: %w", err)
	}
	return nil
}

// RevokeRuntimeInferenceCredentialsInTx rotates authority atomically with lifecycle state.
// The caller owns the transaction and must hold runtime startup admission.
func (m *Manager) RevokeRuntimeInferenceCredentialsInTx(ctx context.Context, tx pgx.Tx, projectID, runtimeID uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("missing inference revocation transaction")
	}
	if err := tokenrepo.New(tx).RevokeRuntimeInferenceCredentials(ctx, tokenrepo.RevokeRuntimeInferenceCredentialsParams{ProjectID: projectID, RuntimeID: runtimeID}); err != nil {
		return fmt.Errorf("revoke runtime inference credentials: %w", err)
	}
	return nil
}

// snapshotInferenceCeiling keeps startup credentials bounded by the agent's
// issuance-time policy even though ordinary principal assertions derive it live.
func (m *Manager) snapshotInferenceCeiling(ctx context.Context, tx pgx.Tx, identity assistantidentity.Identity) (inferenceCeiling, error) {
	resolved, err := new(assistantidentity.Service).ResolveLocked(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.AssistantID, identity.TriggerID)
	if err != nil {
		return inferenceCeiling{}, fmt.Errorf("validate inference identity: %w", err)
	}
	if resolved.State != assistantidentity.Active || resolved.Identity == nil || *resolved.Identity != identity {
		return inferenceCeiling{}, assistantidentity.ErrInvalidIdentity
	}
	// Identity validation above establishes this internal workload context. Use
	// live admission directly: taking a ceiling must not require a token signer.
	ac := new(contextvalues.AuthContext)
	ac.ActiveOrganizationID = identity.OrganizationID
	ac.ProjectID = &identity.ProjectID
	authed := contextvalues.WithPrincipalCredentialAuthorization(ctx, ac,
		urn.NewWorkloadPrincipal(identity.IssuerID, identity.Subject), contextvalues.PrincipalCredential{
			AuthorizerUserID: "", DelegatedGrants: nil, DelegatedGrantsVersion: 0,
			DerivesPolicyAtAdmission: true,
		})
	admission, err := runtimepolicy.AdmitWorkloadSessionInTx(authed, tx)
	if err != nil {
		return inferenceCeiling{}, fmt.Errorf("admit inference workload: %w", err)
	}
	if admission.AgentPrincipal != urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID.String()).String() {
		return inferenceCeiling{}, oops.C(oops.CodeUnauthorized)
	}
	grants, err := runtimepolicy.DelegableGrantsWithExclusions(admission.Agent, admission.Agent, admission.Agent)
	if err != nil {
		return inferenceCeiling{}, fmt.Errorf("derive inference ceiling grants: %w", err)
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	policy, err := runtimepolicy.NewDelegatedPolicy(version, grants)
	if err != nil {
		return inferenceCeiling{}, fmt.Errorf("build inference ceiling policy: %w", err)
	}
	raw, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return inferenceCeiling{}, fmt.Errorf("encode inference ceiling policy: %w", err)
	}
	return inferenceCeiling{Policy: raw, EncodingVersion: version}, nil
}

type inferenceCeiling struct {
	Policy          []byte
	EncodingVersion runtimepolicy.DelegatedPolicyVersion
}
