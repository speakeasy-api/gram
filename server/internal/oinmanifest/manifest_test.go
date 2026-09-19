package oinmanifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oinmanifest/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var (
	fixtureNow    = time.Date(2026, 9, 18, 23, 10, 0, 0, time.UTC)
	fixtureConfig = Config{ListingName: "Speakeasy", OrgDomain: "example.com", ServerURL: "https://gram.example.test/"}
	idjagGrants   = []string{"authorization_code", grantTypeJWTBearer}
	idjagProfiles = []string{grantProfileIDJAG}
)

func issuerRow(id uuid.UUID, issuer, name string) repo.ListGlobalIDJAGIssuersRow {
	return repo.ListGlobalIDJAGIssuersRow{
		IssuerRowID:            id,
		Issuer:                 issuer,
		IssuerName:             pgtype.Text{String: name, Valid: name != ""},
		ScopesSupported:        []string{"read", "write"},
		GrantTypesSupported:    idjagGrants,
		GrantProfilesSupported: idjagProfiles,
		MetadataFetchedAt:      pgtype.Timestamptz{Time: fixtureNow.Add(-24 * time.Hour), Valid: true},
	}
}

func withClient(row repo.ListGlobalIDJAGIssuersRow, clientID string, scopes []string) repo.ListGlobalIDJAGIssuersRow {
	row.ClientRowID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	row.ClientID = pgtype.Text{String: clientID, Valid: true}
	row.ClientScope = scopes
	row.TokenEndpointAuthMethod = pgtype.Text{String: "none", Valid: true}
	return row
}

func TestBuildPairWithClientOnlyCarriesTheAudienceBlocker(t *testing.T) {
	t.Parallel()

	row := withClient(issuerRow(uuid.New(), "https://as.example.test", "Example"), "client-1", []string{"read"})
	row.ResourceIdentifier = pgtype.Text{String: "https://as.example.test/mcp", Valid: true}
	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{row}, fixtureConfig, fixtureNow)

	require.Equal(t, ManifestVersion, manifest.ManifestVersion)
	require.Equal(t, "Speakeasy", manifest.RequestingApp.Name)
	require.Equal(t, "https://gram.example.test/oauth/callback", manifest.RequestingApp.RedirectURI)
	require.Equal(t, TrustRequirements, manifest.TrustRequirements)
	require.Len(t, manifest.ResourceRegistrations, 1)
	registration := manifest.ResourceRegistrations[0]
	require.Equal(t, "Example", registration.ResourceName)
	require.Equal(t, "https://as.example.test/mcp", registration.ResourceIdentifier)
	require.Equal(t, "client-1", *registration.ClientID)
	require.Equal(t, registrationStatic, registration.Registration)
	require.Equal(t, []string{"read"}, registration.Scopes)
	require.Equal(t, scopesSourceClient, registration.ScopesSource)
	require.Nil(t, registration.XAAAudience)
	require.Equal(t, []string{blockerAudience}, registration.Blockers)
	require.Equal(t, evidenceUnavailable, registration.Evidence.Status)
	require.Equal(t, []string{"https://as.example.test/mcp"}, registration.ResourceAppExpectations.ResourceIdentifiers)
	require.Equal(t, []string{"read"}, registration.ResourceAppExpectations.Scopes)
	require.Equal(t, Summary{Registrations: 1, Ready: 0, Blocked: 1}, manifest.Summary)
}

func TestBuildMissingClientIsBlocked(t *testing.T) {
	t.Parallel()

	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{issuerRow(uuid.New(), "https://as.example.test", "")}, fixtureConfig, fixtureNow)

	registration := manifest.ResourceRegistrations[0]
	require.Equal(t, "https://as.example.test", registration.ResourceName)
	require.Nil(t, registration.ClientID)
	require.Equal(t, registrationNone, registration.Registration)
	require.Equal(t, []string{"read", "write"}, registration.Scopes)
	require.Equal(t, scopesSourceIssuer, registration.ScopesSource)
	require.Contains(t, registration.Blockers, blockerNoClient)
	require.Equal(t, 1, manifest.Summary.Blocked)
}

func TestBuildTwoClientsCollapseWithBlocker(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	first := withClient(issuerRow(id, "https://as.example.test", "Example"), "client-a", []string{"read"})
	second := withClient(issuerRow(id, "https://as.example.test", "Example"), "client-b", []string{"read"})
	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{first, second}, fixtureConfig, fixtureNow)

	require.Len(t, manifest.ResourceRegistrations, 1)
	registration := manifest.ResourceRegistrations[0]
	require.Equal(t, "client-a", *registration.ClientID)
	require.Contains(t, registration.Blockers, "more than one global client registered (2)")
}

func TestBuildCIMDRegistrationAndScopeFallback(t *testing.T) {
	t.Parallel()

	row := withClient(issuerRow(uuid.New(), "https://as.example.test", "Example"), "https://gram.example.test/cimd/x.json", nil)
	row.ClientIDMetadataUri = pgtype.Text{String: "https://gram.example.test/cimd/x.json", Valid: true}
	row.CimdSupported = true
	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{row}, fixtureConfig, fixtureNow)

	registration := manifest.ResourceRegistrations[0]
	require.Equal(t, registrationCIMD, registration.Registration)
	require.True(t, registration.CIMDSupported)
	require.Equal(t, []string{"read", "write"}, registration.Scopes)
	require.Equal(t, scopesSourceIssuer, registration.ScopesSource)
}

func TestBuildNoScopesAnywhereIsBlocked(t *testing.T) {
	t.Parallel()

	row := withClient(issuerRow(uuid.New(), "https://as.example.test", "Example"), "client-1", nil)
	row.ScopesSupported = nil
	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{row}, fixtureConfig, fixtureNow)

	registration := manifest.ResourceRegistrations[0]
	require.Equal(t, []string{}, registration.Scopes)
	require.Equal(t, scopesSourceNone, registration.ScopesSource)
	require.Contains(t, registration.Blockers, blockerNoScopes)
}

func TestBuildMetadataBlockers(t *testing.T) {
	t.Parallel()

	stale := withClient(issuerRow(uuid.New(), "https://stale.example.test", "Stale"), "client-1", []string{"read"})
	stale.MetadataFetchedAt = pgtype.Timestamptz{Time: fixtureNow.Add(-31 * 24 * time.Hour), Valid: true}
	never := withClient(issuerRow(uuid.New(), "https://never.example.test", "Never"), "client-2", []string{"read"})
	never.MetadataFetchedAt = pgtype.Timestamptz{}
	failed := withClient(issuerRow(uuid.New(), "https://failed.example.test", "Failed"), "client-3", []string{"read"})
	failed.MetadataLastError = pgtype.Text{String: "well-known returned 503", Valid: true}
	rejected := withClient(issuerRow(uuid.New(), "https://rejected.example.test", "Rejected"), "client-4", []string{"read"})
	rejected.UpstreamRejectedAt = pgtype.Timestamptz{Time: fixtureNow.Add(-time.Hour), Valid: true}

	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{stale, never, failed, rejected}, fixtureConfig, fixtureNow)

	byName := map[string]Registration{}
	for _, registration := range manifest.ResourceRegistrations {
		byName[registration.ResourceName] = registration
	}
	require.Contains(t, byName["Stale"].Blockers, blockerStaleMetadata)
	require.Contains(t, byName["Never"].Blockers, blockerNoMetadata)
	require.Nil(t, byName["Never"].MetadataFetchedAt)
	require.Contains(t, byName["Failed"].Blockers, "issuer metadata refresh failed: well-known returned 503")
	require.Contains(t, byName["Rejected"].Blockers, blockerRejected+" at 2026-09-18T22:10:00Z")
	require.Equal(t, []string{"https://failed.example.test", "https://never.example.test", "https://rejected.example.test", "https://stale.example.test"}, []string{
		manifest.ResourceRegistrations[0].ResourceASIssuer, manifest.ResourceRegistrations[1].ResourceASIssuer,
		manifest.ResourceRegistrations[2].ResourceASIssuer, manifest.ResourceRegistrations[3].ResourceASIssuer,
	})
}

func TestBuildUnsetListingRendersUnset(t *testing.T) {
	t.Parallel()

	manifest := Build(nil, Config{ListingName: "  ", OrgDomain: "", ServerURL: "https://gram.example.test"}, fixtureNow)

	require.Equal(t, unsetValue, manifest.RequestingApp.Name)
	require.Equal(t, unsetValue, manifest.RequestingApp.OrgDomain)
	require.Empty(t, manifest.ResourceRegistrations)
	require.Equal(t, Summary{Registrations: 0, Ready: 0, Blocked: 0}, manifest.Summary)
}

func TestAdvertisesIDJAGRequiresBothSets(t *testing.T) {
	t.Parallel()

	require.True(t, AdvertisesIDJAG(idjagGrants, idjagProfiles))
	require.False(t, AdvertisesIDJAG([]string{"authorization_code"}, idjagProfiles))
	require.False(t, AdvertisesIDJAG(idjagGrants, nil))
	require.False(t, AdvertisesIDJAG(nil, nil))
}

func TestRenderMarkdownEscapesAndOrdersDeterministically(t *testing.T) {
	t.Parallel()

	ready := withClient(issuerRow(uuid.New(), "https://b.example.test", "Pipe | name\nsecond line"), "client-b", []string{"read"})
	blocked := issuerRow(uuid.New(), "https://a.example.test", "# heading")
	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{ready, blocked}, fixtureConfig, fixtureNow)
	// Clear the audience blocker on one pair so the "Ready" table has a row.
	manifest.ResourceRegistrations[1].Blockers = nil
	manifest.Summary = Summary{Registrations: 2, Ready: 1, Blocked: 1}

	markdown := string(RenderMarkdown(manifest))

	require.Contains(t, markdown, "# Speakeasy OIN Cross App Access manifest")
	require.Contains(t, markdown, "Generated at 2026-09-18T23:10:00Z")
	require.Contains(t, markdown, `| Pipe \| name second line | https://b.example.test |`)
	require.Contains(t, markdown, `| \# heading | https://a.example.test |`)
	require.Contains(t, markdown, "### Not ready")
	require.Less(t, strings.Index(markdown, "### Ready"), strings.Index(markdown, "### Not ready"))
	require.Contains(t, markdown, "| Listing name | Speakeasy |")
	require.Contains(t, markdown, "no global client registered; audience unknown")
	require.Equal(t, markdown, string(RenderMarkdown(manifest)))
}

func TestRenderMarkdownOmitsNotReadyWhenEverythingIsReady(t *testing.T) {
	t.Parallel()

	manifest := Build([]repo.ListGlobalIDJAGIssuersRow{withClient(issuerRow(uuid.New(), "https://a.example.test", "A"), "client-a", []string{"read"})}, fixtureConfig, fixtureNow)
	manifest.ResourceRegistrations[0].Blockers = nil
	manifest.Summary = Summary{Registrations: 1, Ready: 1, Blocked: 0}

	markdown := string(RenderMarkdown(manifest))

	require.NotContains(t, markdown, "### Not ready")
	require.Contains(t, markdown, "| A | https://a.example.test | https://a.example.test | unknown | client-a | read | static | none |")
}

func TestEscapeCell(t *testing.T) {
	t.Parallel()

	require.Equal(t, `a \| b`, escapeCell("a | b"))
	require.Equal(t, "line one line two", escapeCell("line one\nline two"))
	require.Equal(t, `\- item`, escapeCell("- item"))
	require.Equal(t, "plain", escapeCell("plain\x00"))
	require.Empty(t, escapeCell("   "))
}

func TestRenderJSONMatchesGolden(t *testing.T) {
	t.Parallel()

	issuerID := uuid.MustParse("00000000-0000-0000-0000-00000000aaaa")
	row := withClient(issuerRow(issuerID, "https://as.example.test", "Example"), "client-1", []string{"read"})
	row.ClientRowID = uuid.NullUUID{UUID: uuid.MustParse("00000000-0000-0000-0000-00000000bbbb"), Valid: true}
	row.ResourceIdentifier = pgtype.Text{String: "https://as.example.test/mcp", Valid: true}
	noClient := issuerRow(uuid.MustParse("00000000-0000-0000-0000-00000000cccc"), "https://empty.example.test", "")

	body, err := RenderJSON(Build([]repo.ListGlobalIDJAGIssuersRow{row, noClient}, fixtureConfig, fixtureNow))
	require.NoError(t, err)

	path := filepath.Join("fixtures", "manifest.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.WriteFile(path, body, 0o600))
	}
	require.Equal(t, string(testenv.ReadFixture(t, path)), string(body))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.InDelta(t, ManifestVersion, decoded["manifest_version"], 0)
}
