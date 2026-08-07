package evidence_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/authority"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
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

// quietProbes stands in for the remote probes: discovery finds nothing and
// tools/list returns no declarations, so no section and no gap.
type quietProbes struct{}

func (quietProbes) DiscoverAuthority(_ context.Context, _ string) (*authority.Declaration, error) {
	return nil, nil
}

func (quietProbes) ListToolDeclarations(_ context.Context, _ string) ([]capability.Declaration, error) {
	return nil, nil
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
	}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil}, quietProbes{}, quietProbes{})

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
	}, quietProbes{}, quietProbes{})

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

	clean := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil}, quietProbes{}, quietProbes{})
	raw, err := clean.Assemble(t.Context(), uuid.New(), reference)
	require.NoError(t, err)
	doc := decode(t, raw)
	require.Equal(t, true, doc["package_not_published"])
	require.NotContains(t, doc, "gaps")

	failing := evidence.NewAssembler(&fakePackages{metadata: nil, err: errors.New("registry down")}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil}, quietProbes{}, quietProbes{})
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
	}, quietProbes{}, quietProbes{})

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

	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil}, quietProbes{}, quietProbes{})

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
	}, quietProbes{}, quietProbes{})

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
	assembler := evidence.NewAssembler(client, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil}, quietProbes{}, quietProbes{})

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

	assembler := evidence.NewAssembler(&fakePackages{metadata: nil, err: nil}, &fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil}, quietProbes{}, quietProbes{})
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

// blockingPackages never answers until its context is cancelled, standing in
// for an unreachable registry.
type blockingPackages struct{}

func (blockingPackages) Lookup(ctx context.Context, _ identity.Registry, _ string) (*packagemeta.Metadata, error) {
	<-ctx.Done()

	return nil, fmt.Errorf("lookup aborted: %w", ctx.Err())
}

// An unreachable source costs its own budget, not the caller's whole window:
// the gather returns with the failure recorded as a gap instead of hanging.
func TestAssemble_UnreachableSourceIsBoundedAndBecomesAGap(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(
		blockingPackages{},
		&fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil},
		quietProbes{},
		quietProbes{},
		evidence.WithSourceTimeout(50*time.Millisecond),
	)

	started := time.Now()
	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("npx -y @scope/mcp-server@1.2.3"))
	require.NoError(t, err)
	require.Less(t, time.Since(started), 5*time.Second)

	doc := decode(t, raw)
	gaps, ok := doc["gaps"].([]any)
	require.True(t, ok)
	require.Contains(t, gaps, "package_lookup_failed")
}

// Authority and capability sections are part of the version-1 shape even
// though intake does not populate them yet: a document carrying them must
// round-trip, so gathers that have the inputs need no version bump.
func TestDecodeDocument_CarriesAuthorityAndCapabilities(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
	  "identity": {"kind": "remote", "artifact_ref": "url:https://mcp.example.com/mcp", "version_pinned": false},
	  "authority": {
	    "mode": "oauth", "transport": "streamable-http",
	    "scopes": ["issues:read", "issues:write"], "dynamic_registration": true,
	    "demanded_secrets": [{"name": "EXAMPLE_API_KEY", "required": true}]
	  },
	  "capabilities": [
	    {"tool": "delete_page", "declared": ["destructive"], "acts_on_behalf": true},
	    {"tool": "mystery_tool", "unannotated": true}
	  ]
	}`)

	document, err := evidence.DecodeDocument(raw, evidence.Version)
	require.NoError(t, err)
	require.NotNil(t, document.Authority)
	require.Equal(t, "oauth", document.Authority.Mode)
	require.True(t, document.Authority.DynamicRegistration)
	require.Len(t, document.Authority.DemandedSecrets, 1)
	require.Len(t, document.Capabilities, 2)
	require.True(t, document.Capabilities[0].ActsOnBehalf)
	require.True(t, document.Capabilities[1].Unannotated)
}

// failingProbes stands in for probes against an unreachable or refusing
// server: both gathers fail, and each failure must surface as its own gap.
type failingProbes struct{}

func (failingProbes) DiscoverAuthority(_ context.Context, _ string) (*authority.Declaration, error) {
	return nil, errors.New("well-known unreachable")
}

func (failingProbes) ListToolDeclarations(_ context.Context, _ string) ([]capability.Declaration, error) {
	return nil, errors.New("server refused unauthenticated tools/list")
}

// declaringProbes stands in for a server that publishes OAuth metadata and
// answers tools/list without credentials.
type declaringProbes struct{}

func (declaringProbes) DiscoverAuthority(_ context.Context, _ string) (*authority.Declaration, error) {
	return &authority.Declaration{
		Transport:            "http",
		RequiresOAuth:        true,
		OAuthVersion:         "2.1",
		RegistrationEndpoint: "https://auth.example.com/register",
		Scopes:               []string{"read", "write"},
		Credentials:          nil,
		UnauthenticatedTools: nil,
	}, nil
}

func (declaringProbes) ListToolDeclarations(_ context.Context, _ string) ([]capability.Declaration, error) {
	destructive := true
	return []capability.Declaration{
		{
			Name: "delete_page", Description: "", InputSchema: "",
			ReadOnly: nil, Destructive: &destructive, Idempotent: nil, OpenWorld: nil,
		},
		{
			Name: "mystery", Description: "", InputSchema: "",
			ReadOnly: nil, Destructive: nil, Idempotent: nil, OpenWorld: nil,
		},
	}, nil
}

func TestAssemble_RemoteProbesFillAuthorityAndCapabilities(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(
		&fakePackages{metadata: nil, err: nil},
		&fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil},
		declaringProbes{},
		declaringProbes{},
	)

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("https://mcp.example.com/mcp"))
	require.NoError(t, err)
	doc := decode(t, raw)

	authoritySection, ok := doc["authority"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "oauth", authoritySection["mode"])
	require.Equal(t, true, authoritySection["dynamic_registration"])

	capabilities, ok := doc["capabilities"].([]any)
	require.True(t, ok)
	require.Len(t, capabilities, 2)
	first, ok := capabilities[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "delete_page", first["tool"])
	require.Equal(t, true, first["acts_on_behalf"])
	second, ok := capabilities[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, second["unannotated"], "declaring nothing is carried as unannotated, never as an empty list")
}

// A server that refuses both probes yields two gaps — could-not-consult must
// never read as consulted-and-clean.
func TestAssemble_FailedProbesAreGaps(t *testing.T) {
	t.Parallel()

	assembler := evidence.NewAssembler(
		&fakePackages{metadata: nil, err: nil},
		&fakeTraffic{row: nil, usage: nil, rowErr: nil, usageErr: nil},
		failingProbes{},
		failingProbes{},
	)

	raw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("https://mcp.example.com/mcp"))
	require.NoError(t, err)
	doc := decode(t, raw)

	gaps, ok := doc["gaps"].([]any)
	require.True(t, ok)
	require.Contains(t, gaps, "authority_probe_failed")
	require.Contains(t, gaps, "tool_declarations_probe_failed")
	require.NotContains(t, doc, "authority")
	require.NotContains(t, doc, "capabilities")

	// Package references have no server to probe, so no probe gaps appear.
	packageRaw, err := assembler.Assemble(t.Context(), uuid.New(), identity.Resolve("npx -y @scope/pkg@1.0.0"))
	require.NoError(t, err)
	packageDoc := decode(t, packageRaw)
	require.NotContains(t, packageDoc, "gaps")
}
