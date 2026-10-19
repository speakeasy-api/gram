package assistanttokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const inferencePrefix = "sai_"

// MaxInferenceLifetime bounds leaked startup credentials even if explicit revocation fails.
const MaxInferenceLifetime = 30 * 24 * time.Hour

type InferenceIssueInput struct {
	Identity  assistantidentity.Identity
	RuntimeID uuid.UUID
	Lifetime  time.Duration
}
type InferenceCredential struct {
	ID        uuid.UUID
	Token     string
	ExpiresAt time.Time
}
type inferenceContextKey struct{}
type inferenceRuntimeContextKey struct{}

// IsInferenceCredential recognizes only the dedicated opaque startup token namespace.
func IsInferenceCredential(header string) bool {
	return strings.HasPrefix(bearerToken(header), inferencePrefix)
}

// InferenceTranscriptOwned reports a trusted runtime whose ACP transcript owns persistence.
func InferenceTranscriptOwned(ctx context.Context) bool {
	_, ok := ctx.Value(inferenceContextKey{}).(assistantidentity.Identity)
	return ok
}

// IssueInferenceCredential issues a startup-only bearer for an existing root workload
// binding. It does not register identities or delegate an initiating human's authority.
func (m *Manager) IssueInferenceCredential(ctx context.Context, in InferenceIssueInput) (InferenceCredential, error) {
	if in.Lifetime <= 0 || in.Lifetime > MaxInferenceLifetime || in.RuntimeID == uuid.Nil {
		return InferenceCredential{}, fmt.Errorf("invalid inference credential lifetime or runtime")
	}
	ceiling, err := m.snapshotInferenceCeiling(ctx, in.Identity)
	if err != nil {
		return InferenceCredential{}, fmt.Errorf("snapshot inference credential ceiling: %w", err)
	}
	identity, err := json.Marshal(in.Identity)
	if err != nil {
		return InferenceCredential{}, fmt.Errorf("encode inference identity: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return InferenceCredential{}, fmt.Errorf("generate inference credential secret: %w", err)
	}
	token := inferencePrefix + base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().Add(in.Lifetime)
	id, err := m.tokens.IssueInferenceCredential(ctx, tokenrepo.IssueInferenceCredentialParams{ProjectID: in.Identity.ProjectID, AssistantID: in.Identity.AssistantID, RuntimeID: in.RuntimeID, TokenHash: hash[:], IdentityJson: identity, PolicyJson: ceiling.Policy, PolicyVersion: int32(ceiling.EncodingVersion), ExpiresAt: pgtype.Timestamptz{Time: expires, InfinityModifier: pgtype.Finite, Valid: true}})
	if err != nil {
		return InferenceCredential{}, fmt.Errorf("persist inference credential: %w", err)
	}
	return InferenceCredential{ID: id, Token: token, ExpiresAt: expires}, nil
}

func (m *Manager) RevokeInferenceCredential(ctx context.Context, projectID, credentialID uuid.UUID) error {
	if err := m.tokens.RevokeInferenceCredential(ctx, tokenrepo.RevokeInferenceCredentialParams{ProjectID: projectID, ID: credentialID}); err != nil {
		return fmt.Errorf("revoke inference credential: %w", err)
	}
	return nil
}

// AuthorizeInference authenticates only inference routes, including catalog reads.
// Completion handlers must call BindInferenceChat before inference.
// Never expose the short-lived internal admission assertion to the runtime.
func (m *Manager) AuthorizeInference(ctx context.Context, header string) (context.Context, error) {
	if !IsInferenceCredential(header) {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	hash := sha256.Sum256([]byte(bearerToken(header)))
	row, err := m.tokens.ReadInferenceCredential(ctx, hash[:])
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	var identity assistantidentity.Identity
	if err := json.Unmarshal(row.IdentityJson, &identity); err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if identity.ProjectID != row.ProjectID || identity.AssistantID != row.AssistantID {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if err := new(assistantidentity.Service).Validate(ctx, m.db, identity); err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	_, err = runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(row.PolicyVersion), row.PolicyJson)
	if errors.Is(err, runtimepolicy.ErrInvalidDelegatedPolicy) {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return ctx, fmt.Errorf("decode inference policy: %w", err)
	}
	// Reuse the ordinary workload credential profile and live policy admission.
	assertion, _, err := m.credentials.Mint(principalcredential.Credential{OrganizationID: identity.OrganizationID, ProjectID: identity.ProjectID, Principal: urn.NewWorkloadPrincipal(identity.IssuerID, identity.Subject), AuthorizerUserID: ""})
	if err != nil {
		return ctx, fmt.Errorf("mint inference assertion: %w", err)
	}
	authed, err := m.credentials.Authenticate(ctx, assertion)
	if err != nil {
		return ctx, fmt.Errorf("authenticate inference assertion: %w", err)
	}
	// Principal assertions now derive live policy. This persisted runtime bearer
	// must instead retain its issuance ceiling, intersected with live authority.
	ac, ok := contextvalues.GetAuthContext(authed)
	if !ok || ac == nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	authed = contextvalues.WithPrincipalCredentialAuthorization(authed, ac,
		urn.NewWorkloadPrincipal(identity.IssuerID, identity.Subject), contextvalues.PrincipalCredential{
			DelegatedGrants: row.PolicyJson, DelegatedGrantsVersion: row.PolicyVersion,
			DerivesPolicyAtAdmission: false, AuthorizerUserID: "",
		})
	authed, err = m.authz.PrepareContext(authed)
	if err != nil {
		return ctx, fmt.Errorf("prepare inference authorization: %w", err)
	}
	authed = context.WithValue(authed, inferenceRuntimeContextKey{}, row.RuntimeID)
	return context.WithValue(authed, inferenceContextKey{}, identity), nil
}

// BindInferenceChat requires an existing assistant-owned thread/chat. ACP session
// UUIDs cannot manufacture arbitrary captured conversations through inference.
func (m *Manager) BindInferenceChat(ctx context.Context, chatID uuid.UUID) (context.Context, error) {
	identity, ok := ctx.Value(inferenceContextKey{}).(assistantidentity.Identity)
	if !ok || chatID == uuid.Nil {
		return ctx, oops.C(oops.CodeForbidden)
	}
	thread, err := m.tokens.InferenceChatThread(ctx, tokenrepo.InferenceChatThreadParams{ProjectID: identity.ProjectID, AssistantID: identity.AssistantID, ChatID: chatID})
	if err != nil {
		return ctx, oops.C(oops.CodeForbidden)
	}
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: identity.AssistantID, ThreadID: thread})
	return context.WithValue(ctx, runtimeChatKey{}, chatID), nil
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

// snapshotInferenceCeiling keeps startup credentials bounded by the agent's
// issuance-time policy even though ordinary principal assertions derive it live.
func (m *Manager) snapshotInferenceCeiling(ctx context.Context, identity assistantidentity.Identity) (inferenceCeiling, error) {
	if err := new(assistantidentity.Service).Validate(ctx, m.db, identity); err != nil {
		return inferenceCeiling{}, fmt.Errorf("validate inference identity: %w", err)
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
	admission, err := runtimepolicy.AdmitWorkloadSession(authed, m.db)
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
