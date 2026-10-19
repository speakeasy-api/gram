// Package deviceagenttoken issues and authenticates the bearer tokens headless
// devices use on the device-agent API. A token is not a session: every request
// re-admits the workload, so revocation applies on the next request.
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
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	// TokenPathPattern is the per-organization token endpoint route. The tenant
	// comes from the path because a platform issuer's URL names no organization.
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

// Ceiling is the most a device-agent token may do: poll policy, read projects,
// and send hooks to any project in organizationID. Admission intersects it with
// the agent's live policy, so the agent's own org:hooks_ingest grants decide
// which projects a hook may name.
func Ceiling(organizationID string) []authz.Grant {
	return []authz.Grant{
		authz.NewGrant(authz.ScopeOrgDeviceAgentSync, organizationID),
		authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeOrgHooksIngest, organizationID),
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
	// A typ that is present but not a string is treated as this token too, so
	// it reaches the validator and is refused there rather than passing for
	// some other credential.
	typ, present := token.Header["typ"]
	_, isString := typ.(string)
	return (present && !isString) || typ == TokenType
}

// Authenticate verifies raw and returns a context authenticated as its workload,
// bounded by the organization's Ceiling. Admission is left to authz.PrepareContext.
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
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, fmt.Errorf("%w: organization not found", ErrInvalid)
	}
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
		// The ceiling is built from Ceiling on each request and supplied as
		// the delegated policy, so admission derives none.
		DerivesPolicyAtAdmission: false,
	}), nil
}

// AssignedAgent is the agent an authenticated workload acts as.
type AssignedAgent struct {
	// Principal is the agent's canonical principal.
	Principal urn.Principal

	// Agent is the agent row, for its name.
	Agent agentsrepo.Agent
}

// admittedAgent returns the agent admission recorded, never re-resolving the
// assignment, so a mid-request reassignment cannot mix two agents.
func (i *Issuer) admittedAgent(ctx context.Context) (AssignedAgent, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !IsWorkloadActor(ctx) {
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	}
	principal, ok := contextvalues.EffectiveAgent(ctx)
	if !ok {
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	}
	agent, err := agents.ResolvePrincipal(ctx, i.db, authCtx.ActiveOrganizationID, principal)
	switch {
	case errors.Is(err, agents.ErrPrincipalInvalid), errors.Is(err, agents.ErrPrincipalNotFound):
		return AssignedAgent{}, oops.C(oops.CodeUnauthorized)
	case err != nil:
		return AssignedAgent{}, fmt.Errorf("resolve admitted agent: %w", err)
	}
	return AssignedAgent{Principal: principal, Agent: agent}, nil
}

// Admit authenticates raw and admits its workload, keeping the workload as actor
// and recording its agent. Refusals are CodeUnauthorized so the device exchanges
// again; a failure that reached no decision is CodeUnexpected.
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

	agent, err := i.admittedAgent(admitted)
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

// EffectiveAgent returns the agent a request acts for: an agent actor itself, or
// the agent Admit recorded for a workload, which stays the authenticated actor.
func EffectiveAgent(ctx context.Context) (urn.Principal, bool) {
	return contextvalues.EffectiveAgent(ctx)
}

// IsWorkloadActor reports whether a workload authenticated the request.
func IsWorkloadActor(ctx context.Context) bool {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && actor.Type == urn.PrincipalTypeWorkload
}

type authFailureKey struct{}

// WithAuthFailure records why a device-agent token was refused, so the API-key
// scheme Goa tries next can report it instead of a missing key.
func WithAuthFailure(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, authFailureKey{}, err)
}

// AuthFailure returns the refusal WithAuthFailure recorded, or nil.
func AuthFailure(ctx context.Context) error {
	err, _ := ctx.Value(authFailureKey{}).(error)
	return err
}

// BearerChallenge adds `WWW-Authenticate: Bearer` to 401s for device-agent
// tokens (RFC 6750 §3), with `error="invalid_token"` only when Admit refused the
// token, which tells the device to exchange again.
func BearerChallenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any bearer presented here is meant for this route, so a refusal of a
		// mis-shaped or non-device-agent bearer is challenged as well.
		scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
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
