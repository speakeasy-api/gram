package builtinmcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Settings struct {
	UserSessionIssuerID    uuid.NullUUID `json:"user_session_issuer_id"`
	UserSessionIssuerBound bool          `json:"user_session_issuer_bound"`
	Hidden                 bool          `json:"hidden"`
	EnvironmentID          uuid.NullUUID `json:"environment_id"`
	Version                int64         `json:"version"`
}

// LoadSettings returns project configuration. Hidden never gates runtime access.
func LoadSettings(ctx context.Context, db repo.DBTX, projectID uuid.UUID, key string) (Settings, error) {
	row, err := repo.New(db).GetSettings(ctx, repo.GetSettingsParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, BuiltinKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{Hidden: false, EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, UserSessionIssuerBound: false, Version: 0}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("load built-in settings: %w", err)
	}
	return Settings{Hidden: row.Hidden, EnvironmentID: row.EnvironmentID, Version: row.Version, UserSessionIssuerID: row.UserSessionIssuerID, UserSessionIssuerBound: row.UserSessionIssuerBound}, nil
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema string `json:"input_schema"`
}
type Server struct {
	UserSessionIssuerID    *string  `json:"user_session_issuer_id,omitempty"`
	UserSessionIssuerBound bool     `json:"user_session_issuer_bound"`
	ID                     string   `json:"id"`
	Key                    string   `json:"key"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	URL                    string   `json:"url"`
	Hidden                 bool     `json:"hidden"`
	EnvironmentID          *string  `json:"environment_id,omitempty"`
	Version                string   `json:"version"`
	Tools                  []Tool   `json:"tools"`
	CredentialVariables    []string `json:"credential_variables"`
}
type Update struct {
	UserSessionIssuerID uuid.NullUUID
	Hidden              bool
	EnvironmentID       uuid.NullUUID
	ExpectedVersion     string
}
type Store struct {
	db    *pgxpool.Pool
	audit *audit.Logger
	hosts *orghost.Resolver
}

func NewStore(db *pgxpool.Pool, auditLogger *audit.Logger, hosts *orghost.Resolver) *Store {
	return &Store{db: db, audit: auditLogger, hosts: hosts}
}

// RequireAdmin bypasses rollout-dependent enforcement and prepared overrides.
// Only an authenticated human with live canonical organization admin membership
// can discover or configure built-ins. Connecting is a separate runtime policy.
func RequireAdmin(ctx context.Context, db repo.DBTX, organizationID string) error {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	ac, hasAuth := contextvalues.GetAuthContext(ctx)
	if !ok || actor.Type != urn.PrincipalTypeUser || !hasAuth || ac == nil || ac.ActiveOrganizationID != organizationID || ac.APIKeyID != "" || contextvalues.IsSupportSession(ctx) {
		return oops.C(oops.CodeForbidden)
	}
	member, err := orgrepo.New(db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: organizationID, UserID: actor.ID})
	if err != nil {
		return fmt.Errorf("read built-in administrator membership: %w", err)
	}
	if !member {
		return oops.C(oops.CodeForbidden)
	}
	role, err := accessrepo.New(db).GetGlobalRoleBySlug(ctx, authz.SystemRoleAdmin)
	if err != nil {
		return oops.C(oops.CodeForbidden)
	}
	if role.Deleted || role.WorkosDeleted {
		return oops.C(oops.CodeForbidden)
	}
	principals, err := authz.ResolveUserPrincipals(ctx, db, organizationID, actor.ID)
	if err != nil {
		return fmt.Errorf("resolve built-in administrator: %w", err)
	}
	wanted := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String()).String()
	for _, principal := range principals {
		if principal.String() == wanted {
			grants, err := authz.LoadGrants(ctx, db, organizationID, principals)
			if err != nil {
				return fmt.Errorf("load built-in administrator grants: %w", err)
			}
			allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil})
			if err != nil {
				return fmt.Errorf("evaluate built-in administrator grants: %w", err)
			}
			if !allowed {
				return oops.C(oops.CodeForbidden)
			}
			return nil
		}
	}
	return oops.C(oops.CodeForbidden)
}

func (s *Store) authorize(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID) error {
	if err := RequireAdmin(ctx, db, organizationID); err != nil {
		return err
	}
	_, err := projectrepo.New(db).GetProjectByIDAndOrganizationID(ctx, projectrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return fmt.Errorf("load built-in project: %w", err)
	}
	return nil
}

func (s *Store) get(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, key string) (Server, error) {
	definition, ok := Lookup(key)
	if !ok {
		return Server{}, oops.C(oops.CodeNotFound)
	}
	settings, err := LoadSettings(ctx, db, projectID, key)
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	if s.hosts == nil {
		return Server{}, oops.E(oops.CodeUnexpected, nil, "built-in MCP host resolver is not configured")
	}
	org, err := orgrepo.New(db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	base := s.hosts.ServerURL(org.DefaultHost)
	if base == nil || !base.IsAbs() || base.Host == "" {
		return Server{}, oops.E(oops.CodeUnexpected, nil, "built-in MCP host resolver has no absolute server URL")
	}
	project, err := projectrepo.New(db).GetProjectByIDAndOrganizationID(ctx, projectrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: organizationID})
	if err != nil {
		return Server{}, fmt.Errorf("load built-in project: %w", err)
	}
	result := Server{ID: ID(projectID, key).String(), Key: key, Name: definition.Name, Description: definition.Description, URL: URL(base, org.Slug, project.Slug, key), Hidden: settings.Hidden, UserSessionIssuerBound: settings.UserSessionIssuerBound, UserSessionIssuerID: nil, EnvironmentID: nil, Version: strconv.FormatInt(settings.Version, 10), Tools: make([]Tool, 0, len(definition.Tools)), CredentialVariables: definition.CredentialVariables}
	if settings.EnvironmentID.Valid {
		value := settings.EnvironmentID.UUID.String()
		result.EnvironmentID = &value
	}
	if settings.UserSessionIssuerID.Valid {
		value := settings.UserSessionIssuerID.UUID.String()
		result.UserSessionIssuerID = &value
	}
	for _, tool := range definition.Tools {
		result.Tools = append(result.Tools, Tool{Name: tool.Name, Description: tool.Description, InputSchema: string(tool.InputSchema)})
	}
	return result, nil
}

func (s *Store) Get(ctx context.Context, organizationID string, projectID uuid.UUID, key string) (Server, error) {
	if err := s.authorize(ctx, s.db, organizationID, projectID); err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	return s.get(ctx, s.db, organizationID, projectID, key)
}

func (s *Store) List(ctx context.Context, organizationID string, projectID uuid.UUID) ([]Server, error) {
	if err := s.authorize(ctx, s.db, organizationID, projectID); err != nil {
		return nil, err
	}
	result := make([]Server, 0, len(All()))
	for _, definition := range All() {
		server, err := s.get(ctx, s.db, organizationID, projectID, definition.Key)
		if err != nil {
			return nil, err
		}
		result = append(result, server)
	}
	return result, nil
}

func (s *Store) Update(ctx context.Context, organizationID string, projectID uuid.UUID, key string, update Update) (Server, error) {
	if _, ok := Lookup(key); !ok {
		return Server{}, oops.C(oops.CodeNotFound)
	}
	version, err := strconv.ParseInt(update.ExpectedVersion, 10, 64)
	if err != nil || version < 0 {
		return Server{}, oops.E(oops.CodeInvalid, nil, "expected_version must be a nonnegative integer")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.authorize(ctx, tx, organizationID, projectID); err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	if update.EnvironmentID.Valid {
		if _, err := envrepo.New(tx).GetEnvironmentByID(ctx, envrepo.GetEnvironmentByIDParams{ID: update.EnvironmentID.UUID, ProjectID: projectID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Server{}, oops.E(oops.CodeInvalid, nil, "environment must belong to this project")
			}
			return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
		}
	}
	before, err := LoadSettings(ctx, tx, projectID, key)
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	if before.Version != version {
		return Server{}, oops.E(oops.CodeConflict, nil, "built-in settings changed; read them again before updating")
	}
	q := repo.New(tx)
	issuerID, issuerBound := before.UserSessionIssuerID, before.UserSessionIssuerBound
	if update.UserSessionIssuerID.Valid {
		if _, err := q.ValidateUserSessionIssuer(ctx, repo.ValidateUserSessionIssuerParams{ID: update.UserSessionIssuerID.UUID, ProjectID: projectID, OrganizationID: organizationID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Server{}, oops.E(oops.CodeInvalid, nil, "user session issuer must be live and belong to this organization and project")
			}
			return Server{}, fmt.Errorf("validate built-in user session issuer: %w", err)
		}
		issuerID, issuerBound = update.UserSessionIssuerID, true
	}
	if version == 0 {
		_, err = q.InsertSettings(ctx, repo.InsertSettingsParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, BuiltinKey: key, Hidden: update.Hidden, EnvironmentID: update.EnvironmentID, UserSessionIssuerID: issuerID, UserSessionIssuerBound: issuerBound})
	} else {
		_, err = q.UpdateSettings(ctx, repo.UpdateSettingsParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, BuiltinKey: key, Hidden: update.Hidden, EnvironmentID: update.EnvironmentID, UserSessionIssuerID: issuerID, UserSessionIssuerBound: issuerBound, ExpectedVersion: version})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, oops.E(oops.CodeConflict, nil, "built-in settings changed; read them again before updating")
	}
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	after, err := LoadSettings(ctx, tx, projectID, key)
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	actor, _ := contextvalues.AuthenticatedActor(ctx)
	if err := s.audit.LogBuiltinMCPUpdate(ctx, tx, organizationID, projectID, ID(projectID, key), key, actor, before, after); err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	result, err := s.get(ctx, tx, organizationID, projectID, key)
	if err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Server{}, fmt.Errorf("manage built-in MCP: %w", err)
	}
	return result, nil
}
