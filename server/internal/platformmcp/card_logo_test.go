package platformmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"html/template"
	"image"
	"image/png"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testPNG(t *testing.T) []byte {
	t.Helper()

	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	return encoded.Bytes()
}

func TestCardLogoDataURIAcceptsAllowedImage(t *testing.T) {
	t.Parallel()

	body := testPNG(t)
	logo, err := cardLogoDataURI("image/png", body)
	require.NoError(t, err)
	require.Equal(t, template.URL("data:image/png;base64,"+base64.StdEncoding.EncodeToString(body)), logo)
}

func TestCardLogoDataURIRejectsWrongContentType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		contentType string
		body        []byte
	}{
		{"svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"html labelled as png", "image/png", []byte("<!doctype html><script>alert(1)</script>")},
		{"png labelled as jpeg", "image/jpeg", testPNG(t)},
		{"type with parameters", "image/png; charset=utf-8", testPNG(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logo, err := cardLogoDataURI(tc.contentType, tc.body)
			require.ErrorIs(t, err, errCardLogoType)
			require.Empty(t, logo)
		})
	}
}

func TestCardLogoDataURIRejectsOversizedLogo(t *testing.T) {
	t.Parallel()

	oversized := append(testPNG(t), make([]byte, maxCardLogoBytes)...)
	logo, err := cardLogoDataURI("image/png", oversized)
	require.ErrorIs(t, err, errCardLogoSize)
	require.Empty(t, logo)

	logo, err = cardLogoDataURI("image/png", nil)
	require.ErrorIs(t, err, errCardLogoSize)
	require.Empty(t, logo)
}

func TestCardRendersLogoAsDataImage(t *testing.T) {
	t.Parallel()

	logo, err := cardLogoDataURI("image/png", testPNG(t))
	require.NoError(t, err)
	rendered := renderCard(t, signInCard("Example", logo, "https://dashboard.example.test/inspect"))
	require.Contains(t, rendered, `<img class="mark" src="`+string(logo)+`" alt="">`)
	require.NotContains(t, rendered, `<span class="mark"`)
}

// Only a template.URL built from validated bytes becomes an image: a server
// name that looks like a URL is still escaped text.
func TestCardWithoutLogoShowsInitial(t *testing.T) {
	t.Parallel()

	rendered := renderCard(t, serverCard("data:image/png;base64,AAAA", "", ResolvedProject{Name: "Support", Slug: "support"}, nextActionStartSetup, ""))
	require.Contains(t, rendered, `<span class="mark" aria-hidden="true">D</span>`)
	require.NotContains(t, rendered, "<img")
}

// iconCall is one logo fetch the registration store scheduled.
type iconCall struct {
	projectID     uuid.UUID
	mcpServerID   uuid.UUID
	remoteID      uuid.UUID
	authProjectID *uuid.UUID
}

// recordingServerIcons records scheduled logo fetches instead of fetching.
type recordingServerIcons struct {
	mu    sync.Mutex
	calls []iconCall
}

func (r *recordingServerIcons) ScheduleDefaultRemoteServerIcon(ctx context.Context, projectID, mcpServerID, remoteMCPServerID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	call := iconCall{projectID: projectID, mcpServerID: mcpServerID, remoteID: remoteMCPServerID}
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		call.authProjectID = authCtx.ProjectID
	}
	r.calls = append(r.calls, call)
}

func (r *recordingServerIcons) recorded() []iconCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]iconCall(nil), r.calls...)
}

// storeLogo stores an image asset and makes it the MCP server's logo, as the
// detached logo fetch does.
func storeLogo(t *testing.T, conn *pgxpool.Pool, storage assets.BlobStore, project ResolvedProject, organizationID string, mcpServerID uuid.UUID, contentType string, body []byte) {
	t.Helper()

	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	writer, objectURL, err := storage.Write(t.Context(), "logos/"+hash, contentType, int64(len(body)))
	require.NoError(t, err)
	_, err = writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	asset, err := assetsrepo.New(conn).CreateAsset(t.Context(), assetsrepo.CreateAssetParams{
		Name:           "logo-" + hash[:16],
		Url:            objectURL.String(),
		ProjectID:      project.ID,
		OrganizationID: organizationID,
		Sha256:         hash,
		Kind:           "image",
		ContentType:    contentType,
		ContentLength:  int64(len(body)),
	})
	require.NoError(t, err)
	_, err = mcpmetadatarepo.New(conn).UpsertMetadataByMcpServerID(t.Context(), mcpmetadatarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID: mcpServerID,
		ProjectID:   project.ID,
		LogoID:      uuid.NullUUID{UUID: asset.ID, Valid: true},
	})
	require.NoError(t, err)
}

func callRegisterCatalogCard(t *testing.T, ctx context.Context, session *mcp.ClientSession, project ResolvedProject, providerKey string) string {
	t.Helper()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "register_catalog_mcp", Arguments: map[string]any{
		"project_slug":    project.Slug,
		"provider_key":    providerKey,
		"catalog_ref":     "app.example/mcp",
		"idempotency_key": "catalog-card-logo",
	}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	card, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)
	return card
}

// Adding an MCP server schedules its logo fetch once; once a logo is stored,
// the card inlines it, and a stored SVG logo falls back to the first letter.
func TestCatalogRegistrationCapturesLogoForItsCard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_card_logo")
	require.NoError(t, err)

	providerKey := "browser-catalog-registry-" + uuid.NewString()
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	icons := &recordingServerIcons{}
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	store = store.WithServerIcons(icons)
	storage := assetstest.NewTestBlobStore(t)
	dashboardURL, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	registrations := newRegistrationService(testCatalog{details: CatalogDetails{
		CatalogCandidate: CatalogCandidate{Name: "Reviewed MCP", ProviderKey: providerKey, CatalogRef: "app.example/mcp", SetupIntent: "dashboard_source_settings"},
		Transport:        "streamable-http",
		remoteURL:        "https://reviewed.example.test/mcp",
	}}, &testRegistrationGate{enabled: true}, store).
		WithDashboardURL(dashboardURL).
		WithCardLogos(testenv.NewLogger(t), conn, storage)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerCatalogRegistrationTool(reg, registrations, NewOnboardingService(conn))
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	reg.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})
	session := connectTestClient(t, reg.server)

	first := callRegisterCatalogCard(t, ctx, session, project, providerKey)
	require.NotContains(t, first, "<img", "the logo is fetched after the add returns")
	require.Contains(t, first, `<span class="mark" aria-hidden="true">R</span>`)

	calls := icons.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, project.ID, calls[0].projectID)
	require.NotEqual(t, uuid.Nil, calls[0].mcpServerID)
	require.NotEqual(t, uuid.Nil, calls[0].remoteID)
	require.Equal(t, &project.ID, calls[0].authProjectID, "the logo is stored as an asset of the server's project")

	logo := testPNG(t)
	storeLogo(t, conn, storage, project, principal.OrganizationID, calls[0].mcpServerID, "image/png", logo)
	replayed := callRegisterCatalogCard(t, ctx, session, project, providerKey)
	require.Contains(t, replayed, `<img class="mark" src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(logo)+`" alt="">`)
	require.Len(t, icons.recorded(), 1, "a replayed add does not fetch the logo again")

	storeLogo(t, conn, storage, project, principal.OrganizationID, calls[0].mcpServerID, "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	require.NotContains(t, callRegisterCatalogCard(t, ctx, session, project, providerKey), "<img", "an SVG logo is never inlined")
}
