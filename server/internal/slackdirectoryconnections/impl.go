package slackdirectoryconnections

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/slack_directory_connections/server"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

const stateTTL = 10 * time.Minute

type Service struct {
	db            *pgxpool.Pool
	auth          *auth.Auth
	authz         *authz.Engine
	audit         *audit.Logger
	cache         cache.Cache
	encryption    *encryption.Client
	provider      Provider
	siteURL       *url.URL
	tracer        trace.Tracer
	logger        *slog.Logger
	syncScheduler SyncScheduler
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tp trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, engine *authz.Engine, auditLogger *audit.Logger, stateCache cache.Cache, enc *encryption.Client, provider Provider, siteURL *url.URL, scheduler SyncScheduler) *Service {
	return &Service{db: db, auth: auth.New(logger, db, sessions, engine), authz: engine, audit: auditLogger, cache: stateCache, encryption: enc, provider: provider, syncScheduler: scheduler, siteURL: siteURL, tracer: tp.Tracer("github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"), logger: logger}
}
func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	mux.Handle("GET", CallbackPath, service.handleCallback)
}
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) authorize(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.SessionID == nil || *ac.SessionID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, byKey := contextvalues.APIKeyAuthorization(ctx); byKey || contextvalues.IsSupportSession(ctx) {
		return nil, oops.C(oops.CodeForbidden)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return ac, nil
}

// authorizeMutation keeps the shared demo readable without allowing live Slack
// credentials or workspace changes to enter its public dataset.
func (s *Service) authorizeMutation(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	if ac.ActiveOrganizationID == constants.DemoOrganizationID {
		return nil, oops.E(oops.CodeForbidden, nil, "The shared demo is read-only")
	}
	return ac, nil
}

func (s *Service) List(ctx context.Context, _ *gen.ListPayload) (*gen.ListResult, error) {
	ac, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := repo.New(s.db).ListSlackDirectoryConnectionSummaries(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not list Slack workspaces").LogError(ctx, s.logger)
	}
	result := &gen.ListResult{Connections: make([]*gen.SlackDirectoryConnection, 0, len(rows)), AuthorizationConfigured: s.provider != nil}
	statusCtx, cancelStatus := context.WithTimeout(ctx, 2*time.Second)
	defer cancelStatus()
	for _, summary := range rows {
		row := summary.SlackDirectoryConnection
		view := s.connectionView(ctx, row)
		view.MemberCount = summary.MemberCount
		state, stateErr := s.syncScheduler.State(statusCtx, row.ID, row.Generation)
		if stateErr != nil {
			view.SyncStatus = "unknown"
		} else {
			view.SyncStatus = state.Status
			view.SyncPhase = conv.PtrEmpty(state.Progress.Phase)
			view.SyncPages = conv.PtrEmpty(state.Progress.Pages)
			view.SyncMembers = conv.PtrEmpty(state.Progress.Members)
		}
		result.Connections = append(result.Connections, view)
	}
	return result, nil
}

func (s *Service) connectionView(ctx context.Context, row repo.SlackDirectoryConnection) *gen.SlackDirectoryConnection {
	view := mv.BuildSlackDirectoryConnectionView(row)
	if view.Status == "connected" {
		if _, err := usableTokens(s.encryption, row); err != nil {
			view.Status = "reconnect_required"
			if syncErr, ok := errors.AsType[*SyncError](err); ok {
				view.LastErrorCode = conv.PtrEmpty(syncErr.Code)
			}
			if view.DirectoryStatus == "current" {
				view.DirectoryStatus = "stale"
			}
		}
	}
	return view
}

type oauthState struct {
	OrganizationID string
	UserID         string
	SessionID      string
	ConnectionID   uuid.UUID
	WorkspaceID    string
	Generations    map[string]uuid.UUID
	ExpiresAt      time.Time
}

func stateKey(state string) string {
	digest := sha256.Sum256([]byte(state))
	return "slack-directory-oauth:" + hex.EncodeToString(digest[:])
}

func (s *Service) Begin(ctx context.Context, p *gen.BeginPayload) (*gen.BeginResult, error) {
	ac, err := s.authorizeMutation(ctx)
	if err != nil {
		return nil, err
	}
	if s.provider == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "Slack workspace authorization is not configured")
	}
	rows, err := repo.New(s.db).ListSlackDirectoryConnections(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not read Slack workspaces").LogError(ctx, s.logger)
	}
	state := oauthState{OrganizationID: ac.ActiveOrganizationID, UserID: ac.UserID, SessionID: *ac.SessionID, ConnectionID: uuid.Nil, WorkspaceID: "", Generations: make(map[string]uuid.UUID, len(rows)), ExpiresAt: time.Now().Add(stateTTL)}
	for _, row := range rows {
		state.Generations[row.SlackTeamID] = row.Generation
		if p.ConnectionID != nil && row.ID.String() == *p.ConnectionID {
			state.ConnectionID = row.ID
			state.WorkspaceID = row.SlackTeamID
		}
	}
	if p.ConnectionID != nil && state.ConnectionID == uuid.Nil {
		return nil, oops.C(oops.CodeNotFound)
	}
	token := base64.RawURLEncoding.EncodeToString(randBytes())
	if err := s.cache.Set(ctx, stateKey(token), state, stateTTL); err != nil {
		s.logger.WarnContext(ctx, "Could not store Slack authorization state", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnavailable, nil, "could not start Slack authorization")
	}
	return &gen.BeginResult{AuthorizationURL: s.provider.AuthorizationURL(token, state.WorkspaceID)}, nil
}

func randBytes() []byte { b := make([]byte, 32); _, _ = rand.Read(b); return b }

func (s *Service) callbackResult(ac *contextvalues.AuthContext, status string) *CallbackResult {
	destination := *s.siteURL
	destination.Path = "/" + url.PathEscape(ac.OrganizationSlug) + "/identity"
	destination.RawPath = ""
	destination.RawQuery = url.Values{"tab": {"slack-workspaces"}, "slack_result": {status}}.Encode()
	destination.Fragment = ""
	return &CallbackResult{Location: destination.String(), CacheControl: "no-store"}
}

func (s *Service) Callback(ctx context.Context, p *CallbackPayload) (*CallbackResult, error) {
	ac, err := s.authorizeMutation(ctx)
	if err != nil {
		return nil, err
	}
	if (p.Code == nil || *p.Code == "") && p.Error == nil {
		return s.callbackResult(ac, "invalid_state"), nil
	}
	var state oauthState
	if len(p.State) < 32 || len(p.State) > 256 {
		return s.callbackResult(ac, "invalid_state"), nil
	}
	// A single atomic consume also rejects concurrent callback replay across servers.
	if err := s.cache.GetAndDelete(ctx, stateKey(p.State), &state); err != nil || !time.Now().Before(state.ExpiresAt) || state.OrganizationID != ac.ActiveOrganizationID || state.UserID != ac.UserID || subtle.ConstantTimeCompare([]byte(state.SessionID), []byte(*ac.SessionID)) != 1 {
		return s.callbackResult(ac, "invalid_state"), nil
	}
	if p.Error != nil {
		if *p.Error == "access_denied" {
			return s.callbackResult(ac, "cancelled"), nil
		}
		return s.callbackResult(ac, "authorization_failed"), nil
	}
	if s.provider == nil || p.Code == nil || *p.Code == "" {
		return s.callbackResult(ac, "authorization_failed"), nil
	}
	authorization, err := s.provider.Exchange(ctx, *p.Code)
	if err != nil {
		s.logger.WarnContext(ctx, "Slack workspace authorization failed", attr.SlogError(err))
		return s.callbackResult(ac, "authorization_failed"), nil
	}
	if state.WorkspaceID != "" && state.WorkspaceID != authorization.WorkspaceID {
		return s.callbackResult(ac, "wrong_workspace"), nil
	}
	if err := s.saveAuthorization(ctx, ac, state, authorization); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.callbackResult(ac, "connection_changed"), nil
		}
		return nil, oops.E(oops.CodeUnexpected, err, "could not save Slack authorization").LogError(ctx, s.logger)
	}
	return s.callbackResult(ac, "connected"), nil
}

func (s *Service) saveAuthorization(ctx context.Context, ac *contextvalues.AuthContext, state oauthState, authorization *Authorization) error {
	plaintext, err := json.Marshal(authorization.Tokens)
	if err != nil {
		return fmt.Errorf("encode credential bundle: %w", err)
	}
	ciphertext, err := s.encryption.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt credential bundle: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin authorization transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	before, err := queries.LockSlackDirectoryConnectionByTeam(ctx, repo.LockSlackDirectoryConnectionByTeamParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: authorization.WorkspaceID})
	var previous *gen.SlackDirectoryConnection
	var row repo.SlackDirectoryConnection
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if state.ConnectionID != uuid.Nil {
			return pgx.ErrNoRows
		}
		row, err = queries.CreateSlackDirectoryConnection(ctx, repo.CreateSlackDirectoryConnectionParams{OrganizationID: ac.ActiveOrganizationID, SlackTeamID: authorization.WorkspaceID, SlackTeamName: conv.ToPGTextEmpty(authorization.WorkspaceName), CredentialsEncrypted: conv.ToPGText(ciphertext), GrantedScopes: authorization.Scopes, Generation: uuid.New()})
	case err != nil:
		return fmt.Errorf("lock workspace: %w", err)
	default:
		if state.Generations[authorization.WorkspaceID] != before.Generation || (state.ConnectionID != uuid.Nil && state.ConnectionID != before.ID) {
			return pgx.ErrNoRows
		}
		previous = s.connectionView(ctx, before)
		row, err = queries.AuthorizeSlackDirectoryConnection(ctx, repo.AuthorizeSlackDirectoryConnectionParams{OrganizationID: ac.ActiveOrganizationID, ID: before.ID, SlackTeamID: authorization.WorkspaceID, SlackTeamName: conv.ToPGTextEmpty(authorization.WorkspaceName), CredentialsEncrypted: conv.ToPGText(ciphertext), GrantedScopes: authorization.Scopes, Generation: uuid.New(), ExpectedGeneration: before.Generation})
	}
	if err != nil {
		return fmt.Errorf("save workspace authorization: %w", err)
	}
	memberCount, err := queries.CountSlackDirectorySnapshotMembers(ctx, repo.CountSlackDirectorySnapshotMembersParams{OrganizationID: ac.ActiveOrganizationID, ID: row.ID})
	if err != nil {
		return fmt.Errorf("count retained Slack members: %w", err)
	}
	if previous != nil {
		previous.MemberCount = memberCount
	}
	after := s.connectionView(ctx, row)
	after.MemberCount = memberCount
	if err := s.audit.LogSlackDirectoryConnectionAuthorize(ctx, tx, audit.LogSlackDirectoryConnectionEvent{OrganizationID: ac.ActiveOrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ConnectionURN: urn.NewSlackDirectoryConnection(row.ID), ConnectionSnapshotBefore: previous, ConnectionSnapshotAfter: after}); err != nil {
		return fmt.Errorf("audit workspace authorization: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit workspace authorization: %w", err)
	}
	// Authorization is already durable. A start failure leaves a manual retry in the UI.
	if err := s.syncScheduler.Start(ctx, SyncInput{OrganizationID: ac.ActiveOrganizationID, ConnectionID: row.ID, Generation: row.Generation, ActorID: ac.UserID, StartedAt: time.Time{}}); err != nil {
		s.logger.WarnContext(ctx, "could not start initial Slack directory sync", attr.SlogError(err))
	}
	return nil
}

func (s *Service) Disconnect(ctx context.Context, p *gen.DisconnectPayload) (*gen.SlackDirectoryConnection, error) {
	ac, err := s.authorizeMutation(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	generation, err := uuid.Parse(p.Generation)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not disconnect Slack workspace").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	before, err := queries.LockSlackDirectoryConnection(ctx, repo.LockSlackDirectoryConnectionParams{OrganizationID: ac.ActiveOrganizationID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not read Slack workspace").LogError(ctx, s.logger)
	}
	if before.Generation != generation {
		return nil, oops.E(oops.CodeConflict, nil, "The workspace connection changed. Reload and try again.")
	}
	row, err := queries.DisconnectSlackDirectoryConnection(ctx, repo.DisconnectSlackDirectoryConnectionParams{OrganizationID: ac.ActiveOrganizationID, ID: id, ExpectedGeneration: generation, Generation: uuid.New()})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not disconnect Slack workspace").LogError(ctx, s.logger)
	}
	memberCount, err := queries.CountSlackDirectorySnapshotMembers(ctx, repo.CountSlackDirectorySnapshotMembersParams{OrganizationID: ac.ActiveOrganizationID, ID: id})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not count retained Slack members").LogError(ctx, s.logger)
	}
	result := s.connectionView(ctx, row)
	result.MemberCount = memberCount
	previous := s.connectionView(ctx, before)
	previous.MemberCount = memberCount
	if err := s.audit.LogSlackDirectoryConnectionDisconnect(ctx, tx, audit.LogSlackDirectoryConnectionEvent{OrganizationID: ac.ActiveOrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ConnectionURN: urn.NewSlackDirectoryConnection(row.ID), ConnectionSnapshotBefore: previous, ConnectionSnapshotAfter: result}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not audit Slack disconnection").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not disconnect Slack workspace").LogError(ctx, s.logger)
	}
	return result, nil
}

// CallbackPath is shared by the raw callback handler and OAuth redirect URI.
// It sits under /rpc so every environment's ingress routes it to the server.
const CallbackPath = "/rpc/slackDirectoryConnections.callback"

type CallbackPayload struct {
	SessionToken *string
	State        string
	Code         *string
	Error        *string
}
type CallbackResult struct {
	Location     string
	CacheControl string
}

// handleCallback keeps every browser outcome on the dashboard, including expired sessions.
func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	login := s.siteURL.ResolveReference(&url.URL{Path: "/login"}).String()
	ctx := r.Context()
	token, ok := contextvalues.GetSessionTokenFromContext(ctx)
	if !ok {
		http.Redirect(w, r, login, http.StatusSeeOther)
		return
	}
	ctx, err := s.auth.Authorize(ctx, token, &security.APIKeyScheme{Name: "session", Scopes: []string{}, RequiredScopes: []string{}})
	if err != nil {
		http.Redirect(w, r, login, http.StatusSeeOther)
		return
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		http.Redirect(w, r, login, http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	result, err := s.Callback(ctx, &CallbackPayload{SessionToken: nil, State: q.Get("state"), Code: conv.PtrEmpty(q.Get("code")), Error: conv.PtrEmpty(q.Get("error"))})
	if err != nil {
		result = s.callbackResult(ac, "unavailable")
	}
	http.Redirect(w, r, result.Location, http.StatusSeeOther)
}
