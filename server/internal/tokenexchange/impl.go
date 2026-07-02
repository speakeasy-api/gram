// Package tokenexchange implements the device-agent token surface (DNO-383).
// The Speakeasy device agent authenticates with an org-scoped API
// key carrying the `agent` scope and exchanges a vouched user email for a
// long-lived, per-user API key carrying the `agent` and `hooks` scopes.
//
// The minted key is a normal Gram API key (see internal/keys): a
// `gram_<env>_<token>` string whose SHA-256 hash is stored in api_keys with the
// resolved user as `created_by_user_id`, so KeyBasedAuth later resolves it back
// to that user + org. The key is long-lived (api_keys has no TTL); its lifecycle
// lever is revocation. A fresh exchange rotates: it best-effort revokes the
// user's prior device-agent key(s) in the org before minting a new one.
package tokenexchange

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/token_exchange/server"
	gen "github.com/speakeasy-api/gram/server/gen/token_exchange"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keys_repo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	users_repo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// deviceAgentKeyName is the fixed name stamped on every minted device-agent
// key. It is the rotation key: exchange revokes prior keys with this name owned
// by the same user in the org before minting a fresh one.
const deviceAgentKeyName = "device-agent"

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	keysRepo     *keys_repo.Queries
	usersRepo    *users_repo.Queries
	projectsRepo *projects_repo.Queries
	auth         *auth.Auth
	keyPrefix    string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// NewService constructs the token-exchange service. It reuses the already
// constructed db / auth deps from the server entrypoint — do not build new
// ones. `env` selects the API-key prefix (gram_local_/gram_test_/gram_live_),
// matching how keys.Service derives it.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionsManager *sessions.Manager,
	authzEngine *authz.Engine,
	env string,
) *Service {
	logger = logger.With(attr.SlogComponent("tokenexchange"))
	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/tokenexchange"),
		logger:       logger,
		db:           db,
		keysRepo:     keys_repo.New(db),
		usersRepo:    users_repo.New(db),
		projectsRepo: projects_repo.New(db),
		auth:         auth.New(logger, db, sessionsManager, authzEngine),
		keyPrefix:    auth.APIKeyPrefix(env),
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

// APIKeyAuth authorizes the org-scoped agent API key on the exchange method.
// Delegates to the same auth.Authorize path agent.getPlugins uses, so the
// `agent` scope requirement declared in the design is enforced here.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// Exchange trades the authenticated org-scoped agent key + a vouched user email
// for a long-lived, per-user API key carrying the `agent` and `hooks` scopes.
// The raw key is returned exactly once.
func (s *Service) Exchange(ctx context.Context, payload *gen.ExchangePayload) (*gen.TokenResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	orgID := authCtx.ActiveOrganizationID
	if orgID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	email := conv.NormalizeEmail(payload.Email)
	if _, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + email); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
	}

	// Resolve the vouched email to a real user within the authenticated org.
	// The minted key's created_by_user_id MUST be a real users row or the JOIN
	// in GetAPIKeyByKeyHash drops it and later auth fails. Fail closed if the
	// email is not a member of the org — never fabricate a user id.
	user, err := s.usersRepo.GetConnectedUserByEmail(ctx, users_repo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: orgID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, err, "no user with that email in this organization")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving user by email").LogError(ctx, s.logger)
	}

	// Resolve the org's project to bind the key to. ListProjectsByOrganization
	// is ordered by id ASC, so the first entry is the org's default/oldest
	// project (mirrors checkProjectAccess in internal/auth).
	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving org project").LogError(ctx, s.logger)
	}
	if len(projects) == 0 {
		return nil, oops.E(oops.CodeUnexpected, nil, "organization has no project to bind the device-agent key to").LogError(ctx, s.logger)
	}
	projectID := uuid.NullUUID{UUID: projects[0].ID, Valid: true}

	// Rotation: best-effort revoke the user's prior device-agent key(s) in this
	// org before minting a fresh one. Long-lived keys, so revocation is the
	// lifecycle lever. A revoke failure must not block a fresh mint.
	if err := s.keysRepo.DeleteAPIKeysByNameAndUser(ctx, keys_repo.DeleteAPIKeysByNameAndUserParams{
		OrganizationID:  orgID,
		CreatedByUserID: user.ID,
		Name:            deviceAgentKeyName,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to revoke prior device-agent key(s); minting anyway",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
	}

	token, err := generateToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate api key token").LogError(ctx, s.logger)
	}

	fullKey := s.keyPrefix + token
	keyHash, err := auth.GetAPIKeyHash(fullKey)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "hash api key").LogError(ctx, s.logger)
	}

	if _, err := s.keysRepo.CreateAPIKey(ctx, keys_repo.CreateAPIKeyParams{
		OrganizationID:  orgID,
		ProjectID:       projectID,
		CreatedByUserID: user.ID,
		Name:            deviceAgentKeyName,
		KeyPrefix:       s.keyPrefix + token[:5],
		KeyHash:         keyHash,
		Scopes:          []string{auth.APIKeyScopeAgent.String(), auth.APIKeyScopeHooks.String()},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create device-agent api key").LogError(ctx, s.logger)
	}

	// Long-lived, mint-once credential: empty refresh + zero expiry. The device
	// wire contract still parses these fields; it just never refreshes.
	return &gen.TokenResult{
		AccessToken:  fullKey,
		RefreshToken: "",
		ExpiresIn:    0,
		UserEmail:    user.Email,
	}, nil
}

// generateToken produces a 32-byte (64 hex char) cryptographically random
// token, mirroring keys.Service.generateToken so minted keys are
// indistinguishable from dashboard-created ones.
func generateToken() (string, error) {
	const randomKeyLength = 64
	randomBytes := make([]byte, randomKeyLength/2)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate random token bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}
