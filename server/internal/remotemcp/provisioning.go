package remotemcp

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionbindings "github.com/speakeasy-api/gram/server/internal/usersessions/bindings"
)

const dashboardRemoteMCPInitialVisibility = "disabled"

type remoteMCPSourceInput struct {
	Name          *string
	URL           string
	TransportType string
}

// RemoteMCPProvisioningService owns the transactional source-plus-linked-server
// materialization shared by dashboard and future Platform MCP adapters. Surface
// policy is closed and selected by server composition, never by a request.
type RemoteMCPProvisioningService struct {
	db         *pgxpool.Pool
	policy     *guardian.Policy
	audit      *audit.Logger
	iconSetter mcpservers.DefaultServerIconSetter
}

type DashboardRemoteMCPProvisioningInput struct {
	Name                *string
	URL                 string
	TransportType       string
	UserSessionIssuerID *string
}

type RemoteMCPProvisioningResult struct {
	RemoteMCPServer repo.RemoteMcpServer
	MCPServer       mcpserversrepo.McpServer
}

func NewRemoteMCPProvisioningService(db *pgxpool.Pool, policy *guardian.Policy, auditLogger *audit.Logger, iconSetter mcpservers.DefaultServerIconSetter) *RemoteMCPProvisioningService {
	if auditLogger == nil {
		auditLogger = audit.NewLogger()
	}
	return &RemoteMCPProvisioningService{db: db, policy: policy, audit: auditLogger, iconSetter: iconSetter}
}

// ProvisionDashboardRemoteMCP preserves the dashboard workflow's disabled
// initial visibility while atomically creating the remote source, its linked
// MCP server, and the required user-session issuer. OAuth auto-configuration
// and best-effort endpoint creation remain outside this core transaction.
func (s *RemoteMCPProvisioningService) ProvisionDashboardRemoteMCP(ctx context.Context, authCtx *contextvalues.AuthContext, input DashboardRemoteMCPProvisioningInput) (RemoteMCPProvisioningResult, error) {
	if s == nil || s.db == nil || s.policy == nil || s.audit == nil || authCtx == nil || authCtx.ProjectID == nil || input.URL == "" || !dashboardRemoteMCPTransportSupported(input.TransportType) {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeBadRequest, nil, "invalid dashboard remote MCP provisioning input")
	}
	if _, err := proxy.ValidateRemoteMCPURL(ctx, s.policy, input.URL); err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeBadRequest, err, "invalid url")
	}

	remoteName := normalizedRemoteMCPSourceName(input.Name)
	displayName := remoteMCPDisplayName(input.URL, remoteName)
	userSessionIssuerID, err := conv.PtrToNullUUID(input.UserSessionIssuerID)
	if err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "begin dashboard remote MCP provisioning")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := authclientbackfill.LockEMAWriterScope(ctx, tx, authCtx.ActiveOrganizationID, []uuid.UUID{*authCtx.ProjectID}); err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "lock provisioning writer scope")
	}
	if err := requireLiveProjectForActiveOrganization(ctx, tx, authCtx); err != nil {
		return RemoteMCPProvisioningResult{}, err
	}
	remote, err := createRemoteMCPSource(ctx, tx, s.audit, authCtx, remoteMCPSourceInput{
		Name:          input.Name,
		URL:           input.URL,
		TransportType: input.TransportType,
	})
	if err != nil {
		return RemoteMCPProvisioningResult{}, err
	}

	mcpServer, err := mcpservers.CreateMCPServerInTransaction(ctx, tx, s.audit, mcpservers.MCPServerTransactionInput{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		ActorUserID:           authCtx.UserID,
		ActorEmail:            authCtx.Email,
		Name:                  displayName,
		Visibility:            dashboardRemoteMCPInitialVisibility,
		NetworkAccessMode:     networkaccess.ModePublicOnly,
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   userSessionIssuerID,
		RemoteMCPServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMCPServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMCPServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		if errors.Is(err, usersessionbindings.ErrNotFound) {
			return RemoteMCPProvisioningResult{}, oops.E(oops.CodeNotFound, err, "user session issuer not found")
		}
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "materialize remote-backed MCP server")
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "commit dashboard remote MCP provisioning")
	}
	if s.iconSetter != nil {
		s.iconSetter.ScheduleDefaultRemoteServerIcon(ctx, *authCtx.ProjectID, mcpServer.ID, remote.ID)
	}

	return RemoteMCPProvisioningResult{RemoteMCPServer: remote, MCPServer: mcpServer}, nil
}

func dashboardRemoteMCPTransportSupported(transportType string) bool {
	return transportType == "streamable-http" || transportType == "sse"
}

func normalizedRemoteMCPSourceName(name *string) pgtype.Text {
	if name == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	trimmed := strings.TrimSpace(*name)
	return pgtype.Text{String: trimmed, Valid: trimmed != ""}
}

// remoteMCPDisplayName is the name the server is shown with: the one given,
// or else one read from its address.
func remoteMCPDisplayName(rawURL string, name pgtype.Text) string {
	if name.Valid {
		return name.String
	}
	if readable := readableServiceName(rawURL); readable != "" {
		return readable
	}
	return strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
}

// serviceHostPrefixes are leading host labels that name a role, not the
// service.
var serviceHostPrefixes = map[string]bool{
	"mcp": true, "api": true, "www": true, "app": true, "auth": true, "access": true,
	"login": true, "sso": true, "oauth": true, "id": true, "accounts": true,
}

// secondLevelSuffixes precede a two-letter country code in public suffixes
// such as co.uk.
var secondLevelSuffixes = map[string]bool{"co": true, "com": true, "org": true, "net": true, "ac": true, "gov": true}

// readableServiceName reads the service's name from a URL's host:
// https://mcp.ramp.com/mcp reads "Ramp" and mcp.acme-wiki.example reads
// "Acme Wiki". Hosts that cannot be read that way, such as IP addresses and
// localhost, come back as they are; an unparsable URL gives "". The dashboard
// suggests names with the same rules (readableServiceNameFromUrl).
func readableServiceName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(parsed.Hostname(), ".")
	if host == "" {
		return ""
	}
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if len(labels) < 2 || strings.Trim(last, "0123456789") == "" {
		return host
	}
	suffix := 1
	if len(labels) >= 3 && len(last) == 2 && secondLevelSuffixes[labels[len(labels)-2]] {
		suffix = 2
	}
	rest := labels[:len(labels)-suffix]
	for len(rest) > 1 && serviceHostPrefixes[rest[0]] {
		rest = rest[1:]
	}
	words := strings.FieldsFunc(rest[len(rest)-1], func(r rune) bool { return r == '-' || r == '_' })
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// createRemoteMCPSource owns the source row, its derived identifier, and the
// matching audit event. Both dashboard workflows use it so their source records
// cannot diverge while the atomic workflow adds its linked MCP server.
func createRemoteMCPSource(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, input remoteMCPSourceInput) (repo.RemoteMcpServer, error) {
	if tx == nil || auditLogger == nil || authCtx == nil || authCtx.ProjectID == nil {
		return repo.RemoteMcpServer{}, oops.E(oops.CodeBadRequest, nil, "invalid remote MCP source input")
	}
	remoteID, err := uuid.NewV7()
	if err != nil {
		return repo.RemoteMcpServer{}, oops.E(oops.CodeUnexpected, err, "generate remote MCP server ID")
	}
	remoteSlug, err := conv.URLBackedSlug(input.URL, remoteID)
	if err != nil {
		return repo.RemoteMcpServer{}, oops.E(oops.CodeUnexpected, err, "compute remote MCP server slug")
	}
	remote, err := repo.New(tx).CreateServer(ctx, repo.CreateServerParams{
		ID:            remoteID,
		ProjectID:     *authCtx.ProjectID,
		Name:          normalizedRemoteMCPSourceName(input.Name),
		Slug:          conv.ToPGText(remoteSlug),
		TransportType: input.TransportType,
		Url:           input.URL,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return repo.RemoteMcpServer{}, oops.E(oops.CodeConflict, err, "remote MCP server slug already in use")
		}
		return repo.RemoteMcpServer{}, oops.E(oops.CodeUnexpected, err, "create remote MCP server")
	}
	if err := auditLogger.LogRemoteMcpServerCreate(ctx, tx, audit.LogRemoteMcpServerCreateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(remote.ID),
		RemoteMcpServerURL: remote.Url,
	}); err != nil {
		return repo.RemoteMcpServer{}, oops.E(oops.CodeUnexpected, err, "log remote MCP server creation")
	}
	return remote, nil
}

// requireLiveProjectForActiveOrganization locks the project row for the atomic
// write. It prevents a stale session from creating linked resources after the
// project is deleted or moved out of the caller's active organization.
func requireLiveProjectForActiveOrganization(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext) error {
	if tx == nil || authCtx == nil || authCtx.ProjectID == nil || authCtx.ActiveOrganizationID == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid project ownership check")
	}
	_, err := repo.New(tx).LockLiveProjectForRemoteMCPProvisioning(ctx, repo.LockLiveProjectForRemoteMCPProvisioningParams{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "project not found in active organization")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock project for remote MCP provisioning")
	}
	return nil
}
