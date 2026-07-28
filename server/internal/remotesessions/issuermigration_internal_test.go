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

	require.Equal(t, []string{"issuer", "token_endpoint", "authorization_endpoint"}, endpointMismatches(source, target))
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

	require.Equal(t, []string{"token_endpoint"}, endpointMismatches(withEndpoint, withoutEndpoint))
	require.Equal(t, []string{"token_endpoint"}, endpointMismatches(withoutEndpoint, withEndpoint))
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
	require.Len(t, warnings, 3)
	require.Contains(t, warnings[0], "oidc")
	require.Contains(t, warnings[1], "passthrough")
	require.Contains(t, warnings[2], "scopes_supported")

	// Warnings never block; only endpoint mismatches and binding conflicts do.
	require.Empty(t, endpointMismatches(source, target))
}

func TestMigratePreflight_CanMigrate(t *testing.T) {
	t.Parallel()

	clean := migratePreflight{warnings: []string{"oidc changes"}}
	require.True(t, clean.canMigrate(), "warnings alone must not block a migration")

	mismatched := migratePreflight{endpointMismatches: []string{"issuer"}}
	require.False(t, mismatched.canMigrate())

	conflicted := migratePreflight{conflictingMcpServerNames: []string{"Acme"}}
	require.False(t, conflicted.canMigrate())
}
