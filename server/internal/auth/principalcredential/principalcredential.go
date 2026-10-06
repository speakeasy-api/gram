// Package principalcredential issues and authenticates short-lived bearer
// tokens for an agent or a workload principal. A token carries the principal,
// the organization and project it acts in, an optional authorizing user for an
// agent, and the delegated grants it is bounded by. Authenticating one yields
// the same principal credential context an agent API key produces, so every
// surface authorizes it through the ordinary credential and workload admission
// paths.
package principalcredential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// TokenType is the typ header that tells principal credentials apart from
	// other tokens signed by the platform key.
	TokenType = "gram-principal-credential+jwt"

	// Audience is the only audience a principal credential is valid for.
	Audience = "urn:gram:principal-credential"

	// Lifetime bounds how long a principal credential authenticates. It covers
	// a queued unit of work and the model and tool calls it makes.
	Lifetime = 60 * time.Minute
)

var (
	// ErrNotCredential reports a token that is not a principal credential, so
	// callers can try their other authentication strategies.
	ErrNotCredential = errors.New("not a principal credential")

	// ErrInvalid reports a principal credential that is malformed, forged,
	// expired, or names a tenant that no longer matches.
	ErrInvalid = errors.New("invalid principal credential")
)

// Credential is what a principal credential asserts.
type Credential struct {
	OrganizationID string
	ProjectID      uuid.UUID
	// Principal is an agent or a workload principal.
	Principal urn.Principal
	// AuthorizerUserID is the user an agent acts for. Agents require one;
	// workloads never carry one.
	AuthorizerUserID string
	// Grants bound everything the credential can authorize.
	Grants []authz.Grant
}

// Authenticated is a verified credential, its token ID, and its delegated
// policy as signed.
type Authenticated struct {
	ID                     string
	Credential             Credential
	DelegatedGrants        json.RawMessage
	DelegatedGrantsVersion int32
}

type claims struct {
	OrganizationID         string          `json:"org_id"`
	ProjectID              uuid.UUID       `json:"project_id"`
	AuthorizerUserID       string          `json:"authorizer_user_id,omitempty"`
	DelegatedGrants        json.RawMessage `json:"delegated_grants"`
	DelegatedGrantsVersion int32           `json:"delegated_grants_version"`
	jwt.RegisteredClaims
}

// Issuer signs principal credentials with the platform key and authenticates
// them against live tenant state.
type Issuer struct {
	signer *mcpauthz.Issuer
	db     *pgxpool.Pool
}

func New(signer *mcpauthz.Issuer, db *pgxpool.Pool) *Issuer {
	return &Issuer{signer: signer, db: db}
}

func checkShape(c Credential) error {
	if c.OrganizationID == "" || c.ProjectID == uuid.Nil {
		return ErrInvalid
	}
	switch c.Principal.Type {
	case urn.PrincipalTypeAgent:
		if _, err := uuid.Parse(c.Principal.ID); err != nil || c.AuthorizerUserID == "" {
			return ErrInvalid
		}
	case urn.PrincipalTypeWorkload:
		if _, _, err := c.Principal.Workload(); err != nil || c.AuthorizerUserID != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Mint signs c, encoding its grants with the current delegated policy version.
// It returns the token and its ID.
func (i *Issuer) Mint(c Credential) (string, string, error) {
	if err := checkShape(c); err != nil {
		return "", "", err
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	policy, err := runtimepolicy.NewDelegatedPolicy(version, c.Grants)
	if err != nil {
		return "", "", fmt.Errorf("build principal credential policy: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return "", "", fmt.Errorf("encode principal credential policy: %w", err)
	}
	now := time.Now()
	id := uuid.NewString()
	raw, err := i.signer.SignRS256(claims{
		OrganizationID: c.OrganizationID, ProjectID: c.ProjectID, AuthorizerUserID: c.AuthorizerUserID,
		DelegatedGrants: encoded, DelegatedGrantsVersion: int32(version),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: i.signer.URL(), Subject: c.Principal.String(), Audience: jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(Lifetime)), NotBefore: nil, IssuedAt: jwt.NewNumericDate(now), ID: id,
		},
	}, TokenType)
	if err != nil {
		return "", "", fmt.Errorf("sign principal credential: %w", err)
	}
	return raw, id, nil
}

// IsToken reports whether raw is shaped as a principal credential, without
// verifying it. A token whose typ header is present but not a string is
// treated as one so it can never reach a validator that ignores typ.
func IsToken(raw string) bool {
	token, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		return false
	}
	typ, present := token.Header["typ"]
	_, isString := typ.(string)
	return (present && !isString) || typ == TokenType
}

// Validate verifies raw's signature and shape. It reports ErrNotCredential for
// any other kind of token.
func (i *Issuer) Validate(raw string) (Authenticated, error) {
	if !IsToken(raw) {
		return Authenticated{}, ErrNotCredential
	}
	var c claims
	if err := i.signer.ParseRS256(raw, &c, TokenType, Audience); err != nil {
		return Authenticated{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(c.Audience) != 1 || c.ID == "" {
		return Authenticated{}, ErrInvalid
	}
	principal, err := urn.ParsePrincipal(c.Subject)
	if err != nil {
		return Authenticated{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	version := runtimepolicy.DelegatedPolicyVersion(c.DelegatedGrantsVersion)
	policy, err := runtimepolicy.DecodeDelegatedPolicy(version, c.DelegatedGrants)
	if err != nil {
		return Authenticated{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	credential := Credential{OrganizationID: c.OrganizationID, ProjectID: c.ProjectID, Principal: principal, AuthorizerUserID: c.AuthorizerUserID, Grants: policy.RuntimeGrants()}
	if err := checkShape(credential); err != nil {
		return Authenticated{}, err
	}
	return Authenticated{ID: c.ID, Credential: credential, DelegatedGrants: c.DelegatedGrants, DelegatedGrantsVersion: c.DelegatedGrantsVersion}, nil
}

type authenticatedKey struct{}

// FromContext returns the principal credential a request authenticated with.
func FromContext(ctx context.Context) (Authenticated, bool) {
	a, ok := ctx.Value(authenticatedKey{}).(Authenticated)
	return a, ok
}

// Authenticate verifies raw and returns a context authenticated as its
// principal: the principal credential profile and actor an agent API key
// produces, scoped to the credential's project. Admission happens later
// through authz.PrepareContext like any other principal credential. A request
// that already authenticated with a principal credential is returned as is.
func (i *Issuer) Authenticate(ctx context.Context, raw string) (context.Context, error) {
	if _, ok := FromContext(ctx); ok {
		return ctx, nil
	}
	authenticated, err := i.Validate(raw)
	if err != nil {
		return ctx, err
	}
	c := authenticated.Credential
	project, err := projectsrepo.New(i.db).GetProjectByID(ctx, c.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, ErrInvalid
	}
	if err != nil {
		return ctx, fmt.Errorf("load principal credential project: %w", err)
	}
	if project.OrganizationID != c.OrganizationID {
		return ctx, ErrInvalid
	}
	org, err := orgrepo.New(i.db).GetOrganizationMetadata(ctx, c.OrganizationID)
	if err != nil {
		return ctx, fmt.Errorf("load principal credential organization: %w", err)
	}
	ac := &contextvalues.AuthContext{
		ActiveOrganizationID: c.OrganizationID, UserID: "", ExternalUserID: "", APIKeyID: "", APIKeyName: "",
		OrgWidePluginHooksKey: false, SessionID: nil, ProjectID: &project.ID, OrganizationSlug: org.Slug, Email: nil,
		AccountType: org.GramAccountType, HasActiveSubscription: false, Whitelisted: org.Whitelisted, ProjectSlug: &project.Slug,
		APIKeyScopes: nil, IsAdmin: false, SupportOrganizationID: "",
	}
	ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, ac, c.Principal, contextvalues.PrincipalCredential{
		AuthorizerUserID: c.AuthorizerUserID, DelegatedGrants: authenticated.DelegatedGrants, DelegatedGrantsVersion: authenticated.DelegatedGrantsVersion,
	})
	return context.WithValue(ctx, authenticatedKey{}, authenticated), nil
}
