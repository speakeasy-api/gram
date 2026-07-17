// Package cliauth implements the cliAuth Goa service: the device agent's
// interactive enrollment via a PKCE one-time-code exchange (DNO-388).
//
//   - Authorize runs under a dashboard session (member-available, org:read —
//     NOT org-admin). It mints a short-lived opaque code bound to a PKCE
//     code_challenge and stashes the resolved {user, org, project, scopes,
//     challenge, email, slug} against it in Redis with a ~5 minute TTL.
//   - Redeem takes no session or API-key auth: knowledge of the code_verifier
//     matching the stored challenge IS the credential. The code is single-use,
//     consumed atomically via GETDEL on lookup, so a replayed/expired/unknown
//     code or a PKCE mismatch fails closed with 401. On success it mints a
//     per-user `agent_user` API key and returns the raw key exactly once.
package cliauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/cli_auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/cli_auth/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const (
	// codeTTL is the lifetime of an authorize-minted one-time code. Short by
	// design: the dashboard hands the code to the device agent immediately, so
	// there is no reason to leave a live credential-exchange window open.
	codeTTL = 5 * time.Minute

	// codeKeyNamespace prefixes the Redis keys backing the one-time-code store
	// so they never collide with the other Redis consumers sharing the client.
	codeKeyNamespace = "cliauth:code:"

	// deviceAgentKeyName is the name stamped on the API key minted by redeem, so
	// it is identifiable in the org's key list.
	deviceAgentKeyName = "device-agent"

	// pkceMethodS256 is the only PKCE challenge method this flow accepts.
	pkceMethodS256 = "S256"
)

// codeRecord is the JSON payload stored in Redis against a one-time code. It
// captures everything redeem needs so the redemption path touches no session
// and re-resolves nothing that could have drifted since authorize.
type codeRecord struct {
	UserID              string   `json:"user_id"`
	OrgID               string   `json:"org_id"`
	ProjectID           string   `json:"project_id"`
	ProjectSlug         string   `json:"project_slug"`
	UserEmail           string   `json:"user_email"`
	Scopes              []string `json:"scopes"`
	CodeChallenge       string   `json:"code_challenge"`
	CodeChallengeMethod string   `json:"code_challenge_method"`
}

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	pkce        *oauth.PKCEService
	redis       *redis.Client
	projectRepo *projectsrepo.Queries
	keysRepo    *keysrepo.Queries
	keyPrefix   string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// NewService wires the cliAuth service. It reuses the shared session manager
// (for the Authorize session Auther), authz engine, Redis client (the SAME one
// the session/cache layer uses), and the DB pool. env selects the API-key
// prefix (gram_local_ / gram_test_ / gram_live_) for the key redeem mints.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	redisClient *redis.Client,
	env string,
) *Service {
	logger = logger.With(attr.SlogComponent("cliauth"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/cliauth"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessionManager, authzEngine),
		authz:       authzEngine,
		pkce:        oauth.NewPKCEService(logger),
		redis:       redisClient,
		projectRepo: projectsrepo.New(db),
		keysRepo:    keysrepo.New(db),
		keyPrefix:   auth.APIKeyPrefix(env),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// APIKeyAuth backs the Session security scheme on Authorize (redeem is
// NoSecurity). Delegates to the same auth path userSessions.mint uses.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// Authorize mints a one-time code bound to the caller's PKCE challenge. It
// never fabricates identity: the user, org, and email come straight off the
// authenticated session's auth context.
func (s *Service) Authorize(ctx context.Context, payload *gen.AuthorizePayload) (*gen.AuthorizeResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.Email == nil || *authCtx.Email == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "session has no resolved email").LogError(ctx, s.logger)
	}

	// Member-available gate: any org member (org:read) may enroll their own
	// device agent. Intentionally NOT org:admin — enrollment is self-service.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if payload.CodeChallengeMethod != pkceMethodS256 {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported code_challenge_method: only S256 is supported").LogError(ctx, s.logger)
	}
	if err := s.pkce.ValidateCodeChallenge(ctx, payload.CodeChallenge, pkceMethodS256); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid code_challenge").LogError(ctx, s.logger)
	}

	project, err := s.resolveProject(ctx, authCtx.ActiveOrganizationID, payload.ProjectSlug)
	if err != nil {
		return nil, err
	}

	code, err := generateOpaqueToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate one-time code").LogError(ctx, s.logger)
	}

	record := codeRecord{
		UserID:              authCtx.UserID,
		OrgID:               authCtx.ActiveOrganizationID,
		ProjectID:           project.ID.String(),
		ProjectSlug:         project.Slug,
		UserEmail:           *authCtx.Email,
		Scopes:              []string{auth.APIKeyScopeAgentUser.String()},
		CodeChallenge:       payload.CodeChallenge,
		CodeChallengeMethod: pkceMethodS256,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode code record").LogError(ctx, s.logger)
	}

	// NX guards against the astronomically unlikely code collision; a false
	// result means the random code already exists, which we treat as an error
	// rather than clobbering a live code.
	stored, err := s.redis.SetNX(ctx, codeKeyNamespace+code, raw, codeTTL).Result()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store one-time code").LogError(ctx, s.logger)
	}
	if !stored {
		return nil, oops.E(oops.CodeUnexpected, nil, "one-time code collision").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "minted cliauth one-time code",
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserID(authCtx.UserID),
		attr.SlogProjectID(project.ID.String()),
	)

	return &gen.AuthorizeResult{
		Code:      code,
		ExpiresIn: int(codeTTL.Seconds()),
	}, nil
}

// Redeem consumes a one-time code and mints the per-user `agent_user` API key.
// Every failure path returns 401 so a caller learns nothing beyond "no": the
// code+verifier pair either works or it does not.
func (s *Service) Redeem(ctx context.Context, payload *gen.RedeemPayload) (*gen.RedeemResult, error) {
	if payload.Code == "" || payload.CodeVerifier == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// GETDEL makes consumption atomic and single-use: the record is removed on
	// the same round-trip that reads it, so a replay (or a racing second
	// redeem) sees redis.Nil. A wrong verifier still burns the code — that is
	// the intended fail-closed posture, and it stops verifier brute-forcing.
	raw, err := s.redis.GetDel(ctx, codeKeyNamespace+payload.Code).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return nil, oops.C(oops.CodeUnauthorized)
	case err != nil:
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized").LogError(ctx, s.logger)
	}

	var record codeRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized").LogError(ctx, s.logger)
	}

	if err := s.pkce.VerifyCodeChallenge(ctx, payload.CodeVerifier, record.CodeChallenge, record.CodeChallengeMethod); err != nil {
		// Verification already logs internally; keep the client response opaque.
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if record.UserID == "" || record.OrgID == "" || record.ProjectID == "" || len(record.Scopes) == 0 {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized").LogError(ctx, s.logger)
	}
	projectID, err := uuid.Parse(record.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized").LogError(ctx, s.logger)
	}

	rawKey, err := s.mintKey(ctx, record, projectID)
	if err != nil {
		// The code was already consumed (GETDEL above), so a mint failure is
		// unrecoverable for the caller — they must re-enroll regardless. Fail
		// closed like every other path (the internal error stays in the logs);
		// a distinct 5xx here would leak that the code+verifier was valid.
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "redeemed cliauth one-time code",
		attr.SlogOrganizationID(record.OrgID),
		attr.SlogUserID(record.UserID),
		attr.SlogProjectID(record.ProjectID),
	)

	return &gen.RedeemResult{
		AccessToken: rawKey,
		UserEmail:   record.UserEmail,
		ProjectSlug: record.ProjectSlug,
	}, nil
}

// resolveProject returns the named project (validated against the org) or, when
// no slug is given, the org's default (lowest-id) project.
func (s *Service) resolveProject(ctx context.Context, orgID string, slug *string) (*projectsrepo.Project, error) {
	if slug != nil && *slug != "" {
		project, err := s.projectRepo.GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{
			Slug:           *slug,
			OrganizationID: orgID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, err, "project not found").LogError(ctx, s.logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load project").LogError(ctx, s.logger)
		}
		return &project, nil
	}

	projects, err := s.projectRepo.ListProjectsByOrganization(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list projects").LogError(ctx, s.logger)
	}
	if len(projects) == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "organization has no projects").LogError(ctx, s.logger)
	}
	// ListProjectsByOrganization is ordered by id ASC, so the first row is the
	// org's default project.
	return &projects[0], nil
}

// mintKey assembles and persists a per-user API key via the low-level keys
// repo, returning the raw key (which is only ever seen here). Mirrors the
// key-assembly in keys.CreateKey: prefix + random token, sha256 hash stored,
// prefix + token[:5] as the display prefix.
func (s *Service) mintKey(ctx context.Context, record codeRecord, projectID uuid.UUID) (string, error) {
	token, err := generateOpaqueToken()
	if err != nil {
		return "", fmt.Errorf("generate api key token: %w", err)
	}

	fullKey := s.keyPrefix + token
	keyHash, err := auth.GetAPIKeyHash(fullKey)
	if err != nil {
		return "", fmt.Errorf("hash api key: %w", err)
	}

	// Unique per-enrollment key name. API keys are not treated as singletons:
	// each enrollment mints its own key (made unique by the random token suffix),
	// so a user's other enrolled devices keep working — no revoke-then-mint gap,
	// and no collision with the (organization_id, name) unique index. Stale keys
	// are reclaimed out of band via revocation, not by deleting a prior key here.
	keyName := deviceAgentKeyName + ":" + record.UserID + ":" + token[:8]

	// record.Scopes is set by Authorize and validated non-empty in Redeem, so it
	// is used directly here — no default, so an unexpectedly empty scope set
	// surfaces as an integrity failure upstream rather than a silently broken key.
	if _, err := s.keysRepo.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  record.OrgID,
		ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
		CreatedByUserID: record.UserID,
		Name:            keyName,
		KeyPrefix:       s.keyPrefix + token[:5],
		KeyHash:         keyHash,
		Scopes:          record.Scopes,
	}); err != nil {
		return "", fmt.Errorf("create api key: %w", err)
	}

	return fullKey, nil
}

// generateOpaqueToken returns a 64-char hex string from 32 crypto-random bytes.
// Used for both the one-time code and the API-key token body.
func generateOpaqueToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
