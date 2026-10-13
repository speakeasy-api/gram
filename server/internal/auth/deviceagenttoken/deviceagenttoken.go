// Package deviceagenttoken issues and authenticates the bearer tokens a
// headless device agent uses on the device-agent API: polling its plugin
// policy and sending hooks. A workload exchanges an identity assertion for one
// at the device-agent token endpoint, and presents it instead of an agent API
// key.
//
// A token names the workload principal and its organization, and nothing
// else. It is not a session: there is no row to revoke. Every request admits
// the workload afresh, through its assigned agent's live policy, so
// withdrawing the admission or the assignment, or suspending the agent, takes
// effect on the next request; the token's own lifetime only bounds a leaked
// copy.
package deviceagenttoken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agents"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

const (
	// TokenType is the typ header that tells device-agent tokens apart from
	// other tokens signed by the platform key.
	TokenType = "speakeasy-device-agent+jwt"

	// Audience is the only audience a device-agent token is valid for.
	Audience = "urn:speakeasy:device-agent"

	// Lifetime is how long a device-agent token authenticates. It matches the
	// workload sessions minted for MCP servers, so a device re-exchanges both
	// on the same cadence.
	Lifetime = 15 * time.Minute

	// TokenPathPattern is the route of the device-agent token endpoint,
	// relative to the server URL. Each organization has its own, so the tenant
	// comes from the path rather than from an assertion's issuer: a platform
	// issuer's URL names no organization.
	TokenPathPattern = tokenPathPrefix + "{" + TokenPathOrganizationParam + "}" + tokenPathSuffix

	// TokenPathOrganizationParam names the route segment holding the
	// organization ID.
	TokenPathOrganizationParam = "organization_id"

	tokenPathPrefix = "/oauth/workload/device-agent/" //nolint:gosec // G101: an endpoint path, not a credential
	tokenPathSuffix = "/token"
)

var (
	// ErrNotToken reports a token that is not a device-agent token.
	ErrNotToken = errors.New("not a device-agent token")

	// ErrInvalid reports a device-agent token that is malformed, forged,
	// expired, or names an organization that no longer accepts it.
	ErrInvalid = errors.New("invalid device-agent token")
)

type claims struct {
	OrganizationID string `json:"org_id"`
	jwt.RegisteredClaims
}

// Issuer signs device-agent tokens with the platform key and authenticates
// them against live tenant state.
type Issuer struct {
	signer *mcpauthz.Issuer
	db     *pgxpool.Pool
}

// New returns an Issuer, or nil when there is no platform signer, in which
// case the token endpoint refuses every exchange and no request authenticates
// with one.
func New(signer *mcpauthz.Issuer, db *pgxpool.Pool) *Issuer {
	if signer == nil {
		return nil
	}
	return &Issuer{signer: signer, db: db}
}

// TokenPath is organizationID's device-agent token endpoint, relative to the
// server URL.
func TokenPath(organizationID string) string {
	return tokenPathPrefix + organizationID + tokenPathSuffix
}

// TokenURL is organizationID's absolute device-agent token endpoint on
// serverURL. An assertion exchanged there must name this URL as its audience.
func TokenURL(serverURL, organizationID string) string {
	return strings.TrimSuffix(serverURL, "/") + TokenPath(organizationID)
}

// Ceiling is the immutable policy every device-agent token acts under in
// organizationID: polling the device policy, sending hooks, and reading
// projects. Admission intersects it with the assigned agent's live policy, so
// it grants nothing the agent lacks.
func Ceiling(organizationID string) []authz.Grant {
	return []authz.Grant{
		authz.NewGrant(authz.ScopeOrgDeviceAgentSync, organizationID),
		authz.NewGrant(authz.ScopeOrgHooksIngest, organizationID),
		authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource),
	}
}

// EncodeCeiling encodes Ceiling with the current delegated policy version.
func EncodeCeiling(organizationID string) ([]byte, int32, error) {
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	policy, err := runtimepolicy.NewDelegatedPolicy(version, Ceiling(organizationID))
	if err != nil {
		return nil, 0, fmt.Errorf("build device-agent token policy: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return nil, 0, fmt.Errorf("encode device-agent token policy: %w", err)
	}
	return encoded, int32(version), nil
}

// Mint signs a token for workload in organizationID and returns it with its
// expiry.
func (i *Issuer) Mint(organizationID string, workload urn.Principal, now time.Time) (string, time.Time, error) {
	if organizationID == "" || workload.Type != urn.PrincipalTypeWorkload {
		return "", time.Time{}, ErrInvalid
	}
	if _, _, err := workload.Workload(); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	expiresAt := now.Add(Lifetime)
	raw, err := i.signer.SignRS256(claims{
		OrganizationID: organizationID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: i.signer.URL(), Subject: workload.String(), Audience: jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(expiresAt), NotBefore: nil, IssuedAt: jwt.NewNumericDate(now), ID: uuid.NewString(),
		},
	}, TokenType)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign device-agent token: %w", err)
	}
	return raw, expiresAt, nil
}

// IsToken reports whether raw is shaped as a device-agent token, without
// verifying it.
func IsToken(raw string) bool {
	token, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		return false
	}
	return token.Header["typ"] == TokenType
}

// Authenticate verifies raw and returns a context authenticated as its
// workload, carrying the token's ceiling as its credential policy. The
// organization must still exist and not be disabled. Admission is the
// caller's: authz.PrepareContext routes a workload actor to workload
// admission, which resolves its assigned agent's live policy.
func (i *Issuer) Authenticate(ctx context.Context, raw string) (context.Context, error) {
	if !IsToken(raw) {
		return ctx, ErrNotToken
	}
	var c claims
	if err := i.signer.ParseRS256(raw, &c, TokenType, Audience); err != nil {
		return ctx, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(c.Audience) != 1 || c.ID == "" || c.OrganizationID == "" {
		return ctx, ErrInvalid
	}
	principal, err := urn.ParsePrincipal(c.Subject)
	if err != nil || principal.Type != urn.PrincipalTypeWorkload {
		return ctx, ErrInvalid
	}
	if _, _, err := principal.Workload(); err != nil {
		return ctx, ErrInvalid
	}

	org, err := orgrepo.New(i.db).GetOrganizationMetadata(ctx, c.OrganizationID)
	if err != nil {
		return ctx, fmt.Errorf("load device-agent token organization: %w", err)
	}
	if org.DisabledAt.Valid {
		return ctx, fmt.Errorf("%w: organization is disabled", ErrInvalid)
	}
	ceiling, version, err := EncodeCeiling(c.OrganizationID)
	if err != nil {
		return ctx, err
	}

	ac := &contextvalues.AuthContext{
		ActiveOrganizationID: c.OrganizationID, UserID: "", ExternalUserID: "", APIKeyID: "", APIKeyName: "",
		OrgWidePluginHooksKey: false, SessionID: nil, ProjectID: nil, OrganizationSlug: org.Slug, Email: nil,
		AccountType: org.GramAccountType, HasActiveSubscription: false, Whitelisted: org.Whitelisted, ProjectSlug: nil,
		APIKeyScopes: nil, IsAdmin: false, SupportOrganizationID: "",
	}
	return contextvalues.WithPrincipalCredentialAuthorization(ctx, ac, principal, contextvalues.PrincipalCredential{
		// A workload records no approving human.
		AuthorizerUserID:       "",
		DelegatedGrants:        ceiling,
		DelegatedGrantsVersion: version,
	}), nil
}

// AssignedAgent is the agent an authenticated workload acts as.
type AssignedAgent struct {
	// Principal is the agent's canonical principal.
	Principal urn.Principal

	// Agent is the agent row, for its name and project.
	Agent agentsrepo.Agent

	// ProjectSlug is the slug of the agent's project, or empty when the agent
	// is not bound to one.
	ProjectSlug string
}

// ResolveAssignedAgent returns the agent the authenticated workload in ctx is
// assigned to. Call it after admission, which has already refused a workload
// whose agent is missing, suspended, revoked, or awaiting an owner.
func (i *Issuer) ResolveAssignedAgent(ctx context.Context) (AssignedAgent, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if !ok || authCtx == nil || !hasActor || actor.Type != urn.PrincipalTypeWorkload {
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	}
	workloadIssuerID, subject, err := actor.Workload()
	if err != nil {
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	}

	agentID, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, i.db, workloadidentity.AssignmentParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		WorkloadIssuerID: workloadIssuerID,
		Subject:          subject,
	})
	if err != nil {
		return AssignedAgent{}, fmt.Errorf("resolve workload agent assignment: %w", err)
	}
	if !assigned {
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	}

	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String())
	agent, err := agents.ResolvePrincipal(ctx, i.db, authCtx.ActiveOrganizationID, principal)
	switch {
	case errors.Is(err, agents.ErrPrincipalInvalid), errors.Is(err, agents.ErrPrincipalNotFound):
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	case err != nil:
		return AssignedAgent{}, fmt.Errorf("resolve assigned agent: %w", err)
	}

	assignedAgent := AssignedAgent{Principal: principal, Agent: agent, ProjectSlug: ""}
	if agent.ProjectID.Valid {
		project, err := projectsrepo.New(i.db).GetProjectByID(ctx, agent.ProjectID.UUID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// A deleted project leaves its agent's devices nowhere to send
			// to, which is a refusal, not a server failure.
			return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
		case err != nil:
			return AssignedAgent{}, fmt.Errorf("load assigned agent project: %w", err)
		}
		assignedAgent.ProjectSlug = project.Slug
	}
	return assignedAgent, nil
}

// Admit authenticates raw, admits its workload through engine, and resolves
// the agent the workload is assigned to. The returned context carries the
// workload as actor and its admitted policy, and records the assigned agent
// for AssignedAgentFromContext.
//
// A malformed, forged, or expired token, a disabled organization, and a
// workload that admission refuses are all CodeUnauthorized, so the device
// exchanges again; a failure that reached no decision is CodeUnexpected.
func (i *Issuer) Admit(ctx context.Context, raw string, engine *authz.Engine) (_ context.Context, _ AssignedAgent, err error) {
	defer func() {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeUnauthorized {
			MarkInvalidToken(ctx)
		}
	}()
	authenticated, err := i.Authenticate(ctx, raw)
	switch {
	case errors.Is(err, ErrNotToken), errors.Is(err, ErrInvalid):
		return ctx, AssignedAgent{}, oops.E(oops.CodeUnauthorized, err, "invalid device-agent token")
	case err != nil:
		return ctx, AssignedAgent{}, oops.E(oops.CodeUnexpected, err, "authenticate device-agent token")
	}

	admitted, err := engine.PrepareContext(authenticated)
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code != oops.CodeUnexpected {
			return ctx, AssignedAgent{}, oops.E(oops.CodeUnauthorized, err, "device-agent token is no longer admitted")
		}
		return ctx, AssignedAgent{}, oops.E(oops.CodeUnexpected, err, "admit device-agent token")
	}

	agent, err := i.ResolveAssignedAgent(admitted)
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code != oops.CodeUnexpected {
			return ctx, AssignedAgent{}, err
		}
		return ctx, AssignedAgent{}, oops.E(oops.CodeUnexpected, err, "resolve device-agent token agent")
	}
	return context.WithValue(admitted, assignedAgentKey{}, agent), agent, nil
}

type assignedAgentKey struct{}

// AssignedAgentFromContext returns the agent Admit resolved for the request.
func AssignedAgentFromContext(ctx context.Context) (AssignedAgent, bool) {
	agent, ok := ctx.Value(assignedAgentKey{}).(AssignedAgent)
	return agent, ok
}

// EffectiveAgent returns the agent a request acts for: the actor itself when an
// agent key authenticated it, or the agent Admit resolved when a workload did.
// The workload stays the authenticated actor, so audit and logs name the device;
// the agent supplies policy, plugin selection, attribution and spend.
func EffectiveAgent(ctx context.Context) (agent urn.Principal, ok bool) {
	actor, found := contextvalues.AuthenticatedActor(ctx)
	switch {
	case !found:
	case actor.Type == urn.PrincipalTypeAgent:
		agent, ok = actor, true
	case actor.Type == urn.PrincipalTypeWorkload:
		if assigned, found := AssignedAgentFromContext(ctx); found {
			agent, ok = assigned.Principal, true
		}
	}
	return agent, ok
}

// IsWorkloadActor reports whether a workload authenticated the request.
func IsWorkloadActor(ctx context.Context) bool {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && actor.Type == urn.PrincipalTypeWorkload
}

type authFailureKey struct{}

// WithAuthFailure records why a device-agent token was refused. Goa tries a
// method's security requirements in order and hands each the context the last
// one returned, so a later API-key requirement seeing no key can report this
// instead of a missing key.
func WithAuthFailure(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, authFailureKey{}, err)
}

// AuthFailure returns the refusal WithAuthFailure recorded, or nil.
func AuthFailure(ctx context.Context) error {
	err, _ := ctx.Value(authFailureKey{}).(error)
	return err
}

// BearerChallenge adds `WWW-Authenticate: Bearer` to a 401 answering a request
// that presented a device-agent token (RFC 6750 §3). The challenge carries
// `error="invalid_token"`, telling the device agent to exchange for a fresh
// token, only when Admit refused the token itself; a 401 for any other reason,
// such as a deployment that accepts no device-agent tokens, gets a plain
// Bearer challenge. Requests presenting anything else are answered unchanged.
//
// Wrap only the handlers that accept device-agent tokens.
func BearerChallenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || !IsToken(token) {
			next.ServeHTTP(w, r)
			return
		}
		state := &challengeState{invalidToken: false}
		next.ServeHTTP(&challengeWriter{ResponseWriter: w, state: state}, r.WithContext(context.WithValue(r.Context(), challengeStateKey{}, state)))
	})
}

type challengeStateKey struct{}

// challengeState is shared between BearerChallenge and the Admit call that
// runs while it serves the request.
type challengeState struct {
	invalidToken bool
}

// MarkInvalidToken records, for BearerChallenge serving the request, that the
// presented token itself was refused.
func MarkInvalidToken(ctx context.Context) {
	if state, ok := ctx.Value(challengeStateKey{}).(*challengeState); ok {
		state.invalidToken = true
	}
}

type challengeWriter struct {
	http.ResponseWriter
	state *challengeState
}

func (c *challengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized && c.Header().Get("WWW-Authenticate") == "" {
		challenge := "Bearer"
		if c.state.invalidToken {
			challenge = `Bearer error="invalid_token"`
		}
		c.Header().Set("WWW-Authenticate", challenge)
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *challengeWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
