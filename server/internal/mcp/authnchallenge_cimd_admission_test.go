// Integration tests for CIMD admission control: the per-issuer policy that
// decides WHICH Client ID Metadata Document URLs an issuer accepts, applied
// before any document is fetched.
//
// The doc server's URL can never be a catalog preset (it is an ephemeral
// httptest address), so end-to-end coverage of preset admission lives in the
// admission package's unit tests. What these tests cover is the wiring: that
// the mode reaches the enforcement points, that a denial costs no outbound
// request, that the rendering is the actionable one, and that a custom URL
// row admits exactly the issuer it belongs to.

package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// setIssuerAdmissionMode writes an explicit admission mode onto the toolset's
// issuer. Passing an empty mode leaves the column NULL, which is the state a
// freshly-created issuer is in.
func setIssuerAdmissionMode(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, mode admission.Mode) {
	t.Helper()

	if mode == "" {
		return
	}
	err := testrepo.New(ti.conn).SetUserSessionIssuerCIMDAdmissionMode(ctx, testrepo.SetUserSessionIssuerCIMDAdmissionModeParams{
		ClientIDMetadataAdmissionMode: conv.ToPGText(string(mode)),
		ID:                            toolset.UserSessionIssuerID.UUID,
		ProjectID:                     toolset.ProjectID,
	})
	require.NoError(t, err)
}

// seedFreshIssuerToolset seeds a brand-new public issuer-gated toolset and
// pins what a create writes: the admission mode is stored as 'open', not
// left NULL, so the resting policy is a real value on the row rather than an
// absence the application has to interpret. Tests covering an unconfigured
// issuer start here; clearIssuerAdmissionMode covers the older rows that
// predate the create default.
func seedFreshIssuerToolset(t *testing.T, ctx context.Context, ti *testInstance) toolsets_repo.Toolset {
	t.Helper()

	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	toolset, err := toolsets_repo.New(ti.conn).UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	issuer, err := usersessions_repo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        toolset.UserSessionIssuerID.UUID,
		ProjectID: toolset.ProjectID,
	})
	require.NoError(t, err)
	require.True(t, issuer.ClientIDMetadataAdmissionMode.Valid, "a created issuer must carry an explicit admission mode")
	require.Equal(t, string(admission.ModeOpen), issuer.ClientIDMetadataAdmissionMode.String)

	return toolset
}

// clearIssuerAdmissionMode writes the column back to NULL, the state of
// every row created before the create query wrote a mode explicitly. It has
// no management API equivalent on purpose: this reproduces stored history,
// not something an operator can do.
func clearIssuerAdmissionMode(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset) {
	t.Helper()

	err := testrepo.New(ti.conn).SetUserSessionIssuerCIMDAdmissionMode(ctx, testrepo.SetUserSessionIssuerCIMDAdmissionModeParams{
		ClientIDMetadataAdmissionMode: pgtype.Text{String: "", Valid: false},
		ID:                            toolset.UserSessionIssuerID.UUID,
		ProjectID:                     toolset.ProjectID,
	})
	require.NoError(t, err)
}

// admissionDecisionPoints reads the cimd.admission.decisions counter back as
// an attribute-set keyed map.
func admissionDecisionPoints(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "cimd.admission.decisions" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "the admission instrument must be an int64 counter")
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}
	return points
}

// allowCustomCimdURL adds an issuer-specific allowed CIMD document URL,
// the operator-side remedy for a client the catalog does not cover.
func allowCustomCimdURL(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, clientID string) {
	t.Helper()

	row, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuerCimdClient(ctx, usersessions_repo.CreateUserSessionIssuerCimdClientParams{
		ProjectID:           toolset.ProjectID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientIDMetadataUri: clientID,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID.String, "the grant must inherit its issuer's organization")
}

// revokeCustomCimdURL soft-deletes an issuer's custom CIMD URL, the
// operator action whose blast radius the /token asymmetry bounds.
func revokeCustomCimdURL(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, clientID string) {
	t.Helper()

	rows, err := usersessions_repo.New(ti.conn).ListUserSessionIssuerCimdClientsByIssuerID(ctx, usersessions_repo.ListUserSessionIssuerCimdClientsByIssuerIDParams{
		ProjectID:           toolset.ProjectID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Cursor:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:          100,
	})
	require.NoError(t, err)

	for _, row := range rows {
		if row.ClientIDMetadataUri != clientID {
			continue
		}
		_, err := usersessions_repo.New(ti.conn).DeleteUserSessionIssuerCimdClient(ctx, usersessions_repo.DeleteUserSessionIssuerCimdClientParams{
			ID:        row.ID,
			ProjectID: toolset.ProjectID,
		})
		require.NoError(t, err)
		return
	}
	t.Fatalf("no custom cimd client row for %q", clientID)
}

func requireAuthorizeErrorDescription(t *testing.T, w *httptest.ResponseRecorder, wantSubstring string) {
	t.Helper()

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Contains(t, body["error_description"], wantSubstring)
}

// TestCIMDAdmission_DefaultAdmitsWithoutEnforcing pins the resting policy.
// An issuer nobody configured admits every spec-valid client, because a
// presets denial is unrecoverable for the end user and enforcement is
// something an operator chooses rather than something a default imposes.
//
// The URL below is not in the catalog, so presets would refuse it — and the
// request still succeeds, right through to the document fetch.
func TestCIMDAdmission_DefaultAdmitsWithoutEnforcing(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, _ := newTestCIMDService(t)
	fresh := seedFreshIssuerToolset(t, ctx, ti)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, fresh.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	require.Equal(t, http.StatusFound, w.Code, "the default must refuse nobody")
	require.Positive(t, ds.requests.Load(), "an admitted client_id reaches the document fetch")
}

// TestCIMDAdmission_UnsetModeAdmits covers the rows that predate the create
// default. They stay NULL until the backfill reaches them, and they must
// behave exactly like the 'open' the backfill will write, or the deploy and
// the backfill would be two different changes rather than one.
func TestCIMDAdmission_UnsetModeAdmits(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, _ := newTestCIMDService(t)
	legacy := seedFreshIssuerToolset(t, ctx, ti)
	clearIssuerAdmissionMode(t, ctx, ti, legacy)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, legacy.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	require.Equal(t, http.StatusFound, w.Code, "an unset mode must resolve to open, not fail closed")
	require.Positive(t, ds.requests.Load(), "an admitted client_id reaches the document fetch")
}

// TestCIMDAdmission_OpenRecordsShadowDecision is the measurement the open
// default rests on. Open refuses nobody, so without the shadow a catalog gap
// would stop being discoverable the moment open became the resting state —
// and the catalog is still learning from production traffic.
//
// The recorded outcome is admit-shaped because the request WAS admitted. It
// still names what presets would have said, which is the signal an operator
// acts on.
func TestCIMDAdmission_OpenRecordsShadowDecision(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	_, ti, ds, toolset := newTestCIMDServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code, "open admits a client the catalog does not list")

	points := admissionDecisionPoints(t, reader)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModeOpen),
		attr.CIMDAdmissionOutcome(string(admission.AdmitOpenNotListed)),
	)], "an admitted client no rule covers must still name the gap")
	require.Len(t, points, 1, "one request must produce exactly one decision")
}

// TestCIMDAdmission_OpenShadowRecordsOversized: an oversized client_id is
// never handed to the database, so the shadow reaches no verdict about
// whether the catalog covers it. That is the shadow working, not failing, so
// it records its own outcome rather than borrowing the one that means the
// measurement broke — on an unauthenticated endpoint a run of these is a
// probing campaign, and it has to be visible as one.
func TestCIMDAdmission_OpenShadowRecordsOversized(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	_, ti, ds, toolset := newTestCIMDServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	oversized := "https://oversized.example.test/" + strings.Repeat("a", admission.MaxClientIDLength)
	require.Greater(t, len(oversized), admission.MaxClientIDLength)

	verifier := pkceVerifier(t)
	doCIMDAuthorize(t, ti, toolset.McpSlug.String, oversized, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	points := admissionDecisionPoints(t, reader)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModeOpen),
		attr.CIMDAdmissionOutcome(string(admission.AdmitOpenOversized)),
	)], "an oversized client_id must be distinguishable from a broken lookup")
	require.Zero(t, ds.requests.Load(), "an oversized client_id must not reach a document fetch")
}

// TestCIMDAdmission_OpenShadowConsultsCustomURLs: the shadow performs the
// same custom-URL lookup enforcement would, so "no rule anywhere covers this
// client" keeps meaning that rather than the weaker "not in the catalog". An
// issuer that already allowed a URL must not be reported as a catalog gap.
func TestCIMDAdmission_OpenShadowConsultsCustomURLs(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti, ds, toolset := newTestCIMDServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code)

	points := admissionDecisionPoints(t, reader)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModeOpen),
		attr.CIMDAdmissionOutcome(string(admission.AdmitCustom)),
	)], "a URL the issuer already allows is not a catalog gap")
}

// TestCIMDAdmission_PresetsDeniesUnknownURLWithoutFetching is the core
// guarantee, exercised on an issuer that has explicitly opted in to
// enforcement — the only way an issuer enforces.
//
// The no-request assertion is what makes this admission control rather than
// post-fetch filtering.
func TestCIMDAdmission_PresetsDeniesUnknownURLWithoutFetching(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
	require.Zero(t, ds.requests.Load(), "a denied client_id must not cost an outbound document fetch")
}

// TestCIMDAdmission_DefaultAdmitsWhatPresetsRefuses holds the configuration
// constant and varies ONLY the mode, which is the whole claim: the default
// and presets see the same client and differ entirely in what they do about
// it.
//
// An earlier version gave the custom URL to one issuer and not the other, so
// it compared two different configurations and demonstrated nothing about
// mode equivalence. Decision-level equivalence is pinned exhaustively in
// admission.TestEvaluateShadow_DecidesExactlyAsPresets; this covers the
// wiring end to end.
func TestCIMDAdmission_DefaultAdmitsWhatPresetsRefuses(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)

	// Two issuers, neither listing the URL, differing only in mode.
	unconfigured := seedFreshIssuerToolset(t, ctx, ti)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)

	verifier := pkceVerifier(t)

	w := doCIMDAuthorize(t, ti, unconfigured.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code, "the default must admit what presets would refuse")
	require.Positive(t, ds.requests.Load(), "an admitted client_id reaches the document fetch")

	w = doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")

	// A URL the issuer does list is admitted under both, so the difference
	// above is the mode and not the configuration.
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)
	w = doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code, "presets admits a listed URL")

	allowCustomCimdURL(t, ctx, ti, unconfigured, ds.clientID)
	w = doCIMDAuthorize(t, ti, unconfigured.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code, "the default admits a listed URL")
}

// TestCIMDAdmission_PresetsDenialIsActionable: the description is the end
// user's ONLY clue, since MCP clients commit to CIMD at discovery time and
// do not retry via dynamic registration after an authorize rejection. A bare
// "unknown client_id" would leave them with nowhere to go.
func TestCIMDAdmission_PresetsDenialIsActionable(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, t.Context(), ti, toolset, admission.ModePresets)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
	requireAuthorizeErrorDescription(t, w, "not permitted by the server's client policy")
	require.NotContains(t, w.Body.String(), "unknown client_id")
}

// TestCIMDAdmission_CustomURLAdmitted: the operator-side remedy works, and
// the request proceeds all the way to a document fetch.
func TestCIMDAdmission_CustomURLAdmitted(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	require.Equal(t, http.StatusFound, w.Code, "an issuer-allowed URL must be admitted")
	require.Positive(t, ds.requests.Load(), "an admitted client_id must reach the document fetch")
}

// TestCIMDAdmission_CustomURLIsPerIssuer: a URL allowed on one issuer must
// not leak admission to another issuer in the same project.
func TestCIMDAdmission_CustomURLIsPerIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)

	// Allow the URL on a DIFFERENT live issuer in the same project. The
	// admission lookup is issuer-scoped, so this must not admit it here.
	neighbour := seedFreshIssuerToolset(t, ctx, ti)
	setIssuerAdmissionMode(t, ctx, ti, neighbour, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, neighbour, ds.clientID)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
	require.Zero(t, ds.requests.Load(), "another issuer's allowance must not admit this one")

	// The neighbour that DOES allow it is admitted, proving the row exists
	// and the isolation above is scoping rather than a broken fixture.
	w = doCIMDAuthorize(t, ti, neighbour.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code)
}

// TestCIMDAdmission_DisabledDeniesEverything: the off switch is absolute,
// and its description says so rather than pointing at a policy list.
func TestCIMDAdmission_DisabledDeniesEverything(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeDisabled)
	// Even an explicitly-allowed URL is denied while the mode is disabled.
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
	requireAuthorizeErrorDescription(t, w, "does not accept client ID metadata documents")
	require.Zero(t, ds.requests.Load())
}

// TestCIMDAdmission_OpenAdmitsArbitraryValidDocument: open mode skips
// admission but keeps every document validation rule.
func TestCIMDAdmission_OpenAdmitsArbitraryValidDocument(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeOpen)

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	require.Equal(t, http.StatusFound, w.Code)
	require.Positive(t, ds.requests.Load())
}

// TestCIMDAdmission_UnrecognizedModeFailsClosed: a mode value the
// application does not understand is a data error, never an implicit allow.
func TestCIMDAdmission_UnrecognizedModeFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.Mode("allow-everything"))

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
	require.Zero(t, ds.requests.Load())
}

// TestCIMDAdmission_DisabledOmitsMetadataAdvertisement: advertising CIMD
// support while admitting nothing would steer spec-compliant clients into a
// guaranteed-failure flow instead of letting them use dynamic registration.
func TestCIMDAdmission_DisabledOmitsMetadataAdvertisement(t *testing.T) {
	t.Parallel()

	ctx, ti, _, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeDisabled)

	metadata := fetchASMetadata(t, ti, toolset.McpSlug.String)
	_, present := metadata["client_id_metadata_document_supported"]
	require.False(t, present, "a disabled issuer must not advertise CIMD support")
}

// TestCIMDAdmission_PresetsAdvertisesSupport: only `disabled` withdraws the
// advertisement — presets still supports the mechanism, just not every
// client.
func TestCIMDAdmission_PresetsAdvertisesSupport(t *testing.T) {
	t.Parallel()

	ctx, ti, _, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)

	metadata := fetchASMetadata(t, ti, toolset.McpSlug.String)
	require.Equal(t, true, metadata["client_id_metadata_document_supported"])
}

// TestCIMDAdmission_DefaultAdvertisesSupport: an issuer nobody configured
// resolves to open, and every mode but disabled advertises.
func TestCIMDAdmission_DefaultAdvertisesSupport(t *testing.T) {
	t.Parallel()

	ctx, ti, _, _ := newTestCIMDService(t)
	fresh := seedFreshIssuerToolset(t, ctx, ti)

	metadata := fetchASMetadata(t, ti, fresh.McpSlug.String)
	require.Equal(t, true, metadata["client_id_metadata_document_supported"])
}

// TestCIMDAdmission_PortlessLoopbackRedirectMatches protects the catalog
// rather than the admission logic. Three of the seeded presets — Claude
// Code, Zed, and Goose — declare loopback redirect_uris with NO port
// (`http://127.0.0.1/callback`) and bind an ephemeral port at runtime, which
// RFC 8252 §7.3 requires the AS to accept for IP literals.
//
// Admitting a preset whose redirect_uri then fails to match would be the
// worst outcome available: the client is allowed through admission and dies
// one step later, with the same unrecoverable failure a denial would have
// caused. This has shipped as a real bug upstream twice, so it is pinned
// here rather than left to inspection of the matching code.
func TestCIMDAdmission_PortlessLoopbackRedirectMatches(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	// The Claude Code / Zed / Goose shape: portless IP literals, both v4
	// and v6.
	ds.doc["redirect_uris"] = []any{"http://127.0.0.1/callback", "http://[::1]/callback"}

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	require.Equal(t, http.StatusFound, w.Code, "a portless registered loopback URI must match an ephemeral-port request")
}

// TestCIMDAdmission_PortlessLoopbackStillBindsPath: the port is the ONLY
// component the loopback exception may vary. A portless registration must
// not become a wildcard over paths on the same loopback host.
func TestCIMDAdmission_PortlessLoopbackStillBindsPath(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	ds.doc["redirect_uris"] = []any{"http://127.0.0.1/callback"}

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/attacker", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestCIMDAdmission_ChatGPTDocumentShapeAccepted covers the half of ChatGPT
// support that admission alone does not deliver.
//
// The catalog's wildcard entry gets ChatGPT's unbounded per-connector
// client_id past admission (unit-tested in the admission package, since the
// real host cannot be reached from a test). What remains is the document
// itself: OpenAI's documents OMIT token_endpoint_auth_method entirely.
// Gram used to reject that on the RFC 7591 client_secret_basic default,
// which -02 §4.1 makes inapplicable for CIMD.
//
// Without this fix the ChatGPT presets would admit and then fail validation
// one step later — a worse outcome than denying, because the client sees a
// generic failure it cannot fall back from. This test drives the real
// document shape through the full authorize flow.
func TestCIMDAdmission_ChatGPTDocumentShapeAccepted(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	// OpenAI's shape: no token_endpoint_auth_method member at all.
	delete(ds.doc, "token_endpoint_auth_method")

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	require.Equal(t, http.StatusFound, w.Code, "a document omitting token_endpoint_auth_method must be accepted as a public client")

	// It must land as a real public CIMD client row, with no secret.
	clientRow, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            ds.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, ds.clientID, clientRow.ClientIDMetadataUri.String)
	require.False(t, clientRow.ClientSecretHash.Valid, "a CIMD client must never carry a stored secret")
}

// TestCIMDAdmission_ExplicitConfidentialAuthMethodStillRejected: accepting
// an ABSENT auth method must not have opened the door to shared-secret
// clients declaring one explicitly. -02 §4.1 bans every symmetric method
// from a metadata document, since the document is public.
func TestCIMDAdmission_ExplicitConfidentialAuthMethodStillRejected(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	ds.doc["token_endpoint_auth_method"] = "client_secret_basic"

	verifier := pkceVerifier(t)
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(verifier))

	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_client_metadata")
}

// doCIMDToken posts an authorization_code exchange for a CIMD client. The
// code is deliberately bogus: these tests assert on the CLIENT
// AUTHENTICATION outcome, which /token decides before it ever looks at the
// grant, so a client that gets past authentication fails later with
// invalid_grant rather than invalid_client.
func doCIMDToken(t *testing.T, ti *testInstance, mcpSlug, clientID string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "not-a-real-code")
	form.Set("redirect_uri", "http://127.0.0.1:51423/callback")
	form.Set("client_id", clientID)
	form.Set("code_verifier", pkceVerifier(t))

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, req))
	return w
}

// requireTokenOAuthError asserts the RFC 6749 §5.2 error code on a /token
// response.
func requireTokenOAuthError(t *testing.T, w *httptest.ResponseRecorder, wantCode string) {
	t.Helper()

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equalf(t, wantCode, body["error"], "body: %s", w.Body.String())
}

// TestCIMDAdmission_TokenRejectsWhenDisabled: `disabled` is an off switch,
// so it applies to the token leg too. An operator who turns CIMD off for an
// issuer expects outstanding refresh tokens to stop working, not just new
// authorize flows.
func TestCIMDAdmission_TokenRejectsWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeOpen)

	// Establish the CIMD client row while the issuer still admits it.
	mcpSlug := toolset.McpSlug.String
	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t))).Code)

	// Now flip the off switch. The row still exists and is still public.
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeDisabled)

	w := doCIMDToken(t, ti, mcpSlug, ds.clientID)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	requireTokenOAuthError(t, w, "invalid_client")
	requireAuthorizeErrorDescription(t, w, "does not accept client ID metadata documents")
}

// TestCIMDAdmission_TokenAllowsWhenPresetsNoLongerListsClient is the other
// half of the asymmetry, and the one worth pinning hardest because it looks
// like a bug until you know why.
//
// `presets` deliberately does NOT enforce at /token. Preset membership is
// implicit and Gram-mutable — removing a catalog entry de-admits it on every
// presets-mode issuer at deploy — so enforcing here would let a one-line
// catalog edit terminate live sessions fleet-wide, surfacing as a
// mid-session failure no client recovers from.
//
// The client below is admitted at authorize via a custom URL, which is then
// removed. It must still authenticate at /token: the request gets past
// client authentication and fails on the bogus grant instead.
func TestCIMDAdmission_TokenAllowsWhenPresetsNoLongerListsClient(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModePresets)
	allowCustomCimdURL(t, ctx, ti, toolset, ds.clientID)

	mcpSlug := toolset.McpSlug.String
	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t))).Code)

	// De-list the client. New authorize flows are now denied...
	revokeCustomCimdURL(t, ctx, ti, toolset, ds.clientID)
	requireAuthorizeOAuthError(t, doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t))), http.StatusUnauthorized, "invalid_client")

	// ...but the token leg must not be, or removing a URL would revoke live
	// sessions rather than stopping new ones.
	w := doCIMDToken(t, ti, mcpSlug, ds.clientID)
	requireTokenOAuthError(t, w, "invalid_grant")
}

func fetchASMetadata(t *testing.T, ti *testInstance, mcpSlug string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetAuthorizationServer(w, req))
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	return metadata
}
