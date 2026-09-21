package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/management/readmodel"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type JWTAuthenticator struct {
	signer      *sessiontokens.Signer
	store       *platformrepo.Queries
	credentials *CredentialCodec
	issuer      string
	audience    string
}

func NewJWTAuthenticator(signer *sessiontokens.Signer, db *pgxpool.Pool, encryptionClient *encryption.Client, issuer, audience string) (*JWTAuthenticator, error) {
	credentials, err := NewCredentialCodec(encryptionClient)
	if err != nil {
		return nil, fmt.Errorf("create platform MCP credential codec: %w", err)
	}
	return &JWTAuthenticator{
		signer:      signer,
		store:       platformrepo.New(db),
		credentials: credentials,
		issuer:      issuer,
		audience:    audience,
	}, nil
}

func (a *JWTAuthenticator) Authenticate(ctx context.Context, token string) (Principal, error) {
	if a.signer == nil || a.store == nil || a.credentials == nil || a.issuer == "" || a.audience == "" {
		return Principal{}, ErrUnavailable
	}

	claims, err := a.signer.ValidateExactAudience(token, a.audience)
	if err != nil || claims.Issuer != a.issuer {
		return Principal{}, ErrUnauthorized
	}
	subject, err := urn.ParseSessionSubject(claims.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return Principal{}, ErrUnauthorized
	}

	organizationID, err := a.credentials.OrganizationID(accessJTICredential, claims.ID)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	session, err := a.store.GetActivePlatformMCPSessionByJTI(ctx, platformrepo.GetActivePlatformMCPSessionByJTIParams{
		OrganizationID: organizationID,
		Jti:            claims.ID,
	})
	if err != nil {
		return Principal{}, jwtAuthenticationStoreError(err)
	}
	if session.SubjectUrn != subject.String() || session.ClientID != claims.ClientID || session.ConnectionGeneration != session.ActiveGeneration {
		return Principal{}, ErrUnauthorized
	}

	return Principal{
		Surface:        SurfacePlatformMCP,
		UserID:         subject.ID,
		OrganizationID: session.OrganizationID,
		ConnectionID:   session.ConnectionID.String(),
		Generation:     session.ConnectionGeneration.String(),
		ClientID:       session.ClientID,
	}, nil
}

type LiveOrgAdminAuthorizer struct {
	db           *pgxpool.Pool
	engine       *authz.Engine
	dashboardURL *url.URL
}

func jwtAuthenticationStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	return fmt.Errorf("%w: lookup active platform mcp session: %w", ErrUnavailable, err)
}

func NewLiveOrgAdminAuthorizer(db *pgxpool.Pool, engine *authz.Engine) *LiveOrgAdminAuthorizer {
	return &LiveOrgAdminAuthorizer{db: db, engine: engine, dashboardURL: nil}
}

func (a *LiveOrgAdminAuthorizer) WithDashboardURL(dashboardURL *url.URL) *LiveOrgAdminAuthorizer {
	if a != nil && validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		a.dashboardURL = &copyURL
	}
	return a
}

// LiveOrganizationSelector returns active organization memberships. Per-call
// RBAC determines what the user may do after connecting.
type LiveOrganizationSelector struct {
	db         *pgxpool.Pool
	authorizer Authorizer
}

func NewLiveOrganizationSelector(db *pgxpool.Pool, authorizer Authorizer) *LiveOrganizationSelector {
	return &LiveOrganizationSelector{db: db, authorizer: authorizer}
}

func (s *LiveOrganizationSelector) EligibleOrganizations(ctx context.Context, userID string) ([]OrganizationOption, error) {
	if s == nil || s.db == nil || s.authorizer == nil || userID == "" {
		return nil, ErrUnavailable
	}
	organizations, err := organizationsrepo.New(s.db).ListOrganizationsForUser(ctx, pgtype.Text{String: userID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list organizations for Platform MCP selection: %w", err)
	}
	options := make([]OrganizationOption, 0, len(organizations))
	for _, organization := range organizations {
		principal := Principal{UserID: userID, OrganizationID: organization.ID, ConnectionID: "", Generation: "", ClientID: "", Surface: SurfacePlatformMCP}
		if err := s.authorizer.RequireLiveMembership(ctx, principal); err != nil {
			if isAuthorizationDenied(err) {
				continue
			}
			return nil, fmt.Errorf("check organization membership eligibility: %w", err)
		}
		options = append(options, OrganizationOption{ID: organization.ID, Name: organization.Name})
	}
	return options, nil
}

func isAuthorizationDenied(err error) bool {
	if errors.Is(err, ErrForbidden) {
		return true
	}
	var shareable *oops.ShareableError
	return errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden
}

func (a *LiveOrgAdminAuthorizer) RequireLiveMembership(ctx context.Context, principal Principal) error {
	if a == nil || a.db == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return ErrUnavailable
	}
	member, err := organizationsrepo.New(a.db).HasActiveOrganizationUser(ctx, organizationsrepo.HasActiveOrganizationUserParams{
		UserID:         principal.UserID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("check active organization membership: %w", err)
	}
	if !member {
		return ErrForbidden
	}
	return nil
}

// HasLiveOrgAdmin checks current membership and grants without recording a
// denied access challenge. Use it only for capability-shaped presentation; an
// attempted admin operation must still call RequireLiveOrgAdmin.
func (a *LiveOrgAdminAuthorizer) HasLiveOrgAdmin(ctx context.Context, principal Principal) (bool, error) {
	if a == nil || a.db == nil || a.engine == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return false, ErrUnavailable
	}
	if err := a.RequireLiveMembership(ctx, principal); err != nil {
		return false, err
	}
	principals, err := authz.ResolveUserPrincipals(ctx, a.db, principal.OrganizationID, principal.UserID)
	if err != nil {
		return false, fmt.Errorf("resolve live admin principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, a.db, principal.OrganizationID, principals)
	if err != nil {
		return false, fmt.Errorf("load live admin grants: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(grants, authz.Check{
		Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil,
	})
	if err != nil {
		return false, fmt.Errorf("authorize live admin grants: %w", err)
	}
	return allowed, nil
}

func (a *LiveOrgAdminAuthorizer) RequireLiveOrgAdmin(ctx context.Context, principal Principal) error {
	if a == nil || a.db == nil || a.engine == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return ErrUnavailable
	}
	if err := a.RequireLiveMembership(ctx, principal); err != nil {
		return err
	}

	principals, err := authz.ResolveUserPrincipals(ctx, a.db, principal.OrganizationID, principal.UserID)
	if err != nil {
		return fmt.Errorf("resolve live admin principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, a.db, principal.OrganizationID, principals)
	if err != nil {
		return fmt.Errorf("load live admin grants: %w", err)
	}
	if err := a.engine.EvaluateLoadedGrants(ctx, grants, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   principal.OrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return fmt.Errorf("require live org admin: %w", err)
	}
	return nil
}

type PostgresNewModelEligibility struct {
	store *platformrepo.Queries
}

func NewPostgresNewModelEligibility(db *pgxpool.Pool) *PostgresNewModelEligibility {
	return &PostgresNewModelEligibility{store: platformrepo.New(db)}
}

func (e *PostgresNewModelEligibility) EligibleForPlatformMCP(ctx context.Context, organizationID string) (bool, error) {
	if e == nil || e.store == nil || organizationID == "" {
		return false, ErrUnavailable
	}
	eligible, err := e.store.IsPlatformMCPNewModelEligible(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("check Platform MCP new-model eligibility: %w", err)
	}
	return eligible, nil
}

// Lifecycle is the safe management projection for the active organization. It
// deliberately excludes OAuth client, subject, token, JTI, and session values.
type Lifecycle struct {
	DefaultProjectID     string
	MarketplacePublished bool
	Connections          []LifecycleConnection
}

type LifecycleConnection struct {
	ID             string
	AuthorizedAt   *time.Time
	ReauthorizedAt *time.Time
	Ready          bool
}

type PostgresLifecycleStore struct {
	store *platformrepo.Queries
	oauth *PostgresOAuthStore
}

func NewPostgresLifecycleStore(db *pgxpool.Pool) *PostgresLifecycleStore {
	return &PostgresLifecycleStore{store: platformrepo.New(db), oauth: NewPostgresOAuthStore(db)}
}

func (s *PostgresLifecycleStore) GetLifecycle(ctx context.Context, organizationID string) (Lifecycle, error) {
	if s == nil || s.store == nil || organizationID == "" {
		return Lifecycle{}, ErrUnavailable
	}
	row, err := s.store.GetPlatformMCPLifecycle(ctx, organizationID)
	if err != nil {
		return Lifecycle{}, fmt.Errorf("get Platform MCP lifecycle: %w", err)
	}
	connections, err := s.store.ListPlatformMCPConnections(ctx, organizationID)
	if err != nil {
		return Lifecycle{}, fmt.Errorf("list Platform MCP connections: %w", err)
	}
	lifecycle := Lifecycle{
		DefaultProjectID:     uuidString(row.DefaultProjectID),
		MarketplacePublished: row.MarketplacePublished,
		Connections:          make([]LifecycleConnection, 0, len(connections)),
	}
	for _, connection := range connections {
		lifecycle.Connections = append(lifecycle.Connections, LifecycleConnection{
			ID:             connection.ID.String(),
			AuthorizedAt:   timePointer(connection.AuthorizedAt),
			ReauthorizedAt: timePointer(connection.ReauthorizedAt),
			Ready:          connection.Ready,
		})
	}
	return lifecycle, nil
}

func (s *PostgresLifecycleStore) RevokeConnection(ctx context.Context, organizationID, connectionID string, now time.Time) error {
	if s == nil || s.oauth == nil {
		return ErrUnavailable
	}
	return s.oauth.RevokeConnection(ctx, organizationID, connectionID, now)
}

type PostgresReadinessRecorder struct {
	store *platformrepo.Queries
}

func NewPostgresReadinessRecorder(db *pgxpool.Pool) *PostgresReadinessRecorder {
	return &PostgresReadinessRecorder{store: platformrepo.New(db)}
}

func (r *PostgresReadinessRecorder) RecordReady(ctx context.Context, principal Principal, _ time.Time) error {
	if r == nil || r.store == nil || principal.OrganizationID == "" || principal.ConnectionID == "" || principal.Generation == "" {
		return ErrUnavailable
	}
	connectionID, err := uuid.Parse(principal.ConnectionID)
	if err != nil {
		return fmt.Errorf("parse Platform MCP readiness connection id: %w", err)
	}
	generation, err := uuid.Parse(principal.Generation)
	if err != nil {
		return fmt.Errorf("parse Platform MCP readiness generation: %w", err)
	}
	if err := r.store.RecordPlatformMCPConnectionReady(ctx, platformrepo.RecordPlatformMCPConnectionReadyParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
	}); err != nil {
		return fmt.Errorf("record Platform MCP connection readiness: %w", err)
	}
	return nil
}

type PostgresReader struct {
	logger              *slog.Logger
	db                  *pgxpool.Pool
	reader              *readmodel.Reader
	inventory           *platformrepo.Queries
	inventoryCursor     *inventoryCursorCodec
	metadataVersionKey  []byte
	riskReads           *RiskReadService
	riskAnalysisStatus  *RiskAnalysisStatusService
	dataExports         *DataExportReadService
	dataExportMutations *dataExportMutationService
	recentToolCalls     *RecentToolCallReadService
	eventFeed           *EventFeedReadService
	authz               *authz.Engine
	shadowInventory     *ShadowInventoryService
	shadowDecisions     *ShadowDecisionService
	shadowAI            *ShadowAIService
	reviewRequests      MCPReviewRequestService
	reviewRequestBudget OperationBudget
}

func NewPostgresReader(logger *slog.Logger, db *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{
		logger:              logger.With(attr.SlogComponent("platformmcp")),
		db:                  db,
		reader:              readmodel.New(db),
		inventory:           platformrepo.New(db),
		inventoryCursor:     nil,
		metadataVersionKey:  nil,
		riskReads:           nil,
		riskAnalysisStatus:  nil,
		dataExports:         nil,
		dataExportMutations: nil,
		recentToolCalls:     nil,
		eventFeed:           nil,
		authz:               nil,
		shadowInventory:     nil,
		shadowDecisions:     nil,
		shadowAI:            nil,
		reviewRequests:      nil,
		reviewRequestBudget: OperationBudget{Connection: nil, Organization: nil},
	}
}

func (r *PostgresReader) WithAuthorization(engine *authz.Engine) *PostgresReader {
	if r != nil {
		r.authz = engine
	}
	return r
}

// ResolveReviewProject preserves the existing approval-intake policy: asking
// needs no admin or MCP-specific grant, but the explicit project must be one the
// member may read. A failed organization or project boundary is deliberately
// indistinguishable from a missing project.
func (r *PostgresReader) ResolveReviewProject(ctx context.Context, principal Principal, rawProjectID string) (ResolvedProject, error) {
	if r == nil || r.reader == nil || r.authz == nil || principal.OrganizationID == "" {
		return ResolvedProject{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(rawProjectID)
	if err != nil {
		return ResolvedProject{}, oops.E(oops.CodeBadRequest, err, "project_id must be a UUID")
	}
	project, err := r.reader.GetProject(ctx, projectID, principal.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, oops.E(oops.CodeNotFound, err, "project not found")
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve platform MCP review project: %w", err)
	}
	if err := r.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil}); err != nil {
		if isAuthorizationDenied(err) {
			return ResolvedProject{}, oops.E(oops.CodeNotFound, err, "project not found")
		}
		return ResolvedProject{}, err
	}
	return ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}, nil
}

func (r *PostgresReader) WithReviewRequests(service MCPReviewRequestService, budget OperationBudget) *PostgresReader {
	if r != nil && service != nil && budget.valid() {
		r.reviewRequests = service
		r.reviewRequestBudget = budget
	}
	return r
}

func (r *PostgresReader) WithShadowDecisions(service *ShadowDecisionService) *PostgresReader {
	if r != nil && service != nil && service.valid() {
		r.shadowDecisions = service
	}
	return r
}

func (r *PostgresReader) WithShadowInventory(service *ShadowInventoryService) *PostgresReader {
	if r != nil && service != nil && service.valid() {
		r.shadowInventory = service
	}
	return r
}

// WithRiskAnalysisStatus attaches the Watchdog analysis run-state reads. A nil
// or incomplete service leaves the tool served as a stub.
func (r *PostgresReader) WithRiskAnalysisStatus(service *RiskAnalysisStatusService) *PostgresReader {
	if r != nil && service.valid() {
		r.riskAnalysisStatus = service
	}
	return r
}

func (r *PostgresReader) WithShadowAI(service *ShadowAIService) *PostgresReader {
	if r != nil && service.valid() {
		r.shadowAI = service
	}
	return r
}

func (r *PostgresReader) setInventoryCursorKey(keyMaterial string) {
	codec, err := newInventoryCursorCodec(keyMaterial)
	if err == nil {
		r.inventoryCursor = codec
		r.metadataVersionKey = lifecycleMetadataVersionKey(keyMaterial)
	}
	if riskReads, riskErr := newRiskReadService(r.db, keyMaterial); riskErr == nil {
		r.riskReads = riskReads
	}
}

const (
	projectCandidatePageSize = 100
	maxProjectCandidates     = 1000
)

func (r *PostgresReader) ListProjects(ctx context.Context, principal Principal, input ListProjectsInput) (ListProjectsOutput, error) {
	if r.reader == nil || r.authz == nil {
		return ListProjectsOutput{}, ErrUnavailable
	}
	limit := boundedLimit(input.Limit)
	projects := make([]Project, 0, limit+1)
	filtered := false
	exhausted := false
	afterID := uuid.Nil
	candidatesRead := 0

	for len(projects) <= limit && candidatesRead < maxProjectCandidates {
		pageLimit := min(projectCandidatePageSize, maxProjectCandidates-candidatesRead)
		rows, err := r.reader.ListProjectsPage(ctx, principal.OrganizationID, afterID, int32(pageLimit)) // #nosec G115 -- both constants fit in int32.
		if err != nil {
			return ListProjectsOutput{}, fmt.Errorf("list platform mcp project page: %w", err)
		}
		if len(rows) == 0 {
			exhausted = true
			break
		}
		candidatesRead += len(rows)
		afterID = rows[len(rows)-1].ID

		checks := make([]authz.Check, 0, len(rows))
		for _, row := range rows {
			checks = append(checks, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: row.ID.String(), Dimensions: nil})
		}
		allowed, err := r.authz.FindMatched(ctx, checks)
		if err != nil {
			return ListProjectsOutput{}, fmt.Errorf("filter platform mcp project page: %w", err)
		}
		for i, row := range rows {
			if !allowed[i] {
				filtered = true
				continue
			}
			if len(projects) <= limit {
				projects = append(projects, Project{ID: row.ID.String(), Name: row.Name, Slug: row.Slug})
			}
		}
		if len(rows) < pageLimit {
			exhausted = true
			break
		}
	}

	projects, visibleTruncated := boundedRows(projects, limit)
	return ListProjectsOutput{
		Projects:              projects,
		Truncated:             visibleTruncated || !exhausted,
		authorizationFiltered: filtered,
	}, nil
}

func (r *PostgresReader) FindMCP(ctx context.Context, principal Principal, input FindMCPInput) (FindMCPOutput, error) {
	if r.reader == nil || r.inventory == nil || r.inventoryCursor == nil || r.authz == nil {
		return FindMCPOutput{}, ErrUnavailable
	}
	if input.ProjectID != "" && input.ProjectSlug != "" {
		return FindMCPOutput{}, fmt.Errorf("only one of project_id or project_slug may be supplied")
	}
	if input.Readiness != "" && !isInventoryReadinessState(input.Readiness) {
		return FindMCPOutput{}, fmt.Errorf("invalid readiness filter")
	}
	connectionID, generation, err := inventoryConnection(principal)
	if err != nil {
		return FindMCPOutput{}, err
	}
	query := normalizeInventoryQuery(input.Query)
	if query != "" && input.Cursor != "" {
		return FindMCPOutput{}, ErrInventoryCursorInvalid
	}

	projectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	var cursorProject ResolvedProject
	if input.ProjectID != "" || input.ProjectSlug != "" || query == "" {
		cursorProject, err = r.ResolveProjectRead(ctx, principal, input)
		if err != nil {
			return FindMCPOutput{}, err
		}
		projectID = uuid.NullUUID{UUID: cursorProject.ID, Valid: true}
	}

	limit := boundedLimit(input.Limit)
	afterID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if query == "" && input.Cursor != "" {
		cursor, err := r.inventoryCursor.Decode(input.Cursor, principal, cursorProject.ID, query)
		if err != nil {
			return FindMCPOutput{}, err
		}
		afterID = uuid.NullUUID{UUID: cursor, Valid: true}
	}
	if query != "" {
		limit = min(limit, 10)
	}
	allowedMCPIDs, err := r.allowedMCPIDs(ctx, principal.OrganizationID, projectID, afterID, query)
	if err != nil {
		return FindMCPOutput{}, err
	}
	if len(allowedMCPIDs) == 0 {
		return FindMCPOutput{MCPs: []MCP{}, NextCursor: ""}, nil
	}
	rows, err := r.inventory.ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID,
		ConnectionID:   connectionID, ConnectionGeneration: generation,
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		SkipAuthorizationFilter: false, AllowedMcpIds: allowedMCPIDs,
		ProjectID: projectID, AfterMcpID: afterID, QueryText: query, ReadinessState: inventoryText(input.Readiness),
		LimitValue: int32(limit + 1), // #nosec G115 -- boundedLimit caps the value at 100.
	})
	if err != nil {
		r.logger.ErrorContext(ctx, "list platform MCP inventory", attr.SlogError(err))
		return FindMCPOutput{}, fmt.Errorf("%w: inventory could not be read", ErrUnavailable)
	}
	registrationIDs := inventoryRegistrationIDs(rows)
	byRegistration := map[uuid.UUID][]MCPDistribution{}
	if len(registrationIDs) > 0 {
		distributions, err := r.inventory.ListPlatformMCPInventoryDistributions(ctx, platformrepo.ListPlatformMCPInventoryDistributionsParams{
			OrganizationID: principal.OrganizationID,
			ProjectID:      projectID, RegistrationIds: registrationIDs,
		})
		if err != nil {
			r.logger.ErrorContext(ctx, "list platform MCP inventory distributions", attr.SlogError(err))
			return FindMCPOutput{}, fmt.Errorf("%w: inventory could not be read", ErrUnavailable)
		}
		byRegistration = inventoryDistributions(distributions)
	}
	mcps := make([]MCP, 0, len(rows))
	for _, row := range rows {
		mcp := mcpFromInventoryRow(row, byRegistration)
		r.setInventoryVersion(&mcp)
		mcps = append(mcps, mcp)
	}
	if query != "" {
		return FindMCPOutput{MCPs: inventoryQueryResult(mcps, query, limit), NextCursor: ""}, nil
	}
	output := FindMCPOutput{MCPs: mcps, NextCursor: ""}
	if len(output.MCPs) > limit {
		last := output.MCPs[limit-1]
		output.MCPs = output.MCPs[:limit]
		output.NextCursor, err = r.inventoryCursor.Encode(inventoryCursor{
			OrganizationID: principal.OrganizationID,
			Binding:        principalCursorBinding(principal),
			ProjectID:      cursorProject.ID.String(),
			Query:          query,
			AfterMCPID:     last.ID,
		})
		if err != nil {
			return FindMCPOutput{}, err
		}
	}
	return output, nil
}

func (r *PostgresReader) GetMCP(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error) {
	if r == nil || r.reader == nil || r.inventory == nil || r.authz == nil {
		return MCP{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return MCP{}, fmt.Errorf("parse project id: %w", err)
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return MCP{}, fmt.Errorf("parse mcp id: %w", err)
	}
	if err := r.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, mcpID.String(), projectID.String())); err != nil {
		if isAuthorizationDenied(err) {
			return MCP{}, ErrForbidden
		}
		return MCP{}, err
	}
	return r.getMCPInventory(ctx, principal, projectID, mcpID)
}

// GetMCPForDiagnostics reads one MCP after project:read has been enforced. The
// diagnostics projection is project-operational data rather than the MCP's
// configuration, so it deliberately does not require the narrower mcp:read
// grant used by get_mcp.
func (r *PostgresReader) GetMCPForDiagnostics(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error) {
	project, err := r.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: input.ProjectID, ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return MCP{}, err
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return MCP{}, fmt.Errorf("parse mcp id: %w", err)
	}
	return r.getMCPInventory(ctx, principal, project.ID, mcpID)
}

func (r *PostgresReader) getMCPInventory(ctx context.Context, principal Principal, projectID, mcpID uuid.UUID) (MCP, error) {
	if r == nil || r.inventory == nil {
		return MCP{}, ErrUnavailable
	}
	connectionID, generation, err := inventoryConnection(principal)
	if err != nil {
		return MCP{}, err
	}
	row, err := r.inventory.GetPlatformMCPInventoryItem(ctx, platformrepo.GetPlatformMCPInventoryItemParams{
		OrganizationID: principal.OrganizationID,
		ConnectionID:   connectionID, ConnectionGeneration: generation,
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		McpServerID: mcpID, ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return MCP{}, ErrForbidden
	}
	if err != nil {
		return MCP{}, fmt.Errorf("get platform MCP inventory item: %w", err)
	}
	byRegistration := map[uuid.UUID][]MCPDistribution{}
	if row.RegistrationID != uuid.Nil {
		distributions, err := r.inventory.ListPlatformMCPInventoryDistributions(ctx, platformrepo.ListPlatformMCPInventoryDistributionsParams{
			OrganizationID:  principal.OrganizationID,
			ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
			RegistrationIds: []uuid.UUID{row.RegistrationID},
		})
		if err != nil {
			return MCP{}, fmt.Errorf("list platform MCP inventory distributions: %w", err)
		}
		byRegistration = inventoryDistributions(distributions)
	}
	mcp := mcpFromInventoryItem(row, byRegistration)
	r.setInventoryVersion(&mcp)
	return mcp, nil
}

func (r *PostgresReader) setInventoryVersion(mcp *MCP) {
	if r == nil || mcp == nil || mcp.Registration == nil || len(r.metadataVersionKey) == 0 {
		return
	}
	mcp.Version = lifecycleMetadataVersion(r.metadataVersionKey, mcp.ID, mcp.ProjectID, inventoryMCPDisplayName(mcp), mcp.Slug, mcp.Visibility)
}

func inventoryMCPDisplayName(mcp *MCP) string {
	if mcp == nil {
		return ""
	}
	if mcp.Name != "" {
		return mcp.Name
	}
	if mcp.Slug != "" {
		return mcp.Slug
	}
	return mcp.ID
}

const maxInventoryAuthorizationCandidates = 1000

func (r *PostgresReader) allowedMCPIDs(ctx context.Context, organizationID string, projectID, afterID uuid.NullUUID, query string) ([]uuid.UUID, error) {
	servers, err := r.inventory.ListPlatformMCPInventoryAuthorizationCandidatePage(ctx, platformrepo.ListPlatformMCPInventoryAuthorizationCandidatePageParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		AfterMcpID:     afterID,
		QueryText:      query,
		LimitValue:     maxInventoryAuthorizationCandidates + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp authorization candidates: %w", err)
	}
	if len(servers) > maxInventoryAuthorizationCandidates {
		return nil, fmt.Errorf("%w: inventory authorization candidate limit exceeded", ErrUnavailable)
	}
	checks := make([]authz.Check, 0, len(servers))
	for _, server := range servers {
		checks = append(checks, authz.MCPCheck(authz.ScopeMCPRead, server.ID.String(), server.ProjectID.String()))
	}
	matched, err := r.authz.FindMatched(ctx, checks)
	if err != nil {
		return nil, fmt.Errorf("filter platform mcp authorization candidates: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(servers))
	for i, server := range servers {
		if matched[i] {
			ids = append(ids, server.ID)
		}
	}
	return ids, nil
}

// ResolveProjectRead resolves one exact project under the principal's
// organization and enforces the existing project:read scope before callers read
// project-operational data.
func (r *PostgresReader) ResolveProjectRead(ctx context.Context, principal Principal, input FindMCPInput) (ResolvedProject, error) {
	if r == nil || r.reader == nil || r.authz == nil {
		return ResolvedProject{}, ErrUnavailable
	}
	project, err := r.resolveInventoryProject(ctx, principal.OrganizationID, input)
	if err != nil {
		return ResolvedProject{}, err
	}
	if err := r.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: project.ID.String(), Dimensions: nil}); err != nil {
		if isAuthorizationDenied(err) {
			return ResolvedProject{}, ErrForbidden
		}
		return ResolvedProject{}, err
	}
	return project, nil
}

func (r *PostgresReader) resolveInventoryProject(ctx context.Context, organizationID string, input FindMCPInput) (ResolvedProject, error) {
	if input.ProjectID != "" && input.ProjectSlug != "" {
		return ResolvedProject{}, fmt.Errorf("only one of project_id or project_slug may be supplied")
	}
	var projectID uuid.UUID
	var projectName, projectSlug string
	var err error
	switch {
	case input.ProjectID != "":
		projectID, err = uuid.Parse(input.ProjectID)
		if err == nil {
			project, getErr := r.reader.GetProject(ctx, projectID, organizationID)
			err = getErr
			projectName, projectSlug = project.Name, project.Slug
		}
	case input.ProjectSlug != "":
		project, getErr := r.reader.GetProjectBySlug(ctx, input.ProjectSlug, organizationID)
		err = getErr
		projectID, projectName, projectSlug = project.ID, project.Name, project.Slug
	default:
		project, getErr := r.reader.GetProjectBySlug(ctx, "default", organizationID)
		err = getErr
		projectID, projectName, projectSlug = project.ID, project.Name, project.Slug
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, ErrForbidden
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve platform MCP inventory project: %w", err)
	}
	return ResolvedProject{ID: projectID, Name: projectName, Slug: projectSlug}, nil
}

func boundedRows[T any](rows []T, limit int) ([]T, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

func uuidString(value uuid.NullUUID) string {
	if !value.Valid {
		return ""
	}
	return value.UUID.String()
}

func inventoryText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func inventoryConnection(principal Principal) (uuid.NullUUID, uuid.NullUUID, error) {
	if !principal.HasConnection() {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	connectionID, err := uuid.Parse(principal.ConnectionID)
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, fmt.Errorf("parse platform MCP inventory connection: %w", err)
	}
	generation, err := uuid.Parse(principal.Generation)
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, fmt.Errorf("parse platform MCP inventory generation: %w", err)
	}
	return uuid.NullUUID{UUID: connectionID, Valid: true}, uuid.NullUUID{UUID: generation, Valid: true}, nil
}

func normalizeInventoryQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func isInventoryReadinessState(value string) bool {
	if value == "unknown" || value == "unsupported" {
		return true
	}
	return isReadinessState(ReadinessState(value))
}

// inventoryQueryResult relies on ListPlatformMCPInventory ordering exact matches
// before substring candidates. The query intentionally requests one extra row,
// so inspecting the first two rows distinguishes a unique exact match from an
// ambiguous exact result without widening the bounded candidate response.
func inventoryQueryResult(mcps []MCP, query string, limit int) []MCP {
	if len(mcps) == 0 {
		return mcps
	}
	if mcps[0].ID == query || strings.EqualFold(mcps[0].Name, query) || strings.EqualFold(mcps[0].Slug, query) {
		if len(mcps) == 1 || (mcps[1].ID != query && !strings.EqualFold(mcps[1].Name, query) && !strings.EqualFold(mcps[1].Slug, query)) {
			return mcps[:1]
		}
	}
	return mcps[:min(len(mcps), limit)]
}
