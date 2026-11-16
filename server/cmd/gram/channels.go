package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/channels"
	"github.com/speakeasy-api/gram/server/internal/channels/authn"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// channelsConfig composes Channels independently of Platform MCP. The latter
// may expose discovery, but does not own Channels credentials or routes.
type channelsConfig struct {
	Logger                  *slog.Logger
	Mux                     goahttp.Muxer
	DB                      *pgxpool.Pool
	ServerURL, DashboardURL *url.URL
	Authz                   *authz.Engine
	Sessions                channelsSessionAuthenticator
	AuditLogger             *audit.Logger
	Features                feature.Provider
	JWTSigningKey           string
	resolveTarget           func(context.Context, string, string) (authn.Target, error)
}

type channelsTokenAuthenticator interface {
	Authenticate(context.Context, string) (authn.Identity, error)
}

type channelsSessionAuthenticator interface {
	AuthenticateWithCookie(context.Context) (context.Context, error)
}

type channelsContextPreparer interface {
	PrepareContext(context.Context, authn.Identity) (context.Context, error)
}

// channelsAdmission binds only verified identities to fresh organization policy.
// Native agent/workload credentials are not admitted by this OAuth surface.
type channelsAdmission struct {
	db       *pgxpool.Pool
	engine   *authz.Engine
	features feature.Provider
}

func (a channelsAdmission) PrepareContext(ctx context.Context, identity authn.Identity) (context.Context, error) {
	if a.db == nil || a.engine == nil {
		return ctx, errors.New("channels admission unavailable")
	}
	if identity.Principal.Type != urn.PrincipalTypeUser || identity.Principal.ID == "" || identity.OrganizationID == "" || identity.ProjectID == uuid.Nil {
		return ctx, errors.New("unsupported channels identity")
	}
	member, err := organizationsrepo.New(a.db).HasActiveOrganizationUser(ctx, organizationsrepo.HasActiveOrganizationUserParams{
		UserID: identity.Principal.ID, OrganizationID: identity.OrganizationID,
	})
	if err != nil {
		return ctx, fmt.Errorf("check channels organization membership: %w", err)
	}
	if !member {
		return ctx, errors.New("channels organization membership required")
	}
	organization, err := organizationsrepo.New(a.db).GetOrganizationMetadata(ctx, identity.OrganizationID)
	if err != nil {
		return ctx, fmt.Errorf("load channels organization: %w", err)
	}
	project, err := projectsrepo.New(a.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: identity.ProjectID, OrganizationID: identity.OrganizationID})
	if err != nil {
		return ctx, fmt.Errorf("load channels project: %w", err)
	}
	if a.features == nil {
		return ctx, errors.New("channels availability unavailable")
	}
	enabled, err := a.features.IsFlagEnabled(ctx, feature.FlagChannels, identity.OrganizationID, feature.OrgProjectGroups(organization.Slug, project.Slug))
	if err != nil {
		return ctx, fmt.Errorf("check channels availability: %w", err)
	}
	if !enabled {
		return ctx, errors.New("channels is not enabled for this organization")
	}
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: identity.OrganizationID,
		UserID:               identity.Principal.ID,
		OrganizationSlug:     organization.Slug,
		AccountType:          organization.GramAccountType,
		Whitelisted:          organization.Whitelisted,
		ExternalUserID:       "", APIKeyID: "", APIKeyName: "", OrgWidePluginHooksKey: false,
		SessionID: nil, ProjectID: &project.ID, Email: nil, HasActiveSubscription: false,
		ProjectSlug: &project.Slug, APIKeyScopes: nil, IsAdmin: false, SupportOrganizationID: "",
	}, identity.Principal)
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfaceChannels)
	principals, err := authz.ResolveUserPrincipals(ctx, a.db, identity.OrganizationID, identity.Principal.ID)
	if err != nil {
		return ctx, fmt.Errorf("resolve channels principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, a.db, identity.OrganizationID, principals)
	if err != nil {
		return ctx, fmt.Errorf("load channels grants: %w", err)
	}
	// Replacing inherited grants matters for each SSE reconciliation as well as
	// ordinary requests; PrepareContext alone may reuse a cached grant context.
	ctx = authz.GrantsToContext(ctx, grants)
	enforce, err := a.engine.ShouldEnforce(ctx)
	if err != nil {
		return ctx, fmt.Errorf("check channels authorization enforcement: %w", err)
	}
	if !enforce {
		return ctx, errors.New("channels authorization enforcement unavailable")
	}
	if err := a.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: authz.ResourceKindProject, ResourceID: project.ID.String(), Dimensions: nil}); err != nil {
		return ctx, fmt.Errorf("channels project access required: %w", err)
	}
	return ctx, nil
}

// channelsAccountNames resolves the account display name that labels an
// operator's own dashboard session.
type channelsAccountNames interface {
	ChannelsDisplayName(context.Context, string) (string, error)
}

type channelsUserDisplayNames struct{ db *pgxpool.Pool }

func (n channelsUserDisplayNames) ChannelsDisplayName(ctx context.Context, userID string) (string, error) {
	user, err := usersrepo.New(n.db).GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("load channels account display name: %w", err)
	}
	return user.DisplayName, nil
}

// Channels shares control-plane identities and browser sessions, not OAuth
// credentials. Grants are loaded live and evaluated against Channels scopes.
// The handler calls this authenticator again during SSE delivery as well.
func newChannelsAuthenticator(config channelsConfig, tokens channelsTokenAuthenticator, sessions channelsSessionAuthenticator, authorizer channelsContextPreparer, names channelsAccountNames) channels.Authenticator {
	return func(r *http.Request) (channels.Principal, error) {
		ctx := r.Context()
		if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface != requestorigin.SurfacePlatform {
			return channels.Principal{}, errors.New("channels requires a platform origin")
		}
		if config.ServerURL == nil {
			return channels.Principal{}, errors.New("channels target resolution unavailable")
		}
		orgSlug, projectSlug, endpoint, validPath := authn.ResourceSlugs(r.URL.Path)
		if !validPath || r.URL.RawPath != "" {
			return channels.Principal{}, errors.New("invalid channels resource path")
		}
		resource := strings.TrimRight(config.ServerURL.String(), "/") + "/channels/" + orgSlug + "/" + projectSlug + "/mcp"
		resolveTarget := config.resolveTarget
		if resolveTarget == nil {
			resolveTarget = func(ctx context.Context, origin, resource string) (authn.Target, error) {
				return authn.ResolveTarget(ctx, config.DB, origin, resource)
			}
		}
		target, err := resolveTarget(ctx, config.ServerURL.String(), resource)
		if err != nil {
			return channels.Principal{}, fmt.Errorf("resolve channels target: %w", err)
		}
		ctx = authn.WithTarget(ctx, target)
		var principal authn.Identity
		var subjectName string
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			// Only MCP accepts OAuth tokens. Dashboard operations derive their
			// surface from the API path, so they require a browser session.
			if endpoint != "mcp" {
				return channels.Principal{}, errors.New("channels dashboard requires session authentication")
			}
			parts := strings.Fields(authorization)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return channels.Principal{}, errors.New("invalid channels authorization")
			}
			var err error
			if tokens == nil {
				return channels.Principal{}, errors.New("channels credential authentication unavailable")
			}
			principal, err = tokens.Authenticate(ctx, parts[1])
			if err != nil {
				return channels.Principal{}, fmt.Errorf("authenticate channels token: %w", err)
			}
		} else {
			// MCP connections use OAuth, never ambient browser cookies. The dashboard
			// API uses the URL-resolved organization and project, not the active
			// organization in the browser session.
			if endpoint == "mcp" || sessions == nil {
				return channels.Principal{}, errors.New("channels requires authentication")
			}
			mutating := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
			if mutating {
				origin := r.Header.Get("Origin")
				platformOrigin := requestorigin.BaseURL(ctx, config.ServerURL.String())
				if origin == "" || (origin != strings.TrimRight(platformOrigin, "/") && origin != strings.TrimRight(config.DashboardURL.String(), "/")) {
					return channels.Principal{}, errors.New("invalid channels request origin")
				}
			}
			sessionCtx, err := sessions.AuthenticateWithCookie(ctx)
			if err != nil {
				return channels.Principal{}, fmt.Errorf("authenticate channels session: %w", err)
			}
			auth, ok := contextvalues.GetAuthContext(sessionCtx)
			actor, authenticated := contextvalues.AuthenticatedActor(sessionCtx)
			if !ok || auth == nil || auth.UserID == "" || auth.ActiveOrganizationID == "" || !authenticated || actor.Type != urn.PrincipalTypeUser || actor.ID != auth.UserID || !contextvalues.HasValidatedGramSession(sessionCtx) || contextvalues.IsSupportSession(sessionCtx) || contextvalues.IsLegacyImpersonatedSession(sessionCtx) {
				return channels.Principal{}, errors.New("invalid channels session")
			}
			principal = authn.Identity{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID), OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, BrowserOrganizationID: "", BrowserSessionID: ""}
			// The dashboard names an operator's own session after their account.
			// Only a write opens or reclaims a session, so reading the snapshot
			// resolves no name and costs no lookup.
			if mutating {
				subjectName = channelsSubjectName(ctx, config.Logger, names, auth)
			}
		}
		if principal.OrganizationID != target.OrganizationID || principal.ProjectID != target.ProjectID {
			return channels.Principal{}, errors.New("channels token target mismatch")
		}
		if principal.Principal.Type != urn.PrincipalTypeUser || principal.Principal.ID == "" || principal.OrganizationID == "" {
			return channels.Principal{}, errors.New("invalid channels principal")
		}
		// Always start with the original request context. PrepareContext
		// rechecks live membership and replaces cached grants, including on SSE ticks.
		if authorizer == nil {
			return channels.Principal{}, errors.New("channels admission unavailable")
		}
		prepared, err := authorizer.PrepareContext(ctx, principal)
		if err != nil {
			return channels.Principal{}, fmt.Errorf("admit channels principal: %w", err)
		}
		// The three permissions are evaluated as separate checks against the
		// freshly loaded grants. None of them implies another, and none is
		// implied by mcp:connect or by organization membership, so one combined
		// check would quietly widen whichever of them the caller lacks.
		allowed := make([]bool, 0, 3)
		for _, scope := range []authz.Scope{authz.ScopeChannelsConnect, authz.ScopeChannelsAdmin, authz.ScopeChannelsReadAll} {
			yes, err := config.Authz.Evaluate(prepared, authz.Check{Scope: scope, ResourceID: principal.ProjectID.String(), ResourceKind: "project", Dimensions: nil})
			if err != nil {
				return channels.Principal{}, fmt.Errorf("evaluate channels permission %s: %w", scope, err)
			}
			allowed = append(allowed, yes)
		}
		return channels.Principal{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID.String(), SubjectID: principal.Principal.ID, SubjectName: subjectName, CanConnect: allowed[0], CanAdmin: allowed[1], CanReadAll: allowed[2]}, nil
	}
}

// channelsSubjectName is the display label for an operator's own dashboard
// session. It is display only and carries no authority.
//
// The account's display name is preferred over its email address. An instance
// label is readable by every authorized member of the organization and by the
// MCP clients they have connected, including third-party clients whose name
// and metadata URL the client itself chose, so publishing full email addresses
// there would hand out organization contact details that Channels does not
// need. When an account has no display name the label falls back to the local
// part of the address, which keeps one person's sessions distinguishable from
// another's without republishing a routable address.
//
// A failed lookup is not a denial. The handler labels a nameless session
// "member on dashboard", which is worse to read and still safe.
func channelsSubjectName(ctx context.Context, logger *slog.Logger, names channelsAccountNames, auth *contextvalues.AuthContext) string {
	if names != nil {
		switch displayName, err := names.ChannelsDisplayName(ctx, auth.UserID); {
		case err != nil:
			if logger != nil {
				logger.WarnContext(ctx, "channels could not resolve an account display name", attr.SlogError(err))
			}
		case strings.TrimSpace(displayName) != "":
			return strings.TrimSpace(displayName)
		}
	}
	email := conv.PtrValOr(auth.Email, "")
	if localPart, _, found := strings.Cut(email, "@"); found {
		return localPart
	}
	return ""
}

// channelsAuthChallenge adds the RFC 9728 bearer challenge to every 401 the
// Channels endpoint returns, and to no other status.
//
// A client whose Channels token expired, was revoked, or was minted for the
// Platform MCP audience has to be told where the Channels resource metadata
// is, or it cannot discover that it must authorize again for this audience
// rather than retry the token it holds. Emitting the header before the handler
// runs instead would attach a challenge to successful responses.
type channelsAuthChallenge struct {
	http.ResponseWriter
	metadata string
}

func (w *channelsAuthChallenge) WriteHeader(status int) {
	if status == http.StatusUnauthorized && w.metadata != "" {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer resource_metadata=%q", w.metadata))
	}
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap exposes the underlying writer to http.ResponseController. Subscription
// streams set a write deadline through it, and that call fails silently against
// a wrapper that hides the real writer.
func (w *channelsAuthChallenge) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *channelsAuthChallenge) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// channelsOAuthMux withholds control-plane authentication on custom/private
// ingress, just as the Channels resource routes do. Metadata uses pinned URLs.
type channelsOAuthMux struct{ mux goahttp.Muxer }

func (m channelsOAuthMux) Handle(method, path string, handler http.HandlerFunc) {
	m.mux.Handle(method, path, func(w http.ResponseWriter, r *http.Request) {
		if origin, ok := requestorigin.FromContext(r.Context()); ok && origin.Surface != requestorigin.SurfacePlatform {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	})
}

func configureChannels(ctx context.Context, config channelsConfig) error {
	if config.Mux == nil || config.DB == nil || config.Authz == nil || config.Sessions == nil || config.JWTSigningKey == "" {
		return errors.New("incomplete channels configuration")
	}
	admission := channelsAdmission{db: config.DB, engine: config.Authz, features: config.Features}
	oauth, err := authn.New(authn.Config{
		BaseURL:      config.ServerURL,
		DashboardURL: config.DashboardURL,
		Logger:       config.Logger,
		DB:           config.DB,
		Signer:       sessiontokens.NewSigner(config.JWTSigningKey),
		AuthenticateBrowser: func(r *http.Request) (context.Context, error) {
			if config.Sessions == nil {
				return r.Context(), errors.New("channels browser authentication unavailable")
			}
			return config.Sessions.AuthenticateWithCookie(r.Context())
		},
		// Authentication rechecks membership and loads current grants. Each
		// Channels operation applies enablement and effective access exceptions;
		// a native-only scope check here would reject valid exception grants.
		Admit: admission.PrepareContext,
	})
	if err != nil {
		return fmt.Errorf("configure channels authentication: %w", err)
	}
	oauth.Attach(channelsOAuthMux{mux: config.Mux})
	attachChannels(ctx, config, oauth, admission)
	return nil
}

// attachChannels serves authenticated Channels operations independently of Platform MCP.
func attachChannels(ctx context.Context, config channelsConfig, tokens channelsTokenAuthenticator, authorizer channelsContextPreparer) {
	var names channelsAccountNames
	if config.DB != nil {
		names = channelsUserDisplayNames{db: config.DB}
	}
	handler := channels.New(config.DB, newChannelsAuthenticator(config, tokens, config.Sessions, authorizer, names),
		channels.WithAdminAudit(newChannelsAdminAudit(config.AuditLogger)),
		channels.WithLogger(config.Logger),
	)
	closeChannelsOnShutdown(ctx, config.Logger, handler)
	// The independent Channels authorization server owns its credentials and metadata.
	serve := func(w http.ResponseWriter, r *http.Request) {
		// First-party platform hosts only. A custom domain or private-network
		// ingress has no Channels resource, so answering there would advertise
		// resource metadata for an audience no token can be minted for, and
		// would reflect the requesting host back in the challenge. The
		// authenticator refuses those surfaces too; refusing at the route is
		// what keeps the advertised audience and the accepted audience the same
		// string.
		if origin, ok := requestorigin.FromContext(r.Context()); ok && origin.Surface != requestorigin.SurfacePlatform {
			http.NotFound(w, r)
			return
		}
		orgSlug, projectSlug, endpoint, validPath := authn.ResourceSlugs(r.URL.Path)
		if !validPath || r.URL.RawPath != "" {
			http.NotFound(w, r)
			return
		}
		if endpoint == "mcp" {
			if !channelsProtocolVersionSupported(r.Header.Get(mcpversions.HTTPHeader)) {
				http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
				return
			}
			w = &channelsAuthChallenge{ResponseWriter: w, metadata: config.ServerURL.JoinPath(".well-known/oauth-protected-resource/channels", orgSlug, projectSlug, "mcp").String()}
		}
		handler.ServeHTTP(w, r)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodPatch, http.MethodPut, http.MethodOptions} {
		config.Mux.Handle(method, "/channels/{orgSlug}/{projectSlug}/mcp", serve)
		config.Mux.Handle(method, "/channels/{orgSlug}/{projectSlug}/api", serve)
	}
}

// channelsProtocolVersionSupported admits a request that declares no version
// or one the Channels endpoint serves.
func channelsProtocolVersionSupported(version string) bool {
	return version == "" || slices.Contains(channels.SupportedProtocolVersions, version)
}

// channelsStreamDrainTimeout bounds how long shutdown waits for subscription
// streams to write their completion notification.
const channelsStreamDrainTimeout = 10 * time.Second

type channelsStreamCloser interface {
	Shutdown(context.Context) error
}

// Active SSE requests outlive http.Server.Shutdown unless the handler cancels
// them: Shutdown waits for in-flight handlers, and a subscription handler does
// not return until its stream is cancelled. So this registers its own signal
// notification rather than relying on the root context, which the server
// command does not cancel on a signal — it derives a separate context for
// shutdown. Both registrations receive the signal, so stream cancellation runs
// concurrently with the HTTP drain instead of deadlocking behind it. Do not
// replace this with the root context alone.
func closeChannelsOnShutdown(ctx context.Context, logger *slog.Logger, handler channelsStreamCloser) {
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer stop()
		<-shutdownCtx.Done()
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelsStreamDrainTimeout)
		defer cancel()
		if err := handler.Shutdown(drainCtx); err != nil && logger != nil {
			logger.ErrorContext(ctx, "channels subscription streams did not complete", attr.SlogError(err))
		}
	}()
}
