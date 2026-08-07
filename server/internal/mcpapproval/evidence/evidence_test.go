package evidence_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// fakeTraffic stands in for the ClickHouse-backed telemetry repo.
type fakeTraffic struct {
	row      *telemetryrepo.ShadowMCPInventoryURLRow
	usage    []telemetryrepo.ShadowMCPInventoryUsageRow
	rowErr   error
	usageErr error
}

func (f *fakeTraffic) GetShadowMCPInventoryURL(_ context.Context, _ telemetryrepo.GetShadowMCPInventoryURLParams) (*telemetryrepo.ShadowMCPInventoryURLRow, error) {
	return f.row, f.rowErr
}

func (f *fakeTraffic) ListShadowMCPInventoryUsage(_ context.Context, _ telemetryrepo.ListShadowMCPInventoryUsageParams) ([]telemetryrepo.ShadowMCPInventoryUsageRow, error) {
	return f.usage, f.usageErr
}

// fakePackages returns a canned lookup result.
type fakePackages struct {
	metadata *packagemeta.Metadata
	err      error
}

func (f *fakePackages) Lookup(_ context.Context, _ identity.Registry, _ string) (*packagemeta.Metadata, error) {
	return f.metadata, f.err
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))

	return doc
}

func TestAssemble_PackageReference(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(&fakePackages{
		metadata: &packagemeta.Metadata{
			Registry: identity.RegistryNPM, Name: "@scope/mcp-server", License: "MIT",
			LatestVersion: "1.2.3", FirstPublished: time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC),
			LastPublished: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
			VersionCount:  3, MaintainerCount: 2, Deprecated: false, DeprecationReason: "",
		},
		err: nil,
	}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil})

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("npx -y @scope/mcp-server@1.2.3"))
	require.NoError(t, err)
	doc := decode(t, raw)

	identitySection, ok := doc["identity"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "package", identitySection["kind"])
	require.Equal(t, "npm:@scope/mcp-server@1.2.3", identitySection["artifact_ref"])
	require.Equal(t, true, identitySection["version_pinned"])

	packageSection, ok := doc["package"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "MIT", packageSection["license"])
	require.Equal(t, "2023-01-15T00:00:00Z", packageSection["first_published"])

	// A package reference has no URL, so exposure is not asked about.
	require.NotContains(t, doc, "exposure")
	require.NotContains(t, doc, "gaps")
}

func TestAssemble_RemoteReferenceCarriesExposure(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	last := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{
		row: &telemetryrepo.ShadowMCPInventoryURLRow{
			CanonicalServerURL: "https://mcp.example.com/sse", URLHost: "mcp.example.com",
			ServerName: "example", ServerNameOverride: "", FirstSeen: first, LastSeen: last,
			LastCalledUnixNano: 0, UpdatedAt: time.Time{},
		},
		usage: []telemetryrepo.ShadowMCPInventoryUsageRow{{
			CanonicalServerURL: "https://mcp.example.com/sse", ServerName: "example",
			FirstCalled: &first, LastCalled: &last, CallCount: 42, UserCount: 7, TopUsers: nil,
		}},
		rowErr: nil, usageErr: nil,
	})

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("https://mcp.example.com/sse"))
	require.NoError(t, err)
	doc := decode(t, raw)

	exposureSection, ok := doc["exposure"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "seen", exposureSection["status"])
	require.InDelta(t, float64(7), exposureSection["user_count"], 0)
	require.Equal(t, true, exposureSection["in_use"])
	require.NotContains(t, doc, "package")
}

// The registry not knowing a package is a finding, distinct from a lookup
// that failed — and a failed lookup is a gap, never silence.
func TestAssemble_NotPublishedVersusGap(t *testing.T) {
	t.Parallel()

	reference := identity.Resolve("npx -y @scope/unknown-server")

	clean := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil})
	raw, err := clean.Assemble(t.Context(), uuid.New(), reference)
	require.NoError(t, err)
	doc := decode(t, raw)
	require.Equal(t, true, doc["package_not_published"])
	require.NotContains(t, doc, "gaps")

	failing := evidence.NewAssembler(&fakePackages{metadata: nil, err: errors.New("registry down")}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil})
	raw, err = failing.Assemble(t.Context(), uuid.New(), reference)
	require.NoError(t, err, "one source failing must not lose the gather")
	doc = decode(t, raw)
	require.NotContains(t, doc, "package_not_published")
	require.Contains(t, doc, "gaps")
	gaps, ok := doc["gaps"].([]any)
	require.True(t, ok)
	require.Contains(t, gaps, "package_lookup_failed")
}

func TestAssemble_ExposureFailureIsAGap(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{
		row: nil, usage: nil, rowErr: errors.New("clickhouse down"), usageErr: nil,
	})

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("https://mcp.example.com/sse"))
	require.NoError(t, err)
	doc := decode(t, raw)
	gaps, ok := doc["gaps"].([]any)
	require.True(t, ok)
	require.Contains(t, gaps, "exposure_lookup_failed")
	require.NotContains(t, doc, "exposure")
}

// An unresolved reference still yields a document: the panel must show the
// reference as unknown, not render nothing.
func TestAssemble_UnresolvedIsStillADocument(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil})

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("./run-my-server --local"))
	require.NoError(t, err)
	doc := decode(t, raw)

	identitySection, ok := doc["identity"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "unresolved", identitySection["kind"])
	require.NotContains(t, identitySection, "artifact_ref")
	require.NotContains(t, doc, "package")
	require.NotContains(t, doc, "exposure")
}

// An mcp-remote stdio command resolves to the URL it proxies, and the
// exposure lookup runs against that URL — the documented resolved-target
// contract.
func TestAssemble_MCPRemoteCommandReachesExposure(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{
		row: &telemetryrepo.ShadowMCPInventoryURLRow{
			CanonicalServerURL: "https://mcp.example.com/sse", URLHost: "mcp.example.com",
			ServerName: "example", ServerNameOverride: "", FirstSeen: first, LastSeen: first,
			LastCalledUnixNano: 0, UpdatedAt: time.Time{},
		},
		usage: nil, rowErr: nil, usageErr: nil,
	})

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("npx -y mcp-remote https://mcp.example.com/sse"))
	require.NoError(t, err)
	doc := decode(t, raw)

	exposureSection, ok := doc["exposure"].(map[string]any)
	require.True(t, ok, "an mcp-remote command must reach the inventory through its resolved URL")
	require.Equal(t, "seen", exposureSection["status"])
}

// The real packagemeta client satisfies the assembler's interface end to end.
func TestAssemble_WithRealPackageClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"p","dist-tags":{"latest":"1.0.0"},"time":{"created":"2024-01-01T00:00:00.000Z","1.0.0":"2024-01-01T00:00:00.000Z"},"versions":{"1.0.0":{}},"license":"MIT"}`))
	}))
	t.Cleanup(server.Close)

	client := packagemeta.NewClient(server.Client(), packagemeta.WithNPMBaseURL(server.URL))
	assembler := evidence.NewAssembler(client, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil})

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("npx -y p@1.0.0"))
	require.NoError(t, err)
	doc := decode(t, raw)
	packageSection, ok := doc["package"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "MIT", packageSection["license"])
}

// A stored document round-trips through DecodeDocument, and an unknown
// version is refused rather than misread — a frozen snapshot must stay
// interpretable after the shape moves on.
func TestDecodeDocument_RoundTripAndVersionGate(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil})
	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("npx -y @scope/mcp-server@1.2.3"))
	require.NoError(t, err)

	document, err := evidence.DecodeDocument(raw, evidence.Version)
	require.NoError(t, err)
	require.Equal(t, "package", document.Identity.Kind)
	require.Equal(t, "npm:@scope/mcp-server@1.2.3", document.Identity.ArtifactRef)
	require.True(t, document.PackageNotPublished)

	_, err = evidence.DecodeDocument(raw, evidence.Version+1)
	require.Error(t, err)
}
