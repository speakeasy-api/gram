package evidencediff_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidencediff"
)

func baseDocument() evidence.Document {
	return evidence.Document{
		Identity: evidence.IdentitySection{
			Kind:              "server_url",
			ArtifactRef:       "",
			VersionPinned:     false,
			Host:              "mcp.example.com",
			RegistrableDomain: "example.com",
			Registry:          "",
			PackageName:       "",
			PackageVersion:    "",
		},
		Package:             &evidence.PackageSection{Registry: "npm", Name: "@scope/mcp", License: "", LatestVersion: "1.2.3", FirstPublished: "", LastPublished: "", VersionCount: 0, MaintainerCount: 3, Deprecated: false, DeprecationReason: ""},
		PackageNotPublished: false,
		Repository:          nil,
		RepositoryNotFound:  false,
		Advisories:          &evidence.AdvisoriesSection{Ecosystem: "npm", Package: "@scope/mcp", KnownCount: 1, Advisories: []evidence.AdvisoryItem{{ID: "GHSA-1111", Summary: "old one", Severity: "moderate", Published: ""}}},
		Domain:              nil,
		Exposure:            nil,
		Authority: &evidence.AuthoritySection{
			Mode:                 "oauth",
			Transport:            "http",
			Scopes:               []string{"read:messages", "read:profile"},
			DynamicRegistration:  false,
			DemandedSecrets:      []evidence.CredentialSection{{Name: "API_KEY", Required: true, Description: ""}},
			OptionalSecrets:      nil,
			UnauthenticatedTools: nil,
			Undeclared:           false,
		},
		Capabilities:       nil,
		CapabilitiesSource: "",
		Provenance:         nil,
		Gaps:               nil,
	}
}

func TestCompare_NoDrift(t *testing.T) {
	t.Parallel()

	first := baseDocument()
	second := baseDocument()
	diff := evidencediff.Compare(first, second)
	require.False(t, diff.Changed)
}

func TestCompare_PermissionDrift(t *testing.T) {
	t.Parallel()

	before := baseDocument()
	after := baseDocument()
	after.Authority.Scopes = []string{"read:messages", "write:messages"}
	after.Authority.Mode = "oauth_dcr"
	after.Authority.DemandedSecrets = []evidence.CredentialSection{
		{Name: "API_KEY", Required: true, Description: ""},
		{Name: "ADMIN_TOKEN", Required: true, Description: ""},
	}
	after.Advisories.KnownCount = 2
	after.Advisories.Advisories = append(after.Advisories.Advisories, evidence.AdvisoryItem{ID: "GHSA-2222", Summary: "new one", Severity: "high", Published: ""})

	diff := evidencediff.Compare(before, after)
	require.True(t, diff.Changed)
	require.Equal(t, []string{"write:messages"}, diff.ScopesAdded)
	require.Equal(t, []string{"read:profile"}, diff.ScopesRemoved)
	require.Equal(t, []string{"ADMIN_TOKEN"}, diff.SecretsAdded)
	require.Empty(t, diff.SecretsRemoved)
	require.Len(t, diff.AdvisoriesAdded, 1)
	require.Equal(t, "GHSA-2222", diff.AdvisoriesAdded[0].ID)

	fields := make(map[string][2]string, len(diff.Fields))
	for _, change := range diff.Fields {
		fields[change.Field] = [2]string{change.Before, change.After}
	}
	require.Equal(t, [2]string{"oauth", "oauth_dcr"}, fields[evidencediff.FieldAuthorityMode])
	require.Equal(t, [2]string{"1", "2"}, fields[evidencediff.FieldKnownAdvisories])
}

// Tool declarations, versions, traffic, and repository statistics move
// constantly without widening what the server may do, so none of them count
// as drift.
func TestCompare_IgnoresNonPermissionChurn(t *testing.T) {
	t.Parallel()

	before := baseDocument()
	after := baseDocument()
	after.Capabilities = []evidence.CapabilitySection{{Tool: "new_tool", Declared: []string{"write"}, SchemaImplied: nil, ActsOnBehalf: false, Unannotated: false, ReadOnlyHint: nil, Destructive: nil, Idempotent: nil, OpenWorld: nil}}
	after.Package.LatestVersion = "9.9.9"
	after.Package.VersionCount = 40
	after.Package.MaintainerCount = 1
	after.Exposure = &evidence.ExposureSection{Status: "active", CanonicalURL: "", URLHost: "", ServerName: "", FirstSeen: "", LastSeen: "", FirstCalled: "", LastCalled: "", CallCount: 999, UserCount: 12, InUse: true}

	diff := evidencediff.Compare(before, after)
	require.False(t, diff.Changed)
}

// A gather that recorded a gap did not consult that source; comparing
// against it would report every scope as removed one sweep and restored the
// next.
func TestCompare_SkipsGappedSections(t *testing.T) {
	t.Parallel()

	before := baseDocument()
	after := baseDocument()
	after.Authority = nil
	after.Advisories = nil
	after.Gaps = []string{evidence.GapAuthorityProbe, evidence.GapAdvisoryLookup}

	require.False(t, evidencediff.Compare(before, after).Changed)
	require.False(t, evidencediff.Compare(after, before).Changed)
}

// A server that publishes no OAuth metadata gathers cleanly to a nil
// authority section with no gap. That is checked-and-absent, not unknown —
// and a vendor dropping OAuth for a static long-lived key is the largest
// widening of a standing approval there is, so it must fire.
func TestCompare_DroppingOAuthEntirelyFires(t *testing.T) {
	t.Parallel()

	before := baseDocument()
	after := baseDocument()
	after.Authority = nil

	diff := evidencediff.Compare(before, after)
	require.True(t, diff.Changed, "losing published OAuth metadata is drift, not silence")
	require.Equal(t, []string{"read:messages", "read:profile"}, diff.ScopesRemoved)
	require.Equal(t, []string{"API_KEY"}, diff.SecretsRemoved)

	// And the mirror: a server that published nothing at approval time and
	// now advertises OAuth.
	mirror := evidencediff.Compare(after, before)
	require.True(t, mirror.Changed)
	require.Equal(t, []string{"read:messages", "read:profile"}, mirror.ScopesAdded)
}

// The announce-once key is a hash of what an announcement would say. A
// source that flaps between reachable and gapped must not re-key an
// otherwise identical drift, or every flap re-notifies the same news.
func TestFingerprint_StableAcrossUnrelatedGaps(t *testing.T) {
	t.Parallel()

	before := baseDocument()

	reachable := baseDocument()
	reachable.Authority.Scopes = []string{"read:messages", "read:profile", "write:messages"}

	gapped := baseDocument()
	gapped.Authority.Scopes = []string{"read:messages", "read:profile", "write:messages"}
	gapped.Advisories = nil
	gapped.Gaps = []string{evidence.GapAdvisoryLookup}

	first := evidencediff.Compare(before, reachable)
	second := evidencediff.Compare(before, gapped)
	require.True(t, first.Changed)
	require.Equal(t, first, second, "the advisory gap does not change what is reported")
	require.Equal(t, evidencediff.Fingerprint(first), evidencediff.Fingerprint(second))
}

// A materially different drift is news again.
func TestFingerprint_ChangesWithTheDrift(t *testing.T) {
	t.Parallel()

	before := baseDocument()

	one := baseDocument()
	one.Authority.Scopes = append(one.Authority.Scopes, "write:messages")

	two := baseDocument()
	two.Authority.Scopes = append(two.Authority.Scopes, "write:messages", "admin:write")

	require.NotEqual(t,
		evidencediff.Fingerprint(evidencediff.Compare(before, one)),
		evidencediff.Fingerprint(evidencediff.Compare(before, two)))
}

// An advisory id dropping out of the most-recent sample is not a withdrawal
// — newer advisories push older ones out — so only additions surface.
func TestCompare_AdvisorySampleRotation(t *testing.T) {
	t.Parallel()

	before := baseDocument()
	after := baseDocument()
	after.Advisories.Advisories = []evidence.AdvisoryItem{{ID: "GHSA-2222", Summary: "newer", Severity: "high", Published: ""}}

	diff := evidencediff.Compare(before, after)
	require.True(t, diff.Changed)
	require.Len(t, diff.AdvisoriesAdded, 1)
	require.Equal(t, "GHSA-2222", diff.AdvisoriesAdded[0].ID)
}
