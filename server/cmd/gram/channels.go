package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/channels"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type channelsTokenAuthenticator interface {
	AuthenticateChannels(context.Context, string) (platformmcp.Principal, error)
}

type channelsSessionAuthenticator interface {
	AuthenticateWithCookie(context.Context) (context.Context, error)
}

type channelsContextPreparer interface {
	PrepareExternalContext(context.Context, platformmcp.Principal) (context.Context, error)
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

// Channels shares the control-plane account, OAuth connection and browser
// session, but grants are loaded live and evaluated against channels scopes.
// The handler calls this authenticator again during SSE delivery as well.
func newChannelsAuthenticator(config platformMCPConfig, tokens channelsTokenAuthenticator, sessions channelsSessionAuthenticator, authorizer channelsContextPreparer, names channelsAccountNames) channels.Authenticator {
	return func(r *http.Request) (channels.Principal, error) {
		ctx := r.Context()
		if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface != requestorigin.SurfacePlatform {
			return channels.Principal{}, errors.New("channels requires a platform origin")
		}
		var principal platformmcp.Principal
		var subjectName string
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			parts := strings.Fields(authorization)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return channels.Principal{}, errors.New("invalid channels authorization")
			}
			var err error
			principal, err = tokens.AuthenticateChannels(ctx, parts[1])
			if err != nil {
				return channels.Principal{}, fmt.Errorf("authenticate channels token: %w", err)
			}
		} else {
			// MCP connections use OAuth, never ambient browser cookies. The dashboard
			// API uses the existing selected organization, not a caller-supplied ID.
			if r.URL.Path == "/channels/mcp" || sessions == nil {
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
			if !ok || auth == nil || auth.UserID == "" || auth.ActiveOrganizationID == "" {
				return channels.Principal{}, errors.New("invalid channels session")
			}
			principal = platformmcp.Principal{UserID: auth.UserID, OrganizationID: auth.ActiveOrganizationID, Surface: platformmcp.SurfaceChannels, ConnectionID: "", Generation: "", ClientID: ""}
			// The dashboard names an operator's own session after their account.
			// Only a write opens or reclaims a session, so reading the snapshot
			// resolves no name and costs no lookup.
			if mutating {
				subjectName = channelsSubjectName(ctx, config.Logger, names, auth)
			}
		}
		if principal.UserID == "" || principal.OrganizationID == "" {
			return channels.Principal{}, errors.New("invalid channels principal")
		}
		// Always start with the original request context. PrepareExternalContext
		// rechecks live membership and replaces cached grants, including on SSE ticks.
		prepared, err := authorizer.PrepareExternalContext(ctx, principal)
		if err != nil {
			return channels.Principal{}, fmt.Errorf("admit channels principal: %w", err)
		}
		// The three permissions are evaluated as separate checks against the
		// freshly loaded grants. None of them implies another, and none is
		// implied by mcp:connect or by organization membership, so one combined
		// check would quietly widen whichever of them the caller lacks.
		allowed := make([]bool, 0, 3)
		for _, scope := range []authz.Scope{authz.ScopeChannelsConnect, authz.ScopeChannelsAdmin, authz.ScopeChannelsReadAll} {
			yes, err := config.Authz.Evaluate(prepared, authz.Check{Scope: scope, ResourceID: principal.OrganizationID, ResourceKind: "", Dimensions: nil})
			if err != nil {
				return channels.Principal{}, fmt.Errorf("evaluate channels permission %s: %w", scope, err)
			}
			allowed = append(allowed, yes)
		}
		return channels.Principal{OrganizationID: principal.OrganizationID, SubjectID: principal.UserID, SubjectName: subjectName, CanConnect: allowed[0], CanAdmin: allowed[1], CanReadAll: allowed[2]}, nil
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

// attachChannels serves the Channels endpoints only where the rollout gate is
// on. The gate is deployment scoped rather than per organization because the
// surfaces it has to cover consistently include unauthenticated ones: the
// endpoint itself, its OAuth resource metadata document, and the audience the
// authorization server will bind an authorization to. A per-organization
// product feature cannot decide those, because no organization is known until
// a caller has authenticated against the audience being advertised.
//
// A disabled deployment registers no routes at all, so every Channels path
// answers 404 from the mux. That is deliberate: a denial that named the
// feature, its permissions or its rollout state would disclose unreleased
// product surface to anyone who probed the host.
func attachChannels(ctx context.Context, config platformMCPConfig, tokens channelsTokenAuthenticator, authorizer channelsContextPreparer) {
	if !config.ChannelsEnabled {
		return
	}
	var names channelsAccountNames
	if config.DB != nil {
		names = channelsUserDisplayNames{db: config.DB}
	}
	handler := channels.New(config.DB, newChannelsAuthenticator(config, tokens, config.Sessions, authorizer, names),
		channels.WithAdminAudit(newChannelsAdminAudit(config.AuditLogger)),
		channels.WithLogger(config.Logger),
	)
	closeChannelsOnShutdown(ctx, config.Logger, handler)
	// Channels has its own audience and resource metadata, but reuses the
	// existing control-plane OAuth server, login, subject and organization
	// selector.
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
		if r.URL.Path == "/channels/mcp" {
			if !channelsProtocolVersionSupported(r.Header.Get(mcpversions.HTTPHeader)) {
				http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
				return
			}
			w = &channelsAuthChallenge{ResponseWriter: w, metadata: platformmcp.ChannelsProtectedResourceMetadataURL(r.Context(), config.ServerURL)}
		}
		handler.ServeHTTP(w, r)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodPatch, http.MethodPut, http.MethodOptions} {
		config.Mux.Handle(method, "/channels/mcp", serve)
		config.Mux.Handle(method, "/channels/api", serve)
	}
}

// newChannelsAccessService serves get_my_channels_access only where Channels is
// rolled out. Elsewhere the Platform MCP tool stays registered as its
// unavailable stub, so it never names an endpoint this deployment answers 404
// for.
func newChannelsAccessService(config platformMCPConfig) *platformmcp.ChannelsAccessService {
	if !config.ChannelsEnabled {
		return nil
	}
	return platformmcp.NewChannelsAccessService(config.Authz, config.ServerURL)
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
