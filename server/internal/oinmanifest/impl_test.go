package oinmanifest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/oin_manifest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oinmanifest/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	rsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type platformAdminReaderStub struct {
	results []bool
	err     error
	calls   []string
}

func (s *platformAdminReaderStub) IsPlatformAdmin(_ context.Context, userID string) (bool, error) {
	s.calls = append(s.calls, userID)
	if s.err != nil {
		return false, s.err
	}
	if len(s.results) == 0 {
		return false, nil
	}
	result := s.results[0]
	if len(s.results) > 1 {
		s.results = s.results[1:]
	}
	return result, nil
}

type serviceFixture struct {
	service *PlatformService
	conn    *pgxpool.Pool
	logs    *bytes.Buffer
	reader  *sdkmetric.ManualReader
}

func newServiceFixture(t *testing.T, admin bool) serviceFixture {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "oinmanifest")
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	reader := sdkmetric.NewManualReader()
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	service := &PlatformService{
		tracer:   testenv.NewTracerProvider(t).Tracer("test"),
		logger:   logger,
		sessions: &platformAdminReaderStub{results: []bool{admin}},
		repo:     repo.New(conn),
		metrics:  newExportMetrics(logger, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))),
		config:   fixtureConfig,
		now:      func() time.Time { return fixtureNow },
	}
	return serviceFixture{service: service, conn: conn, logs: logs, reader: reader}
}

type issuerFixture struct {
	scopeOrg  string
	issuer    string
	name      string
	grants    []string
	profiles  []string
	fetchedAt time.Time
}

func insertIssuer(t *testing.T, conn *pgxpool.Pool, fixture issuerFixture) uuid.UUID {
	t.Helper()
	row, err := rsrepo.New(conn).CreateRemoteSessionIssuer(t.Context(), rsrepo.CreateRemoteSessionIssuerParams{
		OrganizationID:                    pgtype.Text{String: fixture.scopeOrg, Valid: fixture.scopeOrg != ""},
		Slug:                              strings.ToLower(uuid.NewString()),
		Issuer:                            fixture.issuer,
		Name:                              pgtype.Text{String: fixture.name, Valid: fixture.name != ""},
		ScopesSupported:                   []string{"read"},
		GrantTypesSupported:               fixture.grants,
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		MetadataFetchedAt:                 pgtype.Timestamptz{Time: fixture.fetchedAt, Valid: !fixture.fetchedAt.IsZero()},
	})
	require.NoError(t, err)
	if len(fixture.profiles) > 0 {
		stamped, err := testrepo.New(conn).SetRemoteSessionIssuerGrantProfilesFixture(t.Context(), testrepo.SetRemoteSessionIssuerGrantProfilesFixtureParams{
			GrantProfiles: fixture.profiles,
			ID:            row.ID,
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), stamped)
	}
	return row.ID
}

func insertClient(t *testing.T, conn *pgxpool.Pool, issuerID uuid.UUID, scopeOrg, clientID string, audience string) uuid.UUID {
	t.Helper()
	row, err := rsrepo.New(conn).CreateRemoteSessionClient(t.Context(), rsrepo.CreateRemoteSessionClientParams{
		OrganizationID:          pgtype.Text{String: scopeOrg, Valid: scopeOrg != ""},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                clientID,
		TokenEndpointAuthMethod: pgtype.Text{String: "none", Valid: true},
		Scope:                   []string{"read"},
		Audience:                pgtype.Text{String: audience, Valid: audience != ""},
	})
	require.NoError(t, err)
	return row.ID
}

func insertOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()
	orgID := "org_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "Tenant",
		Slug:               strings.ToLower(orgID),
		GramAccountType:    "free",
		Whitelisted:        true,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: fixtureNow, Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: fixtureNow.Add(14 * 24 * time.Hour), Valid: true},
	}))
	return orgID
}

func validatedCustomerContext(t *testing.T, organizationID, userID, email string) context.Context {
	t.Helper()
	sessionID := "session"
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: organizationID, UserID: userID, SessionID: &sessionID, Email: &email}
	return contextvalues.WithValidatedGramSession(t.Context(), authCtx, false)
}

func unvalidatedSessionContext(t *testing.T) context.Context {
	t.Helper()
	sessionID, email := "session", "user@example.com"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID, Email: &email})
}

func apiKeyContext(t *testing.T) context.Context {
	t.Helper()
	email := "user@example.com"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", APIKeyID: "key", Email: &email})
}

func organizationOnlyContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org"})
}

func validatedMissingSessionContext(t *testing.T) context.Context {
	t.Helper()
	email := "user@example.com"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", Email: &email}, false)
}

func markedAPIKeyContext(t *testing.T) context.Context {
	t.Helper()
	ctx := validatedCustomerContext(t, "org", "user", "user@example.com")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authCtx.APIKeyID = "key"
	return ctx
}

func markedAssistantContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetAssistantPrincipal(validatedCustomerContext(t, "org", "user", "user@example.com"), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
}

func markedOAuthContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetOAuthClientID(validatedCustomerContext(t, "org", "user", "user@example.com"), "client")
}

func markedPlatformMCPContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetActingSurface(validatedCustomerContext(t, "org", "user", "user@example.com"), contextvalues.ActingSurfacePlatformMCP)
}

func supportSessionContext(t *testing.T) context.Context {
	t.Helper()
	ctx := validatedCustomerContext(t, "org", "support", "support@example.com")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authCtx.IsAdmin = true
	authCtx.SupportOrganizationID = "org"
	return contextvalues.WithValidatedSupportSession(ctx, authCtx)
}

func legacyImpersonationContext(t *testing.T) context.Context {
	t.Helper()
	sessionID, email := "session", "user@example.com"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID, Email: &email}, true)
}

func rbacOverrideContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.SetRBACScopeOverride(validatedCustomerContext(t, "org", "user", "user@example.com"), string(authz.ScopeOrgAdmin))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func export(t *testing.T, ctx context.Context, service *PlatformService, format string) (*gen.ExportResult, []byte, error) {
	t.Helper()
	result, body, err := service.Export(ctx, &gen.ExportPayload{Format: format})
	if err != nil {
		return nil, nil, err
	}
	defer func() { require.NoError(t, body.Close()) }()
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	return result, content, nil
}

func TestPlatformServiceRejectsEveryUnsafeCredentialClass(t *testing.T) {
	t.Parallel()

	cachedAdminRevoked := validatedCustomerContext(t, "org_cached", "user", "user@example.com")
	cachedAuth, _ := contextvalues.GetAuthContext(cachedAdminRevoked)
	cachedAuth.IsAdmin = true

	chatSessionID, chatEmail := "chat-session", "forged@example.com"
	chatToken := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org", UserID: "user", ExternalUserID: "external-user", SessionID: &chatSessionID, Email: &chatEmail,
	})

	//nolint:containedctx // Explicit contexts are the authorization matrix inputs.
	tests := []struct {
		name  string
		ctx   context.Context
		admin bool
		want  oops.Code
	}{
		{name: "unattributed", ctx: t.Context(), want: oops.CodeUnauthorized},
		{name: "unvalidated session", ctx: unvalidatedSessionContext(t), want: oops.CodeUnauthorized},
		{name: "organization only", ctx: organizationOnlyContext(t), want: oops.CodeUnauthorized},
		{name: "chat token", ctx: chatToken, want: oops.CodeUnauthorized},
		{name: "raw api key", ctx: apiKeyContext(t), want: oops.CodeUnauthorized},
		{name: "validated missing session", ctx: validatedMissingSessionContext(t), want: oops.CodeUnauthorized},
		{name: "validated missing user", ctx: validatedCustomerContext(t, "", "", "user@example.com"), want: oops.CodeUnauthorized},
		{name: "validated missing email", ctx: validatedCustomerContext(t, "", "user", ""), want: oops.CodeUnauthorized},
		{name: "api key marker", ctx: markedAPIKeyContext(t), admin: true, want: oops.CodeForbidden},
		{name: "assistant", ctx: markedAssistantContext(t), admin: true, want: oops.CodeForbidden},
		{name: "mcp oauth client", ctx: markedOAuthContext(t), admin: true, want: oops.CodeForbidden},
		{name: "platform mcp surface", ctx: markedPlatformMCPContext(t), admin: true, want: oops.CodeForbidden},
		{name: "validated support", ctx: supportSessionContext(t), admin: true, want: oops.CodeForbidden},
		{name: "legacy impersonation", ctx: legacyImpersonationContext(t), admin: true, want: oops.CodeForbidden},
		{name: "rbac override", ctx: rbacOverrideContext(t), admin: true, want: oops.CodeForbidden},
		{name: "ordinary non-admin", ctx: validatedCustomerContext(t, "", "user", "user@example.com"), want: oops.CodeForbidden},
		{name: "cached admin revoked", ctx: cachedAdminRevoked, want: oops.CodeForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			admins := &platformAdminReaderStub{results: []bool{test.admin}}
			// No repo on purpose: a rejection that reached the database would panic.
			service := &PlatformService{logger: testenv.NewLogger(t), sessions: admins, config: fixtureConfig, now: time.Now}
			for _, format := range []string{FormatJSON, FormatMarkdown} {
				_, _, err := export(t, test.ctx, service, format)
				requireOopsCode(t, err, test.want)
			}
		})
	}
}

func TestPlatformServiceEntitlementReaderFailureIsUnavailable(t *testing.T) {
	t.Parallel()

	service := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{err: errors.New("database unavailable")}, config: fixtureConfig, now: time.Now}
	_, _, err := export(t, validatedCustomerContext(t, "", "user", "user@example.com"), service, FormatJSON)
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestPlatformServiceRejectsUnknownFormat(t *testing.T) {
	t.Parallel()

	service := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{results: []bool{true}}, config: fixtureConfig, now: time.Now}
	_, _, err := export(t, validatedCustomerContext(t, "", "user", "user@example.com"), service, "yaml")
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestExportJSONAndMarkdownFromTheGlobalCatalog(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture(t, true)
	fetched := fixtureNow.Add(-time.Hour)
	ready := insertIssuer(t, fixture.conn, issuerFixture{issuer: "https://mcp.example.test", name: "Example MCP", grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fetched})
	clientID := insertClient(t, fixture.conn, ready, "", "global-client", "")
	updated, err := testrepo.New(fixture.conn).SetGlobalRemoteSessionClientResourceIdentifierFixture(t.Context(), testrepo.SetGlobalRemoteSessionClientResourceIdentifierFixtureParams{
		ResourceIdentifier: pgtype.Text{String: "https://mcp.example.test/mcp", Valid: true},
		ID:                 clientID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)
	insertIssuer(t, fixture.conn, issuerFixture{issuer: "https://orphan.example.test", name: "Orphan", grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fetched})
	insertIssuer(t, fixture.conn, issuerFixture{issuer: "https://plain.example.test", name: "Plain OAuth", grants: []string{"authorization_code"}, fetchedAt: fetched})

	ctx := validatedCustomerContext(t, "", "actor_user", " actor@example.com ")
	result, body, err := export(t, ctx, fixture.service, FormatJSON)
	require.NoError(t, err)
	require.Equal(t, "application/json", result.ContentType)
	require.Equal(t, `attachment; filename="speakeasy-oin-xaa-manifest-2026-09-18.json"`, result.ContentDisposition)

	var manifest Manifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.Len(t, manifest.ResourceRegistrations, 2)
	require.Equal(t, "https://mcp.example.test", manifest.ResourceRegistrations[0].ResourceASIssuer)
	require.Equal(t, "global-client", *manifest.ResourceRegistrations[0].ClientID)
	require.Empty(t, manifest.ResourceRegistrations[0].Blockers)
	require.Equal(t, "https://orphan.example.test", manifest.ResourceRegistrations[1].ResourceASIssuer)
	require.Contains(t, manifest.ResourceRegistrations[1].Blockers, blockerNoClient)
	require.Equal(t, Summary{Registrations: 2, Ready: 1, Blocked: 1}, manifest.Summary)

	result, markdown, err := export(t, ctx, fixture.service, FormatMarkdown)
	require.NoError(t, err)
	require.Equal(t, contentTypeMarkdown, result.ContentType)
	require.Equal(t, `attachment; filename="speakeasy-oin-xaa-manifest-2026-09-18.md"`, result.ContentDisposition)
	require.Contains(t, string(markdown), "| Example MCP | https://mcp.example.test |")
	require.Contains(t, string(markdown), "| Orphan | https://orphan.example.test |")
	require.NotContains(t, string(markdown), "Plain OAuth")

	// One structured audit line per export, carrying the actor and counts.
	lines := strings.Split(strings.TrimSpace(fixture.logs.String()), "\n")
	var auditLines []map[string]any
	for _, line := range lines {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record[string(attr.AuditActionKey)] == AuditActionExport {
			auditLines = append(auditLines, record)
		}
	}
	require.Len(t, auditLines, 2)
	require.Equal(t, "actor_user", auditLines[0][string(attr.UserIDKey)])
	require.Equal(t, "actor@example.com", auditLines[0][string(attr.AuthUserEmailKey)])
	require.Equal(t, "json", auditLines[0][string(attr.OINManifestFormatKey)])
	require.Equal(t, "markdown", auditLines[1][string(attr.OINManifestFormatKey)])
	require.InDelta(t, 2, auditLines[0][string(attr.OINManifestRegistrationCountKey)], 0)
	require.InDelta(t, 1, auditLines[0][string(attr.OINManifestReadyCountKey)], 0)
	require.InDelta(t, 1, auditLines[0][string(attr.OINManifestBlockedCountKey)], 0)

	require.Equal(t, int64(1), counterValue(t, ctx, fixture.reader, meterExport, map[attribute.Key]string{attr.OINManifestFormatKey: FormatJSON}))
	require.Equal(t, int64(1), counterValue(t, ctx, fixture.reader, meterExport, map[attribute.Key]string{attr.OINManifestFormatKey: FormatMarkdown}))
}

func TestExportIgnoresTenantRowsForTheSameIssuer(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture(t, true)
	fetched := fixtureNow.Add(-time.Hour)
	global := insertIssuer(t, fixture.conn, issuerFixture{issuer: "https://mcp.example.test", name: "Global", grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fetched})
	insertClient(t, fixture.conn, global, "", "global-client", "")
	orgID := insertOrganization(t, fixture.conn)
	tenant := insertIssuer(t, fixture.conn, issuerFixture{scopeOrg: orgID, issuer: "https://mcp.example.test", name: "Tenant", grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fetched})
	insertClient(t, fixture.conn, tenant, orgID, "tenant-client", "https://auth.example.test")
	insertClient(t, fixture.conn, global, orgID, "tenant-client-on-global-issuer", "")

	_, body, err := export(t, validatedCustomerContext(t, orgID, "user", "user@example.com"), fixture.service, FormatJSON)
	require.NoError(t, err)

	var manifest Manifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.Len(t, manifest.ResourceRegistrations, 1)
	require.Equal(t, "Global", manifest.ResourceRegistrations[0].ResourceName)
	require.Equal(t, "global-client", *manifest.ResourceRegistrations[0].ClientID)
	require.Equal(t, []string{blockerNoResource}, manifest.ResourceRegistrations[0].Blockers)
	require.NotContains(t, string(body), "tenant-client")
	require.NotContains(t, string(body), orgID)
}

func TestExportBodiesCarryNoSecretsTenantIdsOrRowIds(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture(t, true)
	fetched := fixtureNow.Add(-time.Hour)
	issuerID := insertIssuer(t, fixture.conn, issuerFixture{issuer: "https://mcp.example.test", name: "Example", grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fetched})
	clientID := insertClient(t, fixture.conn, issuerID, "", "global-client", "")

	ctx := validatedCustomerContext(t, "", "user", "user@example.com")
	_, body, err := export(t, ctx, fixture.service, FormatJSON)
	require.NoError(t, err)

	var decoded any
	require.NoError(t, json.Unmarshal(body, &decoded))
	forbidden := regexp.MustCompile(`(?i)secret|encrypted|jwks|organization_id|project_id`)
	uuidShaped := regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	var walk func(path string, value any)
	walk = func(path string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				require.NotRegexp(t, forbidden, key, "field %s.%s", path, key)
				walk(path+"."+key, child)
			}
		case []any:
			for _, child := range typed {
				walk(path+"[]", child)
			}
		case string:
			require.NotRegexp(t, uuidShaped, typed, "value at %s", path)
		}
	}
	walk("$", decoded)
	require.NotContains(t, string(body), issuerID.String())
	require.NotContains(t, string(body), clientID.String())

	_, markdown, err := export(t, ctx, fixture.service, FormatMarkdown)
	require.NoError(t, err)
	require.NotRegexp(t, forbidden, string(markdown))
	require.NotRegexp(t, uuidShaped, string(markdown))
}

func TestQueryPredicateAgreesWithAdvertisesIDJAG(t *testing.T) {
	t.Parallel()

	fixture := newServiceFixture(t, true)
	fetched := fixtureNow.Add(-time.Hour)
	cases := []struct {
		issuer   string
		grants   []string
		profiles []string
	}{
		{issuer: "https://both.example.test", grants: idjagGrants, profiles: idjagProfiles},
		{issuer: "https://grant-only.example.test", grants: idjagGrants, profiles: []string{"urn:example:other"}},
		{issuer: "https://profile-only.example.test", grants: []string{"authorization_code"}, profiles: idjagProfiles},
		{issuer: "https://neither.example.test", grants: []string{"authorization_code"}, profiles: []string{"urn:example:other"}},
	}
	want := map[string]bool{}
	for _, c := range cases {
		insertIssuer(t, fixture.conn, issuerFixture{issuer: c.issuer, name: c.issuer, grants: c.grants, profiles: c.profiles, fetchedAt: fetched})
		want[c.issuer] = AdvertisesIDJAG(c.grants, c.profiles)
	}

	rows, err := fixture.service.repo.ListGlobalIDJAGIssuers(t.Context())
	require.NoError(t, err)
	got := map[string]bool{}
	for _, row := range rows {
		got[row.Issuer] = true
		require.True(t, AdvertisesIDJAG(row.GrantTypesSupported, row.GrantProfilesSupported))
	}
	for issuer, expected := range want {
		require.Equal(t, expected, got[issuer], issuer)
	}
}

func counterValue(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader, name string, want map[attribute.Key]string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %s is an int64 sum", name)
			for _, dp := range sum.DataPoints {
				matches := true
				for k, v := range want {
					got, ok := dp.Attributes.Value(k)
					if !ok || got.AsString() != v {
						matches = false
					}
				}
				if matches {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func TestExportRegistrationLimit(t *testing.T) {
	t.Parallel()
	require.Equal(t, 100, MaxRegistrations)
	fixture := newServiceFixture(t, true)
	ctx := validatedCustomerContext(t, "", "actor_user", "actor@example.com")
	for i := range 100 {
		insertIssuer(t, fixture.conn, issuerFixture{issuer: fmt.Sprintf("https://as-%d.example.test", i), grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fixtureNow})
	}
	for _, format := range []string{FormatJSON, FormatMarkdown} {
		_, body, err := export(t, ctx, fixture.service, format)
		require.NoError(t, err)
		require.NotEmpty(t, body)
		if format == FormatJSON {
			var manifest Manifest
			require.NoError(t, json.Unmarshal(body, &manifest))
			require.Equal(t, 100, manifest.Summary.Registrations)
		}
	}
	insertIssuer(t, fixture.conn, issuerFixture{issuer: "https://overflow.example.test", grants: idjagGrants, profiles: idjagProfiles, fetchedAt: fixtureNow})
	for _, format := range []string{FormatJSON, FormatMarkdown} {
		result, body, err := fixture.service.Export(ctx, &gen.ExportPayload{Format: format})
		require.ErrorContains(t, err, "manifest exceeds 100 registrations")
		require.Nil(t, result)
		require.Nil(t, body)
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeFailedPrecondition, shareable.Code)
	}
}
