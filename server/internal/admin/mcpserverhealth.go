package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// MCPServerHealthReader reads one server's tool call telemetry. Implemented by
// telemetry.MCPServerHealth, which owns the ClickHouse reads.
type MCPServerHealthReader interface {
	Outcomes(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time) (*telemetry.MCPServerOutcomes, error)
	Series(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time, bucket time.Duration) ([]telemetry.MCPServerSeriesPoint, error)
}

// MCPServerTelemetryTarget is built from the admin's own rows.
type MCPServerTelemetryTarget = telemetry.MCPServerTelemetryTarget

const (
	toolCallsLoggingDisabled = "logging:disabled"
	toolCallsLoggingEnabled  = "logging:enabled"
)

// validationStatuses is the closed set remote_sessions.validation_status holds
// once a validation has run.
var validationStatuses = map[string]bool{
	string(remotesessions.ValidationOutcomeValid):            true,
	string(remotesessions.ValidationOutcomeRejectedByMember): true,
	string(remotesessions.ValidationOutcomeInactive):         true,
	string(remotesessions.ValidationOutcomeUnknown):          true,
}

// errHealthMalformed marks a stored or telemetry value outside the closed sets
// the result declares. The handler fails closed on it rather than passing an
// unexpected label through.
var errHealthMalformed = errors.New("malformed mcp server health value")

// healthServer is the target both server health reads resolve first: the
// project checked against the organization, then the server within it.
type healthServer struct {
	queries   *repo.Queries
	projectID uuid.UUID
	row       repo.AdminGetMcpServerAuthRow
	bucket    time.Duration
	logAttrs  []slog.Attr
}

func (s *Service) resolveHealthServer(ctx context.Context, organizationID, rawProjectID, rawServerID string, windowDays int) (*healthServer, error) {
	projectID, err := uuid.Parse(rawProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id")
	}
	serverID, err := uuid.Parse(rawServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}
	bucket, err := healthBucket(windowDays)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "window_days must be 14, 30 or 90")
	}

	logAttrs := []slog.Attr{attr.SlogOrganizationID(organizationID), attr.SlogProjectID(projectID.String())}

	queries := repo.New(s.db)
	belongs, err := queries.AdminProjectBelongsToOrganization(ctx, repo.AdminProjectBelongsToOrganizationParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	switch {
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup project organization").LogError(ctx, s.logger, logAttrs...)
	case !belongs:
		return nil, oops.C(oops.CodeNotFound)
	}

	row, err := queries.AdminGetMcpServerAuth(ctx, repo.AdminGetMcpServerAuthParams{ID: serverID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read mcp server").LogError(ctx, s.logger, logAttrs...)
	}

	return &healthServer{queries: queries, projectID: projectID, row: row, bucket: bucket, logAttrs: logAttrs}, nil
}

func (s *Service) DescribeMcpServerHealth(ctx context.Context, payload *gen.DescribeMcpServerHealthPayload) (*gen.AdminMcpServerHealth, error) {
	target, err := s.resolveHealthServer(ctx, payload.OrganizationID, payload.ProjectID, payload.McpServerID, payload.WindowDays)
	if err != nil {
		return nil, err
	}

	from := time.Now().UTC().AddDate(0, 0, -payload.WindowDays)
	result, err := s.buildMCPServerHealth(ctx, target.queries, payload.OrganizationID, target.projectID, target.row, from)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read mcp server health").LogError(ctx, s.logger, target.logAttrs...)
	}
	if healthServerSource(target.row) == "remote" {
		scopes, err := s.scopes.Describe(ctx, remotemcp.StaffScopeTarget{OrganizationID: payload.OrganizationID, ProjectID: target.projectID, McpServerID: target.row.ID})
		switch {
		case errors.Is(err, remotemcp.ErrNotRemoteBacked):
			// The remote row was deleted under a live server: no resource to report.
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "read mcp server resource scopes").LogError(ctx, s.logger, target.logAttrs...)
		default:
			result.ResourceScopes = adminResourceScopes(scopes)
		}
	}
	return result, nil
}

func (s *Service) GetMcpServerToolCalls(ctx context.Context, payload *gen.GetMcpServerToolCallsPayload) (*gen.AdminMcpServerToolCalls, error) {
	target, err := s.resolveHealthServer(ctx, payload.OrganizationID, payload.ProjectID, payload.McpServerID, payload.WindowDays)
	if err != nil {
		return nil, err
	}
	// Strict, not cached: a stale cache entry could read telemetry for an
	// organization that has just turned logs off.
	features, err := s.productFeatures.SnapshotStrict(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization logs feature").LogError(ctx, s.logger, target.logAttrs...)
	}
	if !features.LogsEnabled {
		// Rows are dropped at write time while logs are off, so zeros would
		// claim a quiet server that was never observed.
		return &gen.AdminMcpServerToolCalls{
			Type:          toolCallsLoggingDisabled,
			WindowDays:    nil,
			Watermark:     nil,
			Outcomes:      nil,
			BucketSeconds: nil,
			Daily:         nil,
		}, nil
	}

	if s.mcpServerHealth == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "mcp server health telemetry is unavailable")
	}
	correlation := healthCorrelation(target.row)
	telemetryTarget := MCPServerTelemetryTarget{
		ProjectID:   target.projectID.String(),
		MCPServerID: conv.PtrValOr(correlation.McpServerID, ""),
		ToolsetSlug: conv.PtrValOr(correlation.ToolsetSlug, ""),
		URLSlug:     conv.PtrValOr(correlation.URLSlug, ""),
	}
	now := time.Now().UTC()
	toolCalls, err := s.readToolCalls(ctx, telemetryTarget, payload.WindowDays, now.AddDate(0, 0, -payload.WindowDays), now, target.bucket)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read mcp server tool calls").LogError(ctx, s.logger, target.logAttrs...)
	}
	return toolCalls, nil
}

func healthBucket(windowDays int) (time.Duration, error) {
	switch windowDays {
	case 14, 30:
		return 24 * time.Hour, nil
	case 90:
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported window of %d days", windowDays)
	}
}

// buildMCPServerHealth assembles the health report from Postgres.
func (s *Service) buildMCPServerHealth(ctx context.Context, queries *repo.Queries, organizationID string, projectID uuid.UUID, server repo.AdminGetMcpServerAuthRow, windowStart time.Time) (*gen.AdminMcpServerHealth, error) {
	switch server.Visibility {
	case "disabled", "private", "public":
	default:
		return nil, fmt.Errorf("%w: visibility %q", errHealthMalformed, server.Visibility)
	}

	result := &gen.AdminMcpServerHealth{
		Server: &gen.AdminMcpServerHealthServer{
			ID:         server.ID.String(),
			Name:       server.Name,
			Source:     healthServerSource(server),
			Visibility: server.Visibility,
			CreatedAt:  server.CreatedAt.Time.UTC().Format(time.RFC3339),
		},
		Correlation:       healthCorrelation(server),
		LegacyAuth:        nil,
		UserSessionIssuer: nil,
		ResourceScopes:    nil,
	}

	if server.UserSessionIssuerID.Valid {
		issuer, err := s.readUserSessionIssuer(ctx, queries, organizationID, projectID, server.ID, server.UserSessionIssuerID.UUID, windowStart)
		if err != nil {
			return nil, err
		}
		result.UserSessionIssuer = issuer
	}
	if result.UserSessionIssuer == nil {
		result.LegacyAuth = legacyAuth(server)
	}

	return result, nil
}

// healthCorrelation lists the identities telemetry is matched on for the
// server. Hosted calls carry only the toolset slug, so a slug shared by
// several wrappers would count their calls as this server's and is left out.
func healthCorrelation(server repo.AdminGetMcpServerAuthRow) *gen.AdminMcpServerHealthCorrelation {
	correlation := &gen.AdminMcpServerHealthCorrelation{
		URLSlug:     conv.PtrEmpty(server.UrlSlug),
		McpServerID: nil,
		ToolsetSlug: nil,
	}
	if !server.ToolsetOnly {
		id := server.ID.String()
		correlation.McpServerID = &id
	}
	if server.ToolsetSlug != "" && (server.ToolsetOnly || server.ToolsetWrapperCount <= 1) {
		slug := server.ToolsetSlug
		correlation.ToolsetSlug = &slug
	}
	return correlation
}

func healthServerSource(server repo.AdminGetMcpServerAuthRow) string {
	switch {
	case server.ToolsetOnly:
		return "toolset_only"
	case server.ToolsetID.Valid:
		return "toolset"
	case server.RemoteMcpServerID.Valid:
		return "remote"
	case server.TunneledMcpServerID.Valid:
		return "tunneled"
	default:
		return "unproxied"
	}
}

// legacyAuth names the authentication a server without a user session issuer
// relies on.
func legacyAuth(server repo.AdminGetMcpServerAuthRow) *string {
	var mode string
	switch {
	case server.ExternalOauthServerID.Valid:
		mode = "external_oauth"
	case server.OauthProxyServerID.Valid:
		mode = "oauth_proxy"
	case server.Visibility == "private":
		mode = "gram_private"
	default:
		return nil
	}
	return &mode
}

// readUserSessionIssuer returns nil when the issuer has been deleted or is not
// visible to the project, which leaves the server with no issuer in force.
func (s *Service) readUserSessionIssuer(ctx context.Context, queries *repo.Queries, organizationID string, projectID, serverID, issuerID uuid.UUID, windowStart time.Time) (*gen.AdminMcpServerHealthUserSessionIssuer, error) {
	row, err := queries.AdminGetUserSessionIssuer(ctx, repo.AdminGetUserSessionIssuerParams{
		ID:             issuerID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read user session issuer: %w", err)
	}

	switch row.Classification {
	case "custom", "project_default_idp":
	default:
		return nil, fmt.Errorf("%w: issuer classification %q", errHealthMalformed, row.Classification)
	}
	switch row.AuthnChallengeMode {
	case "chain", "interactive":
	default:
		return nil, fmt.Errorf("%w: authn challenge mode %q", errHealthMalformed, row.AuthnChallengeMode)
	}
	scope, err := healthAttachmentScope(row.AttachmentScope)
	if err != nil {
		return nil, err
	}
	var admissionMode *string
	if row.ClientIDMetadataAdmissionMode.Valid {
		mode, ok := admission.ResolveMode(row.ClientIDMetadataAdmissionMode.String, true)
		if !ok {
			return nil, fmt.Errorf("%w: cimd admission mode %q", errHealthMalformed, row.ClientIDMetadataAdmissionMode.String)
		}
		stored := string(mode)
		admissionMode = &stored
	}
	var trusted *gen.AdminMcpServerHealthTrustedRemoteSession
	if row.TrustedRemoteSessionIssuerID.Valid && row.TrustedRemoteSessionClientID.Valid {
		trusted = &gen.AdminMcpServerHealthTrustedRemoteSession{
			IssuerID: row.TrustedRemoteSessionIssuerID.UUID.String(),
			ClientID: row.TrustedRemoteSessionClientID.UUID.String(),
		}
	}

	others, err := queries.AdminListOtherServersUsingIssuer(ctx, repo.AdminListOtherServersUsingIssuerParams{
		ProjectID:           projectID,
		UserSessionIssuerID: issuerID,
		ExcludeID:           serverID,
	})
	if err != nil {
		return nil, fmt.Errorf("list servers sharing issuer: %w", err)
	}
	otherServers := make([]*gen.AdminMcpServerHealthServerRef, 0, len(others))
	for _, other := range others {
		otherServers = append(otherServers, &gen.AdminMcpServerHealthServerRef{ID: other.ID.String(), Name: other.Name})
	}

	stats, err := queries.AdminUserSessionStats(ctx, repo.AdminUserSessionStatsParams{
		WindowStart:         pgtype.Timestamptz{Time: windowStart, InfinityModifier: pgtype.Finite, Valid: true},
		UserSessionIssuerID: issuerID,
	})
	if err != nil {
		return nil, fmt.Errorf("read user session stats: %w", err)
	}

	clients, err := s.readRemoteSessionClients(ctx, queries, issuerID)
	if err != nil {
		return nil, err
	}

	return &gen.AdminMcpServerHealthUserSessionIssuer{
		ID:                            row.ID.String(),
		Slug:                          row.Slug,
		Classification:                row.Classification,
		AuthnChallengeMode:            row.AuthnChallengeMode,
		SessionDurationHours:          row.SessionDurationHours,
		AttachmentScope:               scope,
		ClientIDMetadataAdmissionMode: admissionMode,
		UseAuthenticationHost:         row.UseAuthenticationHost,
		TrustedRemoteSession:          trusted,
		OtherServersUsingIssuer:       otherServers,
		CreatedAt:                     row.CreatedAt.Time.UTC().Format(time.RFC3339),
		Sessions: &gen.AdminMcpServerHealthUserSessions{
			DistinctSubjectsEver:     stats.DistinctSubjectsEver,
			DistinctSubjectsInWindow: stats.DistinctSubjectsInWindow,
			FirstIssuedAt:            healthStamp(stats.FirstIssuedAt),
			LastIssuedAt:             healthStamp(stats.LastIssuedAt),
			Live:                     stats.Live,
		},
		RemoteSessionClients: clients,
	}, nil
}

func (s *Service) readRemoteSessionClients(ctx context.Context, queries *repo.Queries, issuerID uuid.UUID) ([]*gen.AdminMcpServerHealthRemoteSessionClient, error) {
	rows, err := queries.AdminListIssuerRemoteSessionClients(ctx, issuerID)
	if err != nil {
		return nil, fmt.Errorf("list remote session clients: %w", err)
	}
	if len(rows) == 0 {
		return []*gen.AdminMcpServerHealthRemoteSessionClient{}, nil
	}

	clientIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		clientIDs = append(clientIDs, row.ID)
	}
	stats, err := queries.AdminRemoteSessionStats(ctx, clientIDs)
	if err != nil {
		return nil, fmt.Errorf("read remote session stats: %w", err)
	}
	counts, err := queries.AdminRemoteSessionValidationCounts(ctx, clientIDs)
	if err != nil {
		return nil, fmt.Errorf("read remote session validation counts: %w", err)
	}

	sessions := make(map[uuid.UUID]*gen.AdminMcpServerHealthRemoteSessions, len(rows))
	for _, id := range clientIDs {
		sessions[id] = &gen.AdminMcpServerHealthRemoteSessions{
			LinkedSubjects:         0,
			Reauthorizations:       0,
			FirstLinkedAt:          nil,
			ValidationStatusCounts: map[string]int64{},
		}
	}
	for _, stat := range stats {
		entry, ok := sessions[stat.RemoteSessionClientID]
		if !ok {
			return nil, fmt.Errorf("%w: stats for an unlisted client", errHealthMalformed)
		}
		entry.LinkedSubjects = stat.LinkedSubjects
		entry.Reauthorizations = stat.Reauthorizations
		entry.FirstLinkedAt = healthStamp(stat.FirstLinkedAt)
	}
	for _, count := range counts {
		entry, ok := sessions[count.RemoteSessionClientID]
		if !ok {
			return nil, fmt.Errorf("%w: validation counts for an unlisted client", errHealthMalformed)
		}
		if !validationStatuses[count.ValidationStatus] {
			return nil, fmt.Errorf("%w: validation status %q", errHealthMalformed, count.ValidationStatus)
		}
		entry.ValidationStatusCounts[count.ValidationStatus] = count.Sessions
	}

	clients := make([]*gen.AdminMcpServerHealthRemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		client, err := healthRemoteSessionClient(row, sessions[row.ID])
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, nil
}

func healthRemoteSessionClient(row repo.AdminListIssuerRemoteSessionClientsRow, sessions *gen.AdminMcpServerHealthRemoteSessions) (*gen.AdminMcpServerHealthRemoteSessionClient, error) {
	registration := "static"
	switch {
	case row.IsCimd:
		registration = "cimd"
	case row.IsDcr:
		registration = "dcr"
	}

	var authMethod *string
	if row.TokenEndpointAuthMethod.Valid {
		switch row.TokenEndpointAuthMethod.String {
		case "client_secret_basic", "client_secret_post", "none", "private_key_jwt":
			method := row.TokenEndpointAuthMethod.String
			authMethod = &method
		default:
			return nil, fmt.Errorf("%w: token endpoint auth method %q", errHealthMalformed, row.TokenEndpointAuthMethod.String)
		}
	}

	clientScope, err := healthAttachmentScope(row.AttachmentScope)
	if err != nil {
		return nil, err
	}
	issuerScope, err := healthAttachmentScope(row.IssuerAttachmentScope)
	if err != nil {
		return nil, err
	}

	networking := "public"
	if row.IssuerTunneled {
		networking = "tunneled"
	}

	var issuerName *string
	if row.IssuerName.Valid {
		name := row.IssuerName.String
		issuerName = &name
	}

	return &gen.AdminMcpServerHealthRemoteSessionClient{
		ID:                            row.ID.String(),
		Registration:                  registration,
		TokenEndpointAuthMethod:       authMethod,
		Scope:                         nonNilStrings(row.Scope),
		GrantTypes:                    nonNilStrings(row.GrantTypes),
		HasIdentityProviderConnection: row.HasIdentityProviderConnection,
		AttachmentScope:               clientScope,
		UpstreamRejectedAt:            healthStamp(row.UpstreamRejectedAt),
		Issuer: &gen.AdminMcpServerHealthRemoteSessionIssuer{
			ID:                  row.IssuerID.String(),
			Slug:                row.IssuerSlug,
			Name:                issuerName,
			Issuer:              row.IssuerUrl,
			AttachmentScope:     issuerScope,
			Networking:          networking,
			Oidc:                row.IssuerOidc,
			Passthrough:         row.IssuerPassthrough,
			Pkce:                string(remotesessionmetrics.ClassifyPKCESupport(row.IssuerCodeChallengeMethodsSupported)),
			CimdSupported:       row.IssuerCimdSupported,
			ScopeOverride:       row.IssuerScopeOverride,
			OmitScopeFallback:   conv.FromPGBool[bool](row.IssuerOmitScopeFallback),
			MetadataFetchedAt:   healthStamp(row.IssuerMetadataFetchedAt),
			MetadataLastErrorAt: healthStamp(row.IssuerMetadataLastErrorAt),
			JwksLastErrorAt:     healthStamp(row.IssuerJwksLastErrorAt),
		},
		Sessions: sessions,
	}, nil
}

// healthAttachmentScope reduces a generated attachment_scope column to its
// kind. The row is already reached through the described server, so the id
// after the colon adds nothing.
func healthAttachmentScope(scope pgtype.Text) (string, error) {
	switch {
	case scope.Valid && scope.String == "global":
		return "global", nil
	case scope.Valid && strings.HasPrefix(scope.String, "project:"):
		return "project", nil
	case scope.Valid && strings.HasPrefix(scope.String, "organization:"):
		return "organization", nil
	default:
		return "", fmt.Errorf("%w: attachment scope", errHealthMalformed)
	}
}

func (s *Service) readToolCalls(ctx context.Context, target MCPServerTelemetryTarget, windowDays int, from, to time.Time, bucket time.Duration) (*gen.AdminMcpServerToolCalls, error) {
	outcomes, err := s.mcpServerHealth.Outcomes(ctx, target, from, to)
	if err != nil {
		return nil, fmt.Errorf("read outcomes: %w", err)
	}
	if outcomes == nil {
		return nil, fmt.Errorf("%w: no outcomes", errHealthMalformed)
	}

	daily := []*gen.AdminMcpServerToolCallBucket{}
	// The series reads the direct lane only. A server reachable only through
	// hook-observed URLs has no identity it can filter on there.
	if target.MCPServerID != "" || target.ToolsetSlug != "" {
		points, err := s.mcpServerHealth.Series(ctx, target, from, to, bucket)
		if err != nil {
			return nil, fmt.Errorf("read series: %w", err)
		}
		daily = make([]*gen.AdminMcpServerToolCallBucket, 0, len(points))
		for _, point := range points {
			if point.Total < 0 || point.Failed < 0 || point.Failed > point.Total {
				return nil, fmt.Errorf("%w: series bucket counts", errHealthMalformed)
			}
			daily = append(daily, &gen.AdminMcpServerToolCallBucket{
				BucketStart: point.BucketStart.UTC().Format(time.RFC3339),
				Total:       point.Total,
				Failed:      point.Failed,
			})
		}
	}

	var watermark *string
	if !outcomes.Watermark.IsZero() {
		stamped := outcomes.Watermark.UTC().Format(time.RFC3339)
		watermark = &stamped
	}
	bucketSeconds := int64(bucket / time.Second)

	return &gen.AdminMcpServerToolCalls{
		Type:       toolCallsLoggingEnabled,
		WindowDays: &windowDays,
		Watermark:  watermark,
		Outcomes: &gen.AdminMcpServerToolCallOutcomes{
			Success:      outcomes.Success,
			Unauthorized: outcomes.Unauthorized,
			ClientError:  outcomes.ClientError,
			ServerError:  outcomes.ServerError,
			Blocked:      outcomes.Blocked,
			Failed:       outcomes.Failed,
			Unknown:      outcomes.Unknown,
		},
		BucketSeconds: &bucketSeconds,
		Daily:         daily,
	}, nil
}

// healthStamp returns nil for an unset timestamp: the fields declare a
// date-time format, so an empty string would fail validation.
func healthStamp(at pgtype.Timestamptz) *string {
	if !at.Valid {
		return nil
	}
	stamped := at.Time.UTC().Format(time.RFC3339)
	return &stamped
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
