package toolsets_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostedmcptest"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createShimIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID.String()
}

func storedHostedIssuer(t *testing.T, ctx context.Context, ti *testInstance, toolset *types.Toolset) uuid.NullUUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(toolset.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	return server.UserSessionIssuerID
}

func requireShimOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func TestSetUserSessionIssuer_SetsAndClearsHostedServerIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Shim issuer toolset")
	issuerID := createShimIssuer(t, ctx, ti, "shim-workforce")
	buf := &lockedBuffer{}
	ti.service.SetLoggerForTest(slog.New(slog.NewJSONHandler(buf, nil)))

	updated, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID})
	require.NoError(t, err)
	require.Equal(t, issuerID, *updated.UserSessionIssuerID)
	require.Equal(t, issuerID, storedHostedIssuer(t, ctx, ti, toolset).UUID.String())
	hostedmcptest.RequireMirrored(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(toolset.ID))
	records := buf.deprecationRecords(t)
	require.Len(t, records, 1)
	require.Equal(t, authCtx.ActiveOrganizationID, records[0]["gram.org.id"])

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerUpdate, toolset.ID)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Empty(t, before["UserSessionIssuerID"])
	require.Equal(t, issuerID, after["UserSessionIssuerID"])

	cleared, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: nil})
	require.NoError(t, err)
	require.Nil(t, cleared.UserSessionIssuerID)
	require.False(t, storedHostedIssuer(t, ctx, ti, toolset).Valid)
	hostedmcptest.RequireMirrored(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(toolset.ID))
	require.Len(t, buf.deprecationRecords(t), 1)

	record, err = audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerUpdate, toolset.ID)
	require.NoError(t, err)
	after, err = audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Empty(t, after["UserSessionIssuerID"])
}

func TestSetUserSessionIssuer_RequiresServerScopedWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Shim authz toolset")
	issuerID := createShimIssuer(t, ctx, ti, "shim-authz")
	payload := &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID}

	projectOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPWrite, authCtx.ProjectID.String()))
	_, err := ti.service.SetUserSessionIssuer(projectOnly, payload)
	requireShimOopsCode(t, err, oops.CodeForbidden)
	require.False(t, storedHostedIssuer(t, ctx, ti, toolset).Valid)

	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPRead, toolset.ID))
	_, err = ti.service.SetUserSessionIssuer(readOnly, payload)
	requireShimOopsCode(t, err, oops.CodeForbidden)

	scoped := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPWrite, toolset.ID))
	_, err = ti.service.SetUserSessionIssuer(scoped, payload)
	require.NoError(t, err)
	require.Equal(t, issuerID, storedHostedIssuer(t, ctx, ti, toolset).UUID.String())
}

func TestSetUserSessionIssuer_ClearRefusedWhileEMABound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Shim EMA toolset")
	issuerID := createShimIssuer(t, ctx, ti, "shim-ema")
	_, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID})
	require.NoError(t, err)

	q := remoterepo.New(ti.conn)
	remoteIssuer, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(*authCtx.ProjectID), Slug: "shim-clear-ema", Issuer: "https://authorization.example.com", ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}})
	require.NoError(t, err)
	key := remoterepo.GetEMABindingParams{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, UserSessionIssuerID: uuid.MustParse(issuerID), RemoteSessionIssuerID: remoteIssuer.ID, Resource: "https://resource.example.com/"}
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams(key)))

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: nil})
	requireShimOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "identity-chaining bindings")
	require.Equal(t, issuerID, storedHostedIssuer(t, ctx, ti, toolset).UUID.String(), "a refused clear keeps the issuer")
}

func TestSetUserSessionIssuer_RejectsUnknownIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Shim unknown issuer toolset")
	unknown := uuid.NewString()

	_, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &unknown})
	requireShimOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: types.Slug("missing-toolset"), UserSessionIssuerID: nil})
	requireShimOopsCode(t, err, oops.CodeNotFound)
}

func TestSetUserSessionIssuer_AnnouncesSunset(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)
	mux := goahttp.NewMuxer()
	toolsets.Attach(mux, ti.service)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/toolsets.setUserSessionIssuer?slug=any", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "@1792972800", rec.Header().Get("Deprecation"))
	require.Equal(t, "Wed, 25 Nov 2026 00:00:00 GMT", rec.Header().Get("Sunset"))
}
