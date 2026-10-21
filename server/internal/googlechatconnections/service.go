// Package googlechatconnections manages hosted Google Chat connections.
package googlechatconnections

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
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/google_chat_connections"
	srv "github.com/speakeasy-api/gram/server/gen/http/google_chat_connections/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bg "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/provider"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/oauth2"
)

const CallbackPath = "/rpc/googleChatConnections.callback"

const (
	// maxSpaceDisplayName matches Google Chat's space display-name limit.
	maxSpaceDisplayName = 128
	// maxCommandPrefix keeps commands and stored trigger configuration bounded.
	maxCommandPrefix = 100
	// maxInvites bounds provider calls within a single setup request.
	maxInvites = 100
)

var spacePattern = regexp.MustCompile(`^spaces/[A-Za-z0-9_-]+$`)

// Service owns Google consent and the organization-scoped connection lifecycle.
type Service struct {
	logger   *slog.Logger
	setup    *AssistantSetup
	db       *pgxpool.Pool
	auth     *auth.Auth
	engine   *authz.Engine
	audit    *audit.Logger
	cache    cache.Cache
	enc      *encryption.Client
	provider *provider.Client
	triggers *bg.App
	site     *url.URL
	tracer   trace.Tracer
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tp trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, engine *authz.Engine, auditLogger *audit.Logger, state cache.Cache, enc *encryption.Client, p *provider.Client, triggers *bg.App, site *url.URL) *Service {
	return &Service{logger: logger, setup: nil, db: db, auth: auth.New(logger, db, sessions, engine), engine: engine, audit: auditLogger, cache: state, enc: enc, provider: p, triggers: triggers, site: site, tracer: tp.Tracer("googlechatconnections")}
}
func Attach(mux goahttp.Muxer, s *Service) {
	endpoints := gen.NewEndpoints(s)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(s.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	mux.Handle("GET", CallbackPath, s.callback)
}
func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, scheme)
}
func (s *Service) authorize(ctx context.Context, mutate bool) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, key := contextvalues.APIKeyAuthorization(ctx); key || contextvalues.IsSupportSession(ctx) {
		return nil, oops.C(oops.CodeForbidden)
	}
	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}
	if mutate && ac.ActiveOrganizationID == constants.DemoOrganizationID {
		return nil, oops.E(oops.CodeForbidden, nil, "The shared demo is read-only")
	}
	return ac, nil
}
func viewConnection(r repo.GoogleChatConnection) *gen.GoogleChatConnection {
	return &gen.GoogleChatConnection{ID: r.ID.String(), Email: r.Email, Status: r.Status, Generation: r.Generation.String(), GrantedScopes: r.GrantedScopes}
}
func viewBinding(r repo.GoogleChatBinding) *gen.GoogleChatBinding {
	return &gen.GoogleChatBinding{ID: r.ID.String(), ConnectionID: r.ConnectionID.String(), ProjectID: r.ProjectID.String(), AssistantID: r.AssistantID.String(), SpaceName: r.SpaceName, DisplayName: r.DisplayName, Prefix: r.Prefix, Status: r.Status, LastError: conv.FromPGText[string](r.LastError), Revision: r.Revision}
}
func viewMember(r repo.GoogleChatMember) *gen.GoogleChatMember {
	return &gen.GoogleChatMember{ID: r.ID.String(), BindingID: r.BindingID.String(), GoogleUserName: r.GoogleUserName, DisplayName: r.DisplayName, UserID: conv.FromPGText[string](r.UserID), Active: r.Active, Revision: r.Revision}
}
func (s *Service) List(ctx context.Context, _ *gen.ListPayload) (*gen.ListResult, error) {
	ac, err := s.authorize(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	q := repo.New(s.db)
	connections, err := q.ListConnections(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not load Google connections")
	}
	bindings, err := q.ListBindings(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not load Google spaces")
	}
	result := &gen.ListResult{Connections: []*gen.GoogleChatConnection{}, Bindings: []*gen.GoogleChatBinding{}, AuthorizationConfigured: s.provider.Ready()}
	for _, v := range connections {
		result.Connections = append(result.Connections, viewConnection(v))
	}
	for _, v := range bindings {
		result.Bindings = append(result.Bindings, viewBinding(v))
	}
	return result, nil
}

type consentState struct {
	OrganizationID string
	UserID         string
	SessionID      string
	Verifier       string
	ExpiresAt      time.Time
}

func stateKey(value string) string {
	h := sha256.Sum256([]byte(value))
	return "google-chat-consent:" + hex.EncodeToString(h[:])
}
func (s *Service) Begin(ctx context.Context, _ *gen.BeginPayload) (*gen.BeginResult, error) {
	ac, err := s.authorize(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	if ac.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if !s.provider.Ready() {
		return nil, oops.E(oops.CodeUnavailable, nil, "Google Chat authorization is not configured")
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("create consent state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier := oauth2.GenerateVerifier()
	challenge := oauth2.S256ChallengeFromVerifier(verifier)
	if err := s.cache.Set(ctx, stateKey(state), consentState{OrganizationID: ac.ActiveOrganizationID, UserID: ac.UserID, SessionID: *ac.SessionID, Verifier: verifier, ExpiresAt: time.Now().Add(10 * time.Minute)}, 10*time.Minute); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not start Google consent")
	}
	return &gen.BeginResult{AuthorizationURL: s.provider.AuthorizationURL(state, challenge)}, nil
}
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	ctx := r.Context()
	session, ok := contextvalues.GetSessionTokenFromContext(ctx)
	if !ok {
		http.Redirect(w, r, s.site.JoinPath("login").String(), http.StatusSeeOther)
		return
	}
	ctx, err := s.auth.Authorize(ctx, session, &security.APIKeyScheme{Name: "session", Scopes: nil, RequiredScopes: nil})
	if err != nil {
		http.Redirect(w, r, s.site.JoinPath("login").String(), http.StatusSeeOther)
		return
	}
	ac, err := s.authorize(ctx, true)
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	result := "authorization_failed"
	query := r.URL.Query()
	var state consentState
	if ac.SessionID != nil && len(query.Get("state")) >= 32 && len(query.Get("state")) <= 256 && s.cache.GetAndDelete(ctx, stateKey(query.Get("state")), &state) == nil && time.Now().Before(state.ExpiresAt) && state.OrganizationID == ac.ActiveOrganizationID && state.UserID == ac.UserID && subtle.ConstantTimeCompare([]byte(state.SessionID), []byte(*ac.SessionID)) == 1 && query.Get("error") == "" && s.provider.Ready() {
		token, identity, err := s.provider.Exchange(ctx, query.Get("code"), state.Verifier)
		if err == nil {
			err = s.saveAuthorization(ctx, ac, token, identity)
		}
		if err == nil {
			result = "connected"
		}
	}
	destination := s.site.JoinPath(ac.OrganizationSlug, "identity")
	q := destination.Query()
	q.Set("google_chat", result)
	q.Set("tab", "google-chat")
	destination.RawQuery = q.Encode()
	http.Redirect(w, r, destination.String(), http.StatusSeeOther)
}
func (s *Service) saveAuthorization(ctx context.Context, ac *contextvalues.AuthContext, token *oauth2.Token, identity provider.Identity) error {
	raw, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode Google tokens: %w", err)
	}
	encrypted, err := s.enc.Encrypt(raw)
	if err != nil {
		return fmt.Errorf("encrypt Google tokens: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start Google connection: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	granted, _ := token.Extra("scope").(string)
	row, err := repo.New(tx).SaveConnection(ctx, repo.SaveConnectionParams{OrganizationID: ac.ActiveOrganizationID, GoogleSubject: identity.Subject, Email: identity.Email, CredentialsEncrypted: conv.ToPGText(encrypted), GrantedScopes: splitScopes(granted)})
	if err != nil {
		return fmt.Errorf("save Google connection: %w", err)
	}
	if err := s.audit.LogGoogleChat(ctx, tx, ac.ActiveOrganizationID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), urn.NewGoogleChatConnection(row.ID), "connect", viewConnection(row)); err != nil {
		return fmt.Errorf("save authorization: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit save authorization: %w", err)
	}
	return nil
}
func (s *Service) userToken(ctx context.Context, org string, id uuid.UUID) (string, error) {
	row, err := repo.New(s.db).GetConnection(ctx, repo.GetConnectionParams{OrganizationID: org, ID: id})
	if err != nil {
		return "", fmt.Errorf("user token: %w", err)
	}
	if row.Status != "connected" || !row.CredentialsEncrypted.Valid {
		return "", errors.New("google connection needs authorization")
	}
	raw, err := s.enc.Decrypt(row.CredentialsEncrypted.String)
	if err != nil {
		return "", errors.New("google credentials are unavailable")
	}
	var token oauth2.Token
	if json.Unmarshal([]byte(raw), &token) != nil {
		return "", errors.New("google credentials are invalid")
	}
	fresh, err := s.provider.Refresh(ctx, &token)
	if err != nil {
		return "", fmt.Errorf("user token: %w", err)
	}
	return fresh.AccessToken, nil
}
func (s *Service) ListSpaces(ctx context.Context, p *gen.ListSpacesPayload) (*gen.ListSpacesResult, error) {
	ac, err := s.authorize(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list spaces: %w", err)
	}
	id, err := uuid.Parse(p.ConnectionID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	token, err := s.userToken(ctx, ac.ActiveOrganizationID, id)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "Reconnect Google to list spaces")
	}
	spaces, err := s.provider.Spaces(ctx, token)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "Could not list Google spaces")
	}
	result := &gen.ListSpacesResult{Spaces: []*gen.GoogleChatSpace{}}
	for _, v := range spaces {
		result.Spaces = append(result.Spaces, &gen.GoogleChatSpace{Name: v.Name, DisplayName: v.DisplayName, ExternalUserAllowed: v.ExternalUserAllowed})
	}
	return result, nil
}
func splitScopes(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Fields(value)
}

func (s *Service) binding(ctx context.Context, org, id string) (repo.GoogleChatBinding, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return repo.GoogleChatBinding{}, oops.C(oops.CodeBadRequest)
	}
	row, err := repo.New(s.db).GetBinding(ctx, repo.GetBindingParams{OrganizationID: org, ID: parsed})
	if err != nil {
		return row, oops.C(oops.CodeNotFound)
	}
	return row, nil
}
func (s *Service) saveBinding(ctx context.Context, ac *contextvalues.AuthContext, b repo.GoogleChatBinding, status, action string) (repo.GoogleChatBinding, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return b, fmt.Errorf("save binding: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	updated, err := repo.New(tx).UpdateBinding(ctx, repo.UpdateBindingParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, ID: b.ID, Revision: b.Revision, TriggerID: b.TriggerID, SubscriptionName: b.SubscriptionName, Status: status, LastError: b.LastError})
	if err != nil {
		return b, oops.E(oops.CodeConflict, err, "The space changed; reload it")
	}
	if err := s.audit.LogGoogleChat(ctx, tx, b.OrganizationID, uuid.NullUUID{UUID: b.ProjectID, Valid: true}, urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), urn.NewGoogleChatBinding(b.ID), action, viewBinding(updated)); err != nil {
		return b, fmt.Errorf("save binding: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return b, fmt.Errorf("commit save binding: %w", err)
	}
	return updated, nil
}
func (s *Service) Bind(ctx context.Context, p *gen.BindPayload) (*gen.GoogleChatBinding, error) {
	if strings.TrimSpace(p.DisplayName) == "" || utf8.RuneCountInString(p.DisplayName) > maxSpaceDisplayName || strings.TrimSpace(p.Prefix) == "" || strings.TrimSpace(p.Prefix) != p.Prefix || utf8.RuneCountInString(p.Prefix) > maxCommandPrefix || len(p.InviteEmails) > maxInvites {
		return nil, oops.E(oops.CodeBadRequest, nil, "Choose a space name, a nonempty command prefix, and at most 100 invitations")
	}
	for _, email := range p.InviteEmails {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			return nil, oops.E(oops.CodeBadRequest, nil, "Enter participant email addresses without display names")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ac, err := s.authorize(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("bind: %w", err)
	}
	connection, err := uuid.Parse(p.ConnectionID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	project, err := uuid.Parse(p.ProjectID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	assistant, err := uuid.Parse(p.AssistantID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	if err := s.engine.Require(ctx, authz.AssistantCheck(authz.ScopeAssistantWrite, assistant.String(), project.String())); err != nil {
		return nil, fmt.Errorf("bind: %w", err)
	}
	token, err := s.userToken(ctx, ac.ActiveOrganizationID, connection)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "Reconnect Google first")
	}
	requestID, err := uuid.Parse(p.RequestID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	q := repo.New(s.db)
	row, err := q.GetBinding(ctx, repo.GetBindingParams{OrganizationID: ac.ActiveOrganizationID, ID: requestID})
	if err == nil {
		if row.AssistantID != assistant || row.ProjectID != project || row.ConnectionID != connection || row.Prefix != p.Prefix {
			return nil, oops.C(oops.CodeConflict)
		}
		if row.Status == "active" {
			return viewBinding(row), nil
		}
		if row.Status == "deleted" {
			return nil, oops.C(oops.CodeConflict)
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("read Google setup attempt: %w", err)
		}
		spaceName := conv.PtrValOr(p.SpaceName, "")
		if spaceName != "" && !spacePattern.MatchString(spaceName) {
			return nil, oops.C(oops.CodeBadRequest)
		}
		if spaceName == "" {
			spaceName = "pending/" + requestID.String()
		}
		row, err = q.CreateBinding(ctx, repo.CreateBindingParams{ID: requestID, OrganizationID: ac.ActiveOrganizationID, ProjectID: project, ConnectionID: connection, AssistantID: assistant, SpaceName: spaceName, DisplayName: p.DisplayName, Prefix: p.Prefix})
		if err != nil {
			return nil, oops.E(oops.CodeConflict, err, "The assistant is unavailable or this space is already connected")
		}
	}
	// The lease prevents two retries from creating competing provider resources.
	acquired, err := s.cache.Add(ctx, "google-chat-setup:"+requestID.String(), 2*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("reserve Google setup: %w", err)
	}
	if !acquired {
		return nil, oops.E(oops.CodeConflict, nil, "This space is connecting; refresh to see its progress")
	}
	defer o11y.NoLogDefer(func() error { return s.cache.Delete(ctx, "google-chat-setup:"+requestID.String()) })
	space := provider.Space{Name: row.SpaceName, DisplayName: row.DisplayName, ExternalUserAllowed: true}
	if strings.HasPrefix(space.Name, "pending/") {
		space, err = s.provider.CreateSpace(ctx, token, p.DisplayName, requestID.String())
	} else {
		space, err = s.provider.GetSpace(ctx, token, space.Name)
	}
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "Could not prepare the space; retry this setup")
	}
	row, err = q.SetBindingSpace(ctx, repo.SetBindingSpaceParams{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, ID: row.ID, SpaceName: space.Name, DisplayName: space.DisplayName})
	if err != nil {
		return nil, fmt.Errorf("save Google space: %w", err)
	}
	for _, email := range p.InviteEmails {
		if email == "" || strings.ContainsAny(email, "/?# ") {
			return nil, oops.C(oops.CodeBadRequest)
		}
		if err := s.provider.AddMember(ctx, token, space.Name, email); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "Could not invite a participant")
		}
	}
	if err := s.provision(ctx, ac, &row, token); err != nil {
		s.logger.WarnContext(ctx, "configure Google Chat binding", attr.SlogError(err))
		code := "setup_failed"
		var api *provider.APIError
		if errors.As(err, &api) && api.Status == http.StatusForbidden {
			code = "google_approval_required"
		}
		row.LastError = conv.ToPGText(code)
		updated, e := s.saveBinding(ctx, ac, row, "error", "setup_failed")
		if e != nil {
			return nil, e
		}
		return viewBinding(updated), nil
	}
	return viewBinding(row), nil
}
func (s *Service) provision(ctx context.Context, ac *contextvalues.AuthContext, b *repo.GoogleChatBinding, token string) error {
	if err := s.provider.AddApplication(ctx, token, b.SpaceName); err != nil {
		return fmt.Errorf("ensure Google app membership: %w", err)
	}
	if err := s.setupAssistant(ctx, *b); err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	if err := s.syncMembers(ctx, *b, token); err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	members, err := repo.New(s.db).ListMembers(ctx, repo.ListMembersParams{OrganizationID: b.OrganizationID, BindingID: b.ID})
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	users := []string{}
	for _, m := range members {
		if m.Active {
			users = append(users, m.GoogleUserName)
		}
	}
	if len(users) == 0 {
		return errors.New("no human members found")
	}
	cfg := s.provider.Config
	pushName := strings.Split(cfg.Topic, "/topics/")[0] + "/subscriptions/chat-" + b.ID.String()
	audience := strings.TrimRight(cfg.ServerURL, "/") + "/google-chat/" + b.ID.String()
	appToken, err := s.provider.HistoryToken(ctx)
	if err != nil {
		return fmt.Errorf("authorize Google Chat events: %w", err)
	}
	subscription, err := s.provider.RestoreSubscription(ctx, appToken, b.SubscriptionName.String, b.SpaceName, time.Now())
	if err != nil {
		return fmt.Errorf("prepare Google subscription: %w", err)
	}
	if err := s.provider.ValidateSubscription(subscription, b.SpaceName); err != nil {
		return fmt.Errorf("validate Google subscription: %w", err)
	}
	b.SubscriptionName = conv.ToPGText(subscription.Name)
	stored, err := s.saveBinding(ctx, ac, *b, "pending", "subscribe")
	if err != nil {
		return err
	}
	*b = stored
	if !b.TriggerID.Valid {
		instance, err := s.triggers.Create(ctx, bg.CreateParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, DefinitionSlug: bg.DefinitionSlugGoogleChat, Name: "Google Chat: " + b.DisplayName, EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, TargetKind: "assistant", TargetRef: b.AssistantID.String(), TargetDisplay: b.DisplayName, Config: map[string]any{"managed_binding_id": b.ID.String(), "space_names": []string{b.SpaceName}, "delivery": map[string]any{"subscription": pushName, "workspace_subscription": subscription.Name, "audience": audience, "service_account_email": cfg.PushServiceAccount, "text_prefix": b.Prefix}}, Status: "active"})
		if err != nil {
			return fmt.Errorf("provision: %w", err)
		}
		b.TriggerID = uuid.NullUUID{UUID: instance.ID, Valid: true}
	} else {
		instance, err := s.triggers.GetInstance(ctx, b.ProjectID, b.TriggerID.UUID)
		if err != nil {
			return fmt.Errorf("provision: %w", err)
		}
		var config map[string]any
		if err := json.Unmarshal(instance.ConfigJson, &config); err != nil {
			return fmt.Errorf("provision: %w", err)
		}
		events, ok := config["delivery"].(map[string]any)
		if !ok {
			return errors.New("invalid managed trigger")
		}
		events["workspace_subscription"] = subscription.Name
		_, err = s.triggers.Update(ctx, bg.UpdateParams{ID: instance.ID, ProjectID: b.ProjectID, DefinitionSlug: instance.DefinitionSlug, Name: instance.Name, EnvironmentID: instance.EnvironmentID, TargetKind: instance.TargetKind, TargetRef: instance.TargetRef, TargetDisplay: instance.TargetDisplay, Config: config, Status: "active"})
		if err != nil {
			return fmt.Errorf("provision: %w", err)
		}
	}
	if err := s.provider.UpsertPushSubscription(ctx, pushName, strings.TrimRight(cfg.ServerURL, "/")+"/rpc/triggers/"+b.TriggerID.UUID.String()+"/webhook", audience, b.SpaceName); err != nil {
		return fmt.Errorf("configure Google delivery: %w", err)
	}
	b.LastError = conv.ToPGTextEmpty("")
	updated, err := s.saveBinding(ctx, ac, *b, "active", "activate")
	*b = updated
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	return nil
}
func (s *Service) syncMembers(ctx context.Context, b repo.GoogleChatBinding, token string) error {
	members, err := s.provider.Members(ctx, token, b.SpaceName)
	if err != nil {
		return fmt.Errorf("sync members: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sync members: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	names := []string{}
	for _, member := range members {
		if member.Member.Type != "HUMAN" || member.State != "JOINED" || member.Member.Anonymous {
			continue
		}
		names = append(names, member.Member.Name)
		if err := q.ObserveMember(ctx, repo.ObserveMemberParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID, GoogleUserName: member.Member.Name, DisplayName: member.Member.DisplayName, Email: member.Member.Email}); err != nil {
			return fmt.Errorf("sync members: %w", err)
		}
	}
	if err := q.MarkMembersAbsent(ctx, repo.MarkMembersAbsentParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID, PresentUsers: names}); err != nil {
		return fmt.Errorf("sync members: %w", err)
	}
	mapped, err := q.AutoMapMembers(ctx, repo.AutoMapMembersParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID})
	if err != nil {
		return fmt.Errorf("match Google identities: %w", err)
	}
	for _, m := range mapped {
		if err := s.audit.LogGoogleChat(ctx, tx, b.OrganizationID, uuid.NullUUID{UUID: b.ProjectID, Valid: true}, urn.NewSystemPrincipal("google-chat-email-match"), urn.NewGoogleChatMember(m.ID), "map", viewMember(m)); err != nil {
			return fmt.Errorf("audit Google identity match: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sync members: %w", err)
	}
	return nil
}
func (s *Service) SyncMembers(ctx context.Context, p *gen.SyncMembersPayload) error {
	ac, err := s.authorize(ctx, true)
	if err != nil {
		return fmt.Errorf("sync members: %w", err)
	}
	b, err := s.binding(ctx, ac.ActiveOrganizationID, p.BindingID)
	if err != nil {
		return fmt.Errorf("sync members: %w", err)
	}
	token, err := s.userToken(ctx, ac.ActiveOrganizationID, b.ConnectionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "Reconnect Google first")
	}
	if err := s.syncMembers(ctx, b, token); err != nil {
		return oops.E(oops.CodeBadRequest, err, "Could not sync space members")
	}
	return nil
}
func (s *Service) ListMembers(ctx context.Context, p *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	ac, err := s.authorize(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	b, err := s.binding(ctx, ac.ActiveOrganizationID, p.BindingID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	rows, err := repo.New(s.db).ListMembers(ctx, repo.ListMembersParams{OrganizationID: b.OrganizationID, BindingID: b.ID})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	result := &gen.ListMembersResult{Members: []*gen.GoogleChatMember{}}
	for _, row := range rows {
		result.Members = append(result.Members, viewMember(row))
	}
	return result, nil
}
func (s *Service) MapMember(ctx context.Context, p *gen.MapMemberPayload) (*gen.GoogleChatMember, error) {
	ac, err := s.authorize(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("map member: %w", err)
	}
	b, err := s.binding(ctx, ac.ActiveOrganizationID, p.BindingID)
	if err != nil {
		return nil, fmt.Errorf("map member: %w", err)
	}
	id, err := uuid.Parse(p.MemberID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	rows, err := repo.New(s.db).ListMembers(ctx, repo.ListMembersParams{OrganizationID: b.OrganizationID, BindingID: b.ID})
	if err != nil {
		return nil, fmt.Errorf("map member: %w", err)
	}
	found := false
	for _, m := range rows {
		if m.ID == id {
			found = true
		}
	}
	if !found {
		return nil, oops.C(oops.CodeNotFound)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("map member: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	row, err := repo.New(tx).MapMember(ctx, repo.MapMemberParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, ID: id, UserID: conv.PtrToPGText(p.UserID), Revision: p.Revision})
	if err != nil {
		return nil, oops.E(oops.CodeConflict, err, "Reload this participant and select an active Speakeasy person")
	}
	if err := s.audit.LogGoogleChat(ctx, tx, b.OrganizationID, uuid.NullUUID{UUID: b.ProjectID, Valid: true}, urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), urn.NewGoogleChatMember(id), "map", viewMember(row)); err != nil {
		return nil, fmt.Errorf("map member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit map member: %w", err)
	}
	return viewMember(row), nil
}
func (s *Service) UpdateBinding(ctx context.Context, p *gen.UpdateBindingPayload) (*gen.GoogleChatBinding, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ac, err := s.authorize(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("update binding: %w", err)
	}
	b, err := s.binding(ctx, ac.ActiveOrganizationID, p.BindingID)
	if err != nil {
		return nil, fmt.Errorf("update binding: %w", err)
	}
	if b.Revision != p.Revision {
		return nil, oops.C(oops.CodeConflict)
	}
	acquired, err := s.cache.Add(ctx, "google-chat-setup:"+b.ID.String(), 2*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("reserve Google space update: %w", err)
	}
	if !acquired {
		return nil, oops.E(oops.CodeConflict, nil, "This space is being updated; refresh to see its progress")
	}
	defer o11y.NoLogDefer(func() error { return s.cache.Delete(ctx, "google-chat-setup:"+b.ID.String()) })
	if p.Action == "repair" || p.Action == "resume" {
		token, err := s.userToken(ctx, ac.ActiveOrganizationID, b.ConnectionID)
		if err != nil {
			return nil, fmt.Errorf("update binding: %w", err)
		}
		if err := s.provision(ctx, ac, &b, token); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "Could not activate the space")
		}
		return viewBinding(b), nil
	}
	if b.TriggerID.Valid {
		if _, err := s.triggers.SetStatus(ctx, b.ProjectID, b.TriggerID.UUID, "paused"); err != nil {
			return nil, fmt.Errorf("update binding: %w", err)
		}
	}
	status := "paused"
	if p.Action == "delete" {
		row, err := s.saveBinding(ctx, ac, b, "deleted", p.Action)
		if err != nil {
			return nil, err
		}
		if err := s.provider.DeletePushSubscription(ctx, b.ID.String()); err != nil {
			s.logger.WarnContext(ctx, "remove deleted Google delivery", attr.SlogError(err))
		}
		if b.SubscriptionName.Valid {
			token, err := s.provider.HistoryToken(ctx)
			if err == nil {
				err = s.provider.DeleteSubscription(ctx, token, b.SubscriptionName.String)
			}
			if err != nil {
				s.logger.WarnContext(ctx, "remove deleted Google subscription", attr.SlogError(err))
			}
		}
		return viewBinding(row), nil
	} else if p.Action != "pause" {
		return nil, oops.C(oops.CodeBadRequest)
	}
	row, err := s.saveBinding(ctx, ac, b, status, p.Action)
	if err != nil {
		return nil, fmt.Errorf("update binding: %w", err)
	}
	return viewBinding(row), nil
}
func (s *Service) Disconnect(ctx context.Context, p *gen.DisconnectPayload) error {
	ac, err := s.authorize(ctx, true)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.C(oops.CodeBadRequest)
	}
	generation, err := uuid.Parse(p.Generation)
	if err != nil {
		return oops.C(oops.CodeBadRequest)
	}
	// Local revocation is atomic and never depends on Google being available.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Google disconnect: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	row, err := q.DisconnectConnection(ctx, repo.DisconnectConnectionParams{OrganizationID: ac.ActiveOrganizationID, ID: id, Generation: generation})
	if err != nil {
		return oops.C(oops.CodeConflict)
	}
	bindings, err := q.DisconnectBindings(ctx, repo.DisconnectBindingsParams{OrganizationID: ac.ActiveOrganizationID, ConnectionID: id})
	if err != nil {
		return fmt.Errorf("disconnect Google spaces: %w", err)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	for _, binding := range bindings {
		if err := s.audit.LogGoogleChat(ctx, tx, ac.ActiveOrganizationID, uuid.NullUUID{UUID: binding.ProjectID, Valid: true}, actor, urn.NewGoogleChatBinding(binding.ID), "disconnect", viewBinding(binding)); err != nil {
			return fmt.Errorf("audit binding disconnect: %w", err)
		}
	}
	if err := s.audit.LogGoogleChat(ctx, tx, ac.ActiveOrganizationID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, actor, urn.NewGoogleChatConnection(id), "disconnect", viewConnection(row)); err != nil {
		return fmt.Errorf("audit connection disconnect: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Google disconnect: %w", err)
	}
	for _, binding := range bindings {
		if err := s.provider.DeletePushSubscription(ctx, binding.ID.String()); err != nil {
			s.logger.WarnContext(ctx, "remove disconnected Google delivery", attr.SlogError(err))
		}
		if binding.SubscriptionName.Valid {
			token, err := s.provider.HistoryToken(ctx)
			if err == nil {
				err = s.provider.DeleteSubscription(ctx, token, binding.SubscriptionName.String)
			}
			if err != nil {
				s.logger.WarnContext(ctx, "remove disconnected Google subscription", attr.SlogError(err))
			}
		}
		if binding.TriggerID.Valid {
			if _, err := s.triggers.SetStatus(ctx, binding.ProjectID, binding.TriggerID.UUID, "paused"); err != nil {
				s.logger.WarnContext(ctx, "pause disconnected Google trigger", attr.SlogError(err))
			}
		}
	}
	return nil
}

// RefreshDeliveryMembers refreshes provider observations for an already-active delivery binding.
func (s *Service) RefreshDeliveryMembers(ctx context.Context, project, id uuid.UUID) error {
	b, err := repo.New(s.db).GetManagedBinding(ctx, repo.GetManagedBindingParams{ID: id, ProjectID: project})
	if err != nil {
		return fmt.Errorf("resolve active Google binding: %w", err)
	}
	token, err := s.userToken(ctx, b.OrganizationID, b.ConnectionID)
	if err != nil {
		return err
	}
	return s.syncMembers(ctx, b, token)
}
