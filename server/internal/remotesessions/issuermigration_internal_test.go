// issuermigration_internal_test.go covers the pure decision functions behind
// migrateIssuer — the tenancy scope ladder, the endpoint parity guard, and the
// non-blocking warning set. These need no database, so they live in the internal
// package and enumerate the combinations the handler tests only sample.

package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	orgA = "org-a"
	orgB = "org-b"
)

var (
	projectA = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	projectB = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
)

func projectIssuer(organizationID string, projectID uuid.UUID) repo.RemoteSessionIssuer {
	return repo.RemoteSessionIssuer{
		ProjectID:      conv.ToNullUUID(projectID),
		OrganizationID: conv.ToPGText(organizationID),
	}
}

func orgIssuer(organizationID string) repo.RemoteSessionIssuer {
	return repo.RemoteSessionIssuer{
		ProjectID:      uuid.NullUUID{},
		OrganizationID: conv.ToPGText(organizationID),
	}
}

func globalIssuer() repo.RemoteSessionIssuer {
	return repo.RemoteSessionIssuer{
		ProjectID:      uuid.NullUUID{},
		OrganizationID: pgtype.Text{},
	}
}

func TestScopeOf(t *testing.T) {
	t.Parallel()

	require.Equal(t, issuerScopeProject, scopeOf(projectIssuer(orgA, projectA)))
	require.Equal(t, issuerScopeOrganization, scopeOf(orgIssuer(orgA)))
	require.Equal(t, issuerScopeGlobal, scopeOf(globalIssuer()))
}

// TestValidateMigrationScope_Allowed enumerates every migration the ladder
// permits: sideways within one tenant, and upward into a broader scope that
// still contains the source's tenant.
func TestValidateMigrationScope_Allowed(t *testing.T) {
	t.Parallel()

	allowed := []struct {
		name           string
		source, target repo.RemoteSessionIssuer
	}{
		{"project to same project", projectIssuer(orgA, projectA), projectIssuer(orgA, projectA)},
		{"project to own organization", projectIssuer(orgA, projectA), orgIssuer(orgA)},
		{"project to platform", projectIssuer(orgA, projectA), globalIssuer()},
		{"organization to same organization", orgIssuer(orgA), orgIssuer(orgA)},
		{"organization to platform", orgIssuer(orgA), globalIssuer()},
		{"platform to platform", globalIssuer(), globalIssuer()},
	}

	for _, tc := range allowed {
		require.NoErrorf(t, validateMigrationScope(tc.source, tc.target), "expected %s to be allowed", tc.name)
	}
}

// TestValidateMigrationScope_Rejected enumerates the forbidden migrations:
// anything that narrows scope, and anything that crosses a tenant boundary.
func TestValidateMigrationScope_Rejected(t *testing.T) {
	t.Parallel()

	rejected := []struct {
		name           string
		source, target repo.RemoteSessionIssuer
	}{
		{"organization down to project", orgIssuer(orgA), projectIssuer(orgA, projectA)},
		{"platform down to organization", globalIssuer(), orgIssuer(orgA)},
		{"platform down to project", globalIssuer(), projectIssuer(orgA, projectA)},
		{"project across projects", projectIssuer(orgA, projectA), projectIssuer(orgA, projectB)},
		{"project across organizations", projectIssuer(orgA, projectA), projectIssuer(orgB, projectB)},
		{"project into another organization", projectIssuer(orgA, projectA), orgIssuer(orgB)},
		{"organization across organizations", orgIssuer(orgA), orgIssuer(orgB)},
	}

	for _, tc := range rejected {
		err := validateMigrationScope(tc.source, tc.target)
		require.Errorf(t, err, "expected %s to be rejected", tc.name)

		var scopeErr migrationScopeError
		require.ErrorAsf(t, err, &scopeErr, "expected %s to yield a scope error", tc.name)
		require.NotEmpty(t, scopeErr.reason)
	}
}

func TestEndpointMismatches_IdenticalIssuersMatch(t *testing.T) {
	t.Parallel()

	issuer := repo.RemoteSessionIssuer{
		Issuer:                "https://idp.example.com",
		TokenEndpoint:         conv.ToPGText("https://idp.example.com/token"),
		AuthorizationEndpoint: conv.ToPGText("https://idp.example.com/authorize"),
	}

	require.Empty(t, endpointMismatches(issuer, issuer))
}

func TestEndpointMismatches_ReportsEveryDivergentField(t *testing.T) {
	t.Parallel()

	source := repo.RemoteSessionIssuer{
		Issuer:                "https://idp.example.com",
		TokenEndpoint:         conv.ToPGText("https://idp.example.com/token"),
		AuthorizationEndpoint: conv.ToPGText("https://idp.example.com/authorize"),
	}
	target := repo.RemoteSessionIssuer{
		Issuer:                "https://other.example.com",
		TokenEndpoint:         conv.ToPGText("https://other.example.com/token"),
		AuthorizationEndpoint: conv.ToPGText("https://other.example.com/authorize"),
	}

	mismatches := endpointMismatches(source, target)
	require.Equal(t, []string{"issuer", "token_endpoint", "authorization_endpoint"}, mismatchFieldNames(mismatches))

	// Every blocking field carries both sides' values, which is the whole reason
	// the dialog can show an admin how far apart the two servers are.
	for _, mismatch := range mismatches {
		require.NotNil(t, mismatch.sourceValue, "%s must carry the source value", mismatch.field)
		require.NotNil(t, mismatch.targetValue, "%s must carry the target value", mismatch.field)
		require.Contains(t, *mismatch.sourceValue, "idp.example.com")
		require.Contains(t, *mismatch.targetValue, "other.example.com")
		require.Nil(t, mismatch.sourceValues, "%s is scalar", mismatch.field)
		require.Nil(t, mismatch.targetValues, "%s is scalar", mismatch.field)
	}
}

// TestEndpointMismatches_UnsetAndSetIsAMismatch proves a target that merely
// omits an endpoint the source declares cannot absorb its clients: NULL and a
// value are not interchangeable, even though both sides "agree" on the issuer.
func TestEndpointMismatches_UnsetAndSetIsAMismatch(t *testing.T) {
	t.Parallel()

	withEndpoint := repo.RemoteSessionIssuer{
		Issuer:        "https://idp.example.com",
		TokenEndpoint: conv.ToPGText("https://idp.example.com/token"),
	}
	withoutEndpoint := repo.RemoteSessionIssuer{
		Issuer:        "https://idp.example.com",
		TokenEndpoint: pgtype.Text{},
	}

	declared := endpointMismatches(withEndpoint, withoutEndpoint)
	require.Equal(t, []string{"token_endpoint"}, mismatchFieldNames(declared))

	omitted := endpointMismatches(withoutEndpoint, withEndpoint)
	require.Equal(t, []string{"token_endpoint"}, mismatchFieldNames(omitted))

	// An unset endpoint stays nil rather than collapsing to an empty string, so
	// the preflight can say the target declares none at all rather than showing a
	// blank that reads as a rendering fault.
	require.Equal(t, "https://idp.example.com/token", *declared[0].sourceValue)
	require.Nil(t, declared[0].targetValue)
	require.Nil(t, omitted[0].sourceValue)
	require.Equal(t, "https://idp.example.com/token", *omitted[0].targetValue)
}

// TestEndpointMismatches_BothUnsetMatch proves two issuers that both omit an
// optional endpoint agree on it, rather than tripping the guard on NULL != NULL.
func TestEndpointMismatches_BothUnsetMatch(t *testing.T) {
	t.Parallel()

	issuer := repo.RemoteSessionIssuer{
		Issuer:                "https://idp.example.com",
		TokenEndpoint:         pgtype.Text{},
		AuthorizationEndpoint: pgtype.Text{},
	}

	require.Empty(t, endpointMismatches(issuer, issuer))
}

func TestMigrationWarnings_IdenticalIssuersWarnNothing(t *testing.T) {
	t.Parallel()

	issuer := repo.RemoteSessionIssuer{
		Oidc:            true,
		Passthrough:     true,
		ScopesSupported: []string{"openid", "profile"},
	}

	require.Empty(t, migrationWarnings(issuer, issuer))
}

// TestMigrationWarnings_ReportsDivergenceWithoutBlocking proves oidc,
// passthrough, and scopes_supported surface as warnings — they change how
// migrated sessions refresh, but the target is authoritative and the migration
// proceeds.
func TestMigrationWarnings_ReportsDivergenceWithoutBlocking(t *testing.T) {
	t.Parallel()

	source := repo.RemoteSessionIssuer{
		Oidc:            false,
		Passthrough:     false,
		ScopesSupported: []string{"openid"},
	}
	target := repo.RemoteSessionIssuer{
		Oidc:            true,
		Passthrough:     true,
		ScopesSupported: []string{"openid", "profile"},
	}

	warnings := migrationWarnings(source, target)
	require.Equal(t, []string{"oidc", "passthrough", "scopes_supported"}, mismatchFieldNames(warnings))

	// The booleans carry both sides as rendered scalars, and the scope lists
	// carry both sides as entries, so the dialog can show a delta rather than
	// only naming the field.
	require.Equal(t, "false", *warnings[0].sourceValue)
	require.Equal(t, "true", *warnings[0].targetValue)
	require.Nil(t, warnings[0].sourceValues)

	require.Nil(t, warnings[2].sourceValue)
	require.Equal(t, []string{"openid"}, warnings[2].sourceValues)
	require.Equal(t, []string{"openid", "profile"}, warnings[2].targetValues)

	// Warnings never block; only endpoint mismatches and binding conflicts do.
	require.Empty(t, endpointMismatches(source, target))
}

// TestMigrationWarnings_ScopeOrderIsNotADivergence proves a target that lists
// the same scopes in another order grants migrated clients exactly what they
// had. Warning about it would render as two visually identical lists.
func TestMigrationWarnings_ScopeOrderIsNotADivergence(t *testing.T) {
	t.Parallel()

	source := repo.RemoteSessionIssuer{
		ScopesSupported: []string{"openid", "profile", "email"},
	}
	target := repo.RemoteSessionIssuer{
		ScopesSupported: []string{"email", "openid", "profile"},
	}

	require.Empty(t, migrationWarnings(source, target))
}

// TestMigrationWarnings_RepeatedScopeIsNotADivergence proves a repeat is as
// meaningless as an ordering: both sides offer openid, so the migrated clients
// gain and lose nothing and there is nothing to warn about.
func TestMigrationWarnings_RepeatedScopeIsNotADivergence(t *testing.T) {
	t.Parallel()

	source := repo.RemoteSessionIssuer{
		ScopesSupported: []string{"openid", "openid"},
	}
	target := repo.RemoteSessionIssuer{
		ScopesSupported: []string{"openid"},
	}

	require.Empty(t, migrationWarnings(source, target))
}

// TestMigrationWarnings_RepeatDoesNotMaskARealChange covers the two together: a
// list that both repeats an entry and drops another still diverges on the entry,
// and the repeat must not distract from it.
func TestMigrationWarnings_RepeatDoesNotMaskARealChange(t *testing.T) {
	t.Parallel()

	source := repo.RemoteSessionIssuer{
		ScopesSupported: []string{"openid", "openid", "email"},
	}
	target := repo.RemoteSessionIssuer{
		ScopesSupported: []string{"openid", "profile"},
	}

	warnings := migrationWarnings(source, target)
	require.Equal(t, []string{"scopes_supported"}, mismatchFieldNames(warnings))
	require.Equal(t, []string{"openid", "openid", "email"}, warnings[0].sourceValues, "the stored list is reported as stored")
}

func TestMigratePreflight_CanMigrate(t *testing.T) {
	t.Parallel()

	clean := migratePreflight{warnings: []issuerFieldMismatch{{field: "oidc", sourceValue: nil, targetValue: nil, sourceValues: nil, targetValues: nil}}}
	require.True(t, clean.canMigrate(), "warnings alone must not block a migration")

	mismatched := migratePreflight{endpointMismatches: []issuerFieldMismatch{{field: "issuer", sourceValue: nil, targetValue: nil, sourceValues: nil, targetValues: nil}}}
	require.False(t, mismatched.canMigrate())

	conflicted := migratePreflight{conflictingMcpServerNames: []string{"Acme"}}
	require.False(t, conflicted.canMigrate())
}

// TestIssuerURLsCanonicallyEqual_CollapsesEquivalentSpellings covers the axes
// parseCanonicalIssuerURL treats as one upstream. These are the duplicates
// consolidation exists to clean up, and discovery finds candidates by the same
// equality, so a stricter comparison here would surface candidates that could
// never be migrated.
func TestIssuerURLsCanonicallyEqual_CollapsesEquivalentSpellings(t *testing.T) {
	t.Parallel()

	require.True(t, issuerURLsCanonicallyEqual("https://idp.example.com", "https://idp.example.com/"))
	require.True(t, issuerURLsCanonicallyEqual("https://idp.example.com", "https://idp.example.com:443"))
	require.True(t, issuerURLsCanonicallyEqual("https://IDP.example.com", "https://idp.example.com"))
	require.True(t, issuerURLsCanonicallyEqual("http://idp.example.com:80/x", "http://idp.example.com/x"))
}

// TestIssuerURLsCanonicallyEqual_KeepsDistinctUpstreamsApart pins the axes that
// stay distinct. http and https are deliberately different: same host, different
// security properties.
func TestIssuerURLsCanonicallyEqual_KeepsDistinctUpstreamsApart(t *testing.T) {
	t.Parallel()

	require.False(t, issuerURLsCanonicallyEqual("https://idp.example.com", "http://idp.example.com"))
	require.False(t, issuerURLsCanonicallyEqual("https://idp.example.com", "https://other.example.com"))
	require.False(t, issuerURLsCanonicallyEqual("https://idp.example.com/a", "https://idp.example.com/A"))
	require.False(t, issuerURLsCanonicallyEqual("https://idp.example.com", "https://idp.example.com:8443"))
}

// TestIssuerURLsCanonicallyEqual_UnparseableIsOnlyEqualToItself proves a stored
// value that is not an issuer identifier never widens the comparison. Rows
// predating URL validation can hold anything, and migration must not decide two
// of them name the same authorization server on input it could not understand.
func TestIssuerURLsCanonicallyEqual_UnparseableIsOnlyEqualToItself(t *testing.T) {
	t.Parallel()

	require.True(t, issuerURLsCanonicallyEqual("not a url", "not a url"))
	require.False(t, issuerURLsCanonicallyEqual("not a url", "not a url/"))
	require.False(t, issuerURLsCanonicallyEqual("not a url", "https://idp.example.com"))
	require.False(t, issuerURLsCanonicallyEqual("", "https://idp.example.com"))
}
