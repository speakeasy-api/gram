package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	srv "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	authz     *authz.Engine
	serverURL string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// NewService constructs the agent service.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	serverURL string,
) *Service {
	logger = logger.With(attr.SlogComponent("agent"))
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/agent"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		authz:     authzEngine,
		serverURL: serverURL,
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// GetPlugins returns every plugin assigned to the device user's resolved
// principal set within the caller's org, marketplace-first. From the polling
// user's email it resolves email → user_id, then RBAC role membership, to
// produce the user:<id>, user:all, and role:<...> principals; the email
// principal and the org wildcard are always included so email- and
// everyone-scoped assignments still deliver.
//
// The polling identity is resolved by credential type (DNO-383), because who
// the key belongs to differs:
//   - Per-user key (`agent_user`): the key owner IS the enrolled developer
//     (minted by token-exchange or manual enrollment), so the polling identity
//     is the authenticated key owner (authCtx.Email) and the vouched `email`
//     param is ignored. Delivery is bound to the authenticated principal.
//   - Org install key (`agent` scope): the key owner is whoever minted the org
//     token in the dashboard (an admin), NOT the developer. The polling identity
//     is the vouched `email` param the MDM profile supplies — required here.
//     This is the zero-touch MDM path where the developer never signs in.
//
// SECURITY: a per-user `agent_user` key's polling identity is the authenticated
// key owner, so it cannot claim another member's user-/role-scoped plugins. An
// org `agent` install key still trusts the caller-supplied payload.Email (not
// bound to the authenticated principal): any holder of the shared org key can
// claim another member's email. That is the accepted shared-org-key limitation,
// and it closes for each device as it migrates to a per-user key.
func (s *Service) GetPlugins(ctx context.Context, payload *gen.GetPluginsPayload) (*gen.GetPluginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Resolve the polling identity by credential type. An org install key carries
	// the `agent` scope; a per-user key carries only `agent_user` (agent implies
	// agent_user, never the reverse — see auth.effectiveScopes), so the presence
	// of `agent` distinguishes them.
	isInstallKey := slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String())

	var email string
	if isInstallKey {
		// Org key: the owner is an admin, not the developer, so we must be vouched
		// an email — the MDM profile supplies it.
		email = conv.NormalizeEmail(payload.Email)
		if email == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "email is required when authenticating with an org-scoped agent install key")
		}
	} else if authCtx.Email != nil {
		// Per-user key: the owner is the enrolled developer, bound to the token.
		email = conv.NormalizeEmail(*authCtx.Email)
	}
	emailPrincipal, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + email)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
	}

	// Best-effort: record that this user's device agent polled, so the dashboard
	// can show who is actively running it. Never fail the sync if the write fails
	// (mirrors api_keys.last_accessed_at). The query's ON CONFLICT guard caps
	// writes to at most once per minute per (org, email).
	if err := s.repo.UpsertDeviceAgentSync(ctx, repo.UpsertDeviceAgentSyncParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          email,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to record device agent sync",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
	}

	// Assignments can target the email or the org wildcard directly; those always
	// apply regardless of whether the email maps to an org member.
	principals := []string{emailPrincipal.String(), urn.PrincipalWildcard}

	// Resolve the reported email to an org member so user:<id>, user:all, and
	// role:<...> assignments deliver too. A non-member (or unknown email) is not
	// an error: the caller still receives email- and wildcard-scoped plugins.
	user, err := usersrepo.New(s.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case err == nil:
		resolved, err := authz.ResolveUserPrincipals(ctx, s.db, authCtx.ActiveOrganizationID, user.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent principals").LogError(ctx, s.logger)
		}
		for _, principal := range resolved {
			principals = append(principals, principal.String())
		}
	case errors.Is(err, pgx.ErrNoRows):
		// Email is not an active member of this org; wildcard/email scoping only.
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent user").LogError(ctx, s.logger)
	}

	rows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin set").LogError(ctx, s.logger)
	}

	base := strings.TrimRight(s.serverURL, "/")
	marketplaceURL := func(token string) string {
		return base + marketplace.RoutePrefix + token + ".git"
	}

	return mv.BuildAgentPluginsView(rows, marketplaceURL), nil
}

// ListSyncedUsers returns the emails seen polling agent.getPlugins for the
// caller's org, most recently active first, for the dashboard's device-agent
// users view. Org admins only; attribution is by the email the agent reports on
// each sync (the org-scoped API key is shared across the fleet).
func (s *Service) ListSyncedUsers(ctx context.Context, _ *gen.ListSyncedUsersPayload) (*gen.ListSyncedUsersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	rows, err := s.repo.ListDeviceAgentSyncs(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing device agent syncs").LogError(ctx, s.logger)
	}

	users := make([]*gen.SyncedAgentUser, 0, len(rows))
	for _, r := range rows {
		users = append(users, &gen.SyncedAgentUser{
			Email:       r.Email,
			FirstSeenAt: r.FirstSeenAt.Time.Format(time.RFC3339),
			LastSeenAt:  r.LastSeenAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListSyncedUsersResult{Users: users}, nil
}
