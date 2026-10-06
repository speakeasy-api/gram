package oktaserversuggestions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/http/okta_server_suggestions/server"
	srv "github.com/speakeasy-api/gram/server/gen/okta_server_suggestions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaserversuggestions/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Suggestion states.
const (
	StateOpen      = "open"
	StateDismissed = "dismissed"
	StateInstalled = "installed"
)

// defaultXAASignOnModes apply when the catalog entry omits xaaSignOnModes. An
// explicit empty list means no mode supports Cross App Access.
var defaultXAASignOnModes = []string{"SAML_2_0", "OPENID_CONNECT"}

// liveStatuses are the connection statuses whose snapshot is served.
var liveStatuses = []string{identityproviderconnections.StatusVerified, identityproviderconnections.StatusDegraded}

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	auth      *auth.Auth
	authz     *authz.Engine
	audit     *audit.Logger
	features  feature.Provider
	validator *mcpregistry.Validator
}

var (
	_ srv.Service = (*Service)(nil)
	_ srv.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	features feature.Provider,
	validator *mcpregistry.Validator,
) *Service {
	logger = logger.With(attr.SlogComponent("oktaserversuggestions"))
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/oktaserversuggestions"),
		logger:    logger,
		db:        db,
		auth:      auth.New(logger, db, sessionManager, authzEngine),
		authz:     authzEngine,
		audit:     auditLogger,
		features:  features,
		validator: validator,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := srv.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	gen.Mount(
		mux,
		gen.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// authorize runs RBAC and the rollout flag; a mutation additionally refuses
// support sessions and non-principal API keys.
func (s *Service) authorize(ctx context.Context, mutation bool) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}
	if mutation {
		if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "server suggestions cannot be changed with an API key").LogWarn(ctx, logger)
		}
		if contextvalues.IsSupportSession(ctx) {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "server suggestions cannot be changed from a support session").LogWarn(ctx, logger)
		}
	}
	if err := s.requireEnabled(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, logger, err
	}
	return authCtx, logger, nil
}

// requireEnabled checks the rollout flag; a lookup failure reads as unavailable, never forbidden.
func (s *Service) requireEnabled(ctx context.Context, logger *slog.Logger, organizationID string) error {
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "okta connections availability could not be determined").LogError(ctx, logger)
	}
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagOktaConnections, organizationID, feature.OrgProjectGroups(org.Slug, ""))
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "okta connections availability could not be determined").LogError(ctx, logger)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "okta connections are not enabled for this organization")
	}
	return nil
}

// actor is the canonical principal for the audit log; a principal credential
// carries no user id, so its URN stands in.
func actor(ctx context.Context, authCtx *contextvalues.AuthContext) urn.Principal {
	if p, ok := contextvalues.AuthenticatedActor(ctx); ok {
		return p
	}
	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
}

func (s *Service) List(ctx context.Context, payload *srv.ListPayload) (*srv.ListOktaServerSuggestionsResult, error) {
	authCtx, logger, err := s.authorize(ctx, false)
	if err != nil {
		return nil, err
	}
	snap, err := s.load(ctx, logger, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	return snap.result(payload.IncludeAll), nil
}

func (s *Service) Dismiss(ctx context.Context, payload *srv.DismissPayload) (*srv.OktaServerSuggestion, error) {
	return s.mutate(ctx, payload.RegistryEntryID, true)
}

func (s *Service) Restore(ctx context.Context, payload *srv.RestorePayload) (*srv.OktaServerSuggestion, error) {
	return s.mutate(ctx, payload.RegistryEntryID, false)
}

// mutate records or clears a dismissal for a suggestion the organization is
// currently shown, in one transaction with its audit row. The precondition
// runs before any write, so a missing connection changes nothing.
func (s *Service) mutate(ctx context.Context, rawID string, dismiss bool) (*srv.OktaServerSuggestion, error) {
	authCtx, logger, err := s.authorize(ctx, true)
	if err != nil {
		return nil, err
	}
	entryID, err := uuid.Parse(rawID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid registry entry id").LogWarn(ctx, logger)
	}
	logger = logger.With(attr.SlogRegistryEntryID(entryID.String()))
	orgID := authCtx.ActiveOrganizationID
	snap, err := s.load(ctx, logger, orgID)
	if err != nil {
		return nil, err
	}
	before := snap.find(entryID)
	if before == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "suggestion not found")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin suggestion update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	if dismiss {
		row, err := q.UpsertDismissal(ctx, repo.UpsertDismissalParams{OrganizationID: orgID, RegistryEntryID: entryID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "dismiss server suggestion").LogError(ctx, logger)
		}
		snap.dismissed[entryID] = row.CreatedAt.Time
	} else {
		if _, err := q.DeleteDismissal(ctx, repo.DeleteDismissalParams{OrganizationID: orgID, RegistryEntryID: entryID}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "restore server suggestion").LogError(ctx, logger)
		}
		delete(snap.dismissed, entryID)
	}
	after := snap.find(entryID)

	event := audit.LogOktaServerSuggestionEvent{
		OrganizationID:   orgID,
		Actor:            actor(ctx, authCtx),
		ActorDisplayName: nil,
		ActorSlug:        nil,
		SuggestionURN:    urn.NewOktaServerSuggestion(entryID),
		ServerName:       after.ServerName,
		SnapshotBefore:   auditSnapshot(before),
		SnapshotAfter:    auditSnapshot(after),
	}
	if dismiss {
		err = s.audit.LogOktaServerSuggestionDismiss(ctx, tx, event)
	} else {
		err = s.audit.LogOktaServerSuggestionRestore(ctx, tx, event)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log suggestion update").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit suggestion update").LogError(ctx, logger)
	}
	return after, nil
}

func auditSnapshot(view *srv.OktaServerSuggestion) *audit.OktaServerSuggestionSnapshot {
	ids := make([]string, 0, len(view.OktaApplications))
	for _, app := range view.OktaApplications {
		ids = append(ids, app.OktaAppID)
	}
	return &audit.OktaServerSuggestionSnapshot{RegistryEntryID: view.RegistryEntryID, ServerName: view.ServerName, OktaAppIDs: ids, State: view.State}
}

// record is the slice of a catalog entry the suggestion needs, decoded with
// exact keys rather than case-insensitive struct matching.
type record struct {
	// name is the catalog server name, the registry specifier.
	name string
	// title is the optional display title.
	title string
	// description is the catalog description.
	description string
	// documentationURL comes from the catalog metadata namespace.
	documentationURL string
	// iconURL is the entry's first icon.
	iconURL string
	// supportsDCR comes from the catalog metadata namespace.
	supportsDCR bool
	// remotes are the endpoints in catalog order.
	remotes []*srv.OktaServerSuggestionRemote
	// mapping is the Okta namespace that matched.
	mapping mcpregistry.OktaMapping
}

// httpsURL keeps a URL the dashboard may load or install: absolute HTTPS
// with a host and no userinfo.
func httpsURL(src string) string {
	u, err := url.Parse(src)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return src
}

func decodeRecord(data []byte) (record, error) {
	var rec record
	var root, server, meta, catalog map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return rec, fmt.Errorf("decode record: %w", err)
	}
	if err := json.Unmarshal(root["server"], &server); err != nil {
		return rec, fmt.Errorf("decode server: %w", err)
	}
	str := func(m map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(m[key], &s)
		return s
	}
	boolean := func(m map[string]json.RawMessage, key string) bool {
		var b bool
		_ = json.Unmarshal(m[key], &b)
		return b
	}
	rec.name, rec.title, rec.description = str(server, "name"), str(server, "title"), str(server, "description")
	var remotes []map[string]json.RawMessage
	if raw, ok := server["remotes"]; ok {
		if err := json.Unmarshal(raw, &remotes); err != nil {
			return rec, fmt.Errorf("decode remotes: %w", err)
		}
	}
	rec.remotes = make([]*srv.OktaServerSuggestionRemote, 0, len(remotes))
	for _, r := range remotes {
		var headers []map[string]json.RawMessage
		if raw, ok := r["headers"]; ok {
			if err := json.Unmarshal(raw, &headers); err != nil {
				return rec, fmt.Errorf("decode remote headers: %w", err)
			}
		}
		// The record contract allows plain HTTP; the install flow must not be
		// handed one.
		if httpsURL(str(r, "url")) == "" {
			continue
		}
		remote := &srv.OktaServerSuggestionRemote{Type: str(r, "type"), URL: str(r, "url"), Headers: make([]*srv.OktaServerSuggestionRemoteHeader, 0, len(headers))}
		for _, h := range headers {
			remote.Headers = append(remote.Headers, &srv.OktaServerSuggestionRemoteHeader{
				Name:        str(h, "name"),
				Description: conv.PtrEmpty(str(h, "description")),
				IsRequired:  boolean(h, "isRequired"),
				IsSecret:    boolean(h, "isSecret"),
			})
		}
		rec.remotes = append(rec.remotes, remote)
	}
	var icons []map[string]json.RawMessage
	if raw, ok := server["icons"]; ok {
		_ = json.Unmarshal(raw, &icons)
	}
	if len(icons) > 0 {
		rec.iconURL = httpsURL(str(icons[0], "src"))
	}
	if raw, ok := root["_meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	if raw, ok := meta["com.speakeasy.ai/catalog"]; ok {
		_ = json.Unmarshal(raw, &catalog)
		rec.documentationURL = str(catalog, "documentationUrl")
		rec.supportsDCR = boolean(catalog, "supportsDcr")
	}
	mapping, err := mcpregistry.ParseOktaMapping(data)
	if err != nil {
		return rec, fmt.Errorf("decode okta mapping: %w", err)
	}
	rec.mapping = mapping
	return rec, nil
}

// suggestion groups every matching application under its entry.
type suggestion struct {
	id     uuid.UUID
	record record
	apps   []*srv.OktaServerSuggestionApplication
}

// snapshot is everything one request derives the suggestions from.
type snapshot struct {
	connection  repo.GetLiveConnectionRow
	suggestions []suggestion
	dismissed   map[uuid.UUID]time.Time
	installed   map[string]struct{}
}

func (s *Service) load(ctx context.Context, logger *slog.Logger, organizationID string) (*snapshot, error) {
	q := repo.New(s.db)
	connection, err := q.GetLiveConnection(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "connect an identity provider first")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load identity provider connection").LogError(ctx, logger)
	}
	if !slices.Contains(liveStatuses, connection.Status) {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "verify the identity provider connection first")
	}
	rows, err := q.ListMappedApplications(ctx, repo.ListMappedApplicationsParams{OrganizationID: organizationID, IdentityProviderConnectionID: connection.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mapped applications").LogError(ctx, logger)
	}
	ids := make([]uuid.UUID, 0)
	for _, row := range rows {
		if !slices.Contains(ids, row.RegistryEntryID) {
			ids = append(ids, row.RegistryEntryID)
		}
	}
	records := make(map[uuid.UUID]record, len(ids))
	if len(ids) > 0 {
		entries, err := q.ListMappedEntries(ctx, ids)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load catalog entries").LogError(ctx, logger)
		}
		for _, e := range entries {
			rec, ok := s.decode(ctx, logger, e.ID, e.Data)
			if ok {
				records[e.ID] = rec
			}
		}
	}
	snap := &snapshot{connection: connection, suggestions: group(rows, records), dismissed: make(map[uuid.UUID]time.Time), installed: make(map[string]struct{})}

	dismissals, err := q.ListDismissals(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list dismissed suggestions").LogError(ctx, logger)
	}
	for _, d := range dismissals {
		snap.dismissed[d.RegistryEntryID] = d.CreatedAt.Time
	}
	urls := make([]string, 0)
	for _, sg := range snap.suggestions {
		for _, r := range sg.record.remotes {
			urls = append(urls, normalizeURL(r.URL))
		}
	}
	if len(urls) > 0 {
		found, err := q.ListInstalledRemoteURLs(ctx, repo.ListInstalledRemoteURLsParams{OrganizationID: organizationID, Urls: urls})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list installed remote servers").LogError(ctx, logger)
		}
		for _, u := range found {
			snap.installed[u] = struct{}{}
		}
	}
	return snap, nil
}

// decode skips records that fail the stored contract, as discovery does; a
// staff-curated entry that trips this is a catalog bug worth a warning.
func (s *Service) decode(ctx context.Context, logger *slog.Logger, id uuid.UUID, data []byte) (record, bool) {
	var zero record
	if issues := s.validator.ValidateStored(data); len(issues) > 0 {
		paths := make([]string, 0, len(issues))
		for _, issue := range issues {
			paths = append(paths, issue.Path)
		}
		logger.WarnContext(ctx, "skipping invalid catalog entry", attr.SlogRegistryEntryID(id.String()), attr.SlogRegistryInvalidPaths(paths))
		return zero, false
	}
	rec, err := decodeRecord(data)
	if err != nil {
		logger.WarnContext(ctx, "skipping undecodable catalog entry", attr.SlogRegistryEntryID(id.String()), attr.SlogError(err))
		return zero, false
	}
	return rec, true
}

// group folds the per-application rows into one suggestion per entry, in
// query order, dropping entries whose record was skipped or that list no
// remote to install.
func group(rows []repo.ListMappedApplicationsRow, records map[uuid.UUID]record) []suggestion {
	suggestions := make([]suggestion, 0)
	index := make(map[uuid.UUID]int)
	for _, row := range rows {
		rec, ok := records[row.RegistryEntryID]
		if !ok || len(rec.remotes) == 0 {
			continue
		}
		i, ok := index[row.RegistryEntryID]
		if !ok {
			i = len(suggestions)
			index[row.RegistryEntryID] = i
			suggestions = append(suggestions, suggestion{id: row.RegistryEntryID, record: rec, apps: nil})
		}
		modes := rec.mapping.XAASignOnModes
		if modes == nil {
			modes = defaultXAASignOnModes
		}
		suggestions[i].apps = append(suggestions[i].apps, &srv.OktaServerSuggestionApplication{
			OktaAppID:        row.OktaAppID,
			Label:            row.Label,
			Name:             row.Name,
			SignOnMode:       row.SignOnMode,
			XaaSupported:     slices.Contains(modes, row.SignOnMode),
			UserAssignments:  int(row.UserAssignments),
			GroupAssignments: int(row.GroupAssignments),
		})
	}
	return suggestions
}

// normalizeURL drops the query string, fragment, and trailing slash, matching
// ListInstalledRemoteURLs.
func normalizeURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimRight(u, "/")
}

func (snap *snapshot) result(includeAll bool) *srv.ListOktaServerSuggestionsResult {
	result := &srv.ListOktaServerSuggestionsResult{
		Suggestions:  make([]*srv.OktaServerSuggestion, 0, len(snap.suggestions)),
		OpenCount:    0,
		TotalCount:   len(snap.suggestions),
		ConnectionID: conv.PtrEmpty(snap.connection.ID.String()),
		SnapshotAt:   nil,
	}
	if snap.connection.ApplicationsSyncedAt.Valid {
		result.SnapshotAt = conv.PtrEmpty(snap.connection.ApplicationsSyncedAt.Time.UTC().Format(time.RFC3339))
	}
	for _, sg := range snap.suggestions {
		view := snap.build(sg)
		if view.State == StateOpen {
			result.OpenCount++
		} else if !includeAll {
			continue
		}
		result.Suggestions = append(result.Suggestions, view)
	}
	slices.SortStableFunc(result.Suggestions, func(a, b *srv.OktaServerSuggestion) int {
		if (a.State == StateOpen) != (b.State == StateOpen) {
			if a.State == StateOpen {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ServerName, b.ServerName)
	})
	return result
}

func (snap *snapshot) find(id uuid.UUID) *srv.OktaServerSuggestion {
	for _, sg := range snap.suggestions {
		if sg.id == id {
			return snap.build(sg)
		}
	}
	return nil
}

func (snap *snapshot) build(sg suggestion) *srv.OktaServerSuggestion {
	view := &srv.OktaServerSuggestion{
		RegistryEntryID:  sg.id.String(),
		ServerName:       sg.record.name,
		Title:            conv.PtrEmpty(sg.record.title),
		Description:      sg.record.description,
		DocumentationURL: conv.PtrEmpty(sg.record.documentationURL),
		IconURL:          conv.PtrEmpty(sg.record.iconURL),
		SupportsDcr:      sg.record.supportsDCR,
		Remotes:          sg.record.remotes,
		OktaApplications: sg.apps,
		XaaIssuer:        conv.PtrEmpty(sg.record.mapping.XAAIssuer),
		State:            StateOpen,
		DismissedAt:      nil,
		InstalledUrls:    []string{},
	}
	for _, r := range sg.record.remotes {
		if _, ok := snap.installed[normalizeURL(r.URL)]; ok {
			view.InstalledUrls = append(view.InstalledUrls, r.URL)
		}
	}
	// A server the organization already runs outranks a dismissal: the
	// suggestion is moot either way, and installed says why.
	at, dismissed := snap.dismissed[sg.id]
	switch {
	case len(view.InstalledUrls) > 0:
		view.State = StateInstalled
	case dismissed:
		view.State = StateDismissed
	}
	if dismissed {
		view.DismissedAt = conv.PtrEmpty(at.UTC().Format(time.RFC3339))
	}
	return view
}
