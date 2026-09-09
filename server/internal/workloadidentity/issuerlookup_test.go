package workloadidentity_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

const testIssuerURL = "https://token.actions.githubusercontent.com"

func projectTier(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: true}
}

func organizationTier() uuid.NullUUID {
	return uuid.NullUUID{}
}

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TestResolveIssuerByURL_MalformedIssuerNeverReachesTheDatabase passes a nil
// handle deliberately. A test supplying a working one would pass whether or not
// the parse check runs first; only a nil handle proves the ordering, because
// reaching the store at all would panic instead of returning.
func TestResolveIssuerByURL_MalformedIssuerNeverReachesTheDatabase(t *testing.T) {
	t.Parallel()

	_, err := workloadidentity.ResolveIssuerByURL(t.Context(), nil, workloadidentity.ResolveIssuerParams{
		OrganizationID: "org-anything",
		ProjectID:      organizationTier(),
		IssuerURL:      "not-a-url",
	})

	require.ErrorIs(t, err, workloadidentity.ErrIssuerURLInvalid)
	require.NotErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
}

func TestResolveIssuerByURL_MalformedIsDistinctFromMissing(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	tenant := newTenant(t, conn)

	// Every shape RFC 8414 §2 forbids in an issuer identifier, plus the ones
	// canonicalization refuses to guess at.
	for _, raw := range []string{
		"",
		"   ",
		"not-a-url",
		"ftp://issuer.example.com",
		"https://user:pass@issuer.example.com",
		"https://issuer.example.com?x=1",
		"https://issuer.example.com#frag",
		"https:///no-host",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			_, err := workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
				OrganizationID: tenant.organizationID,
				ProjectID:      projectTier(tenant.projectID),
				IssuerURL:      raw,
			})
			require.ErrorIs(t, err, workloadidentity.ErrIssuerURLInvalid)
			require.NotErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
		})
	}

	_, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: tenant.organizationID,
		ProjectID:      projectTier(tenant.projectID),
		IssuerURL:      "https://nobody-registered-this.example.com",
	})
	require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
	require.NotErrorIs(t, err, workloadidentity.ErrIssuerURLInvalid)
}

func TestResolveIssuerByURL_PrefersProjectTierThenOldest(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	tenant := newTenant(t, conn)

	seedIssuer(t, conn, tenant.organizationID, organizationTier(), "org tier", testIssuerURL, epoch)
	wanted := seedIssuer(t, conn, tenant.organizationID, projectTier(tenant.projectID), "project tier, oldest", testIssuerURL, epoch.Add(time.Hour))
	seedIssuer(t, conn, tenant.organizationID, projectTier(tenant.projectID), "project tier, newer", testIssuerURL, epoch.Add(2*time.Hour))

	// The organization row is the oldest of the three, so a resolver that only
	// sorted by age would pick it. Tier wins first, age only breaks ties inside
	// a tier.
	got, err := workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: tenant.organizationID,
		ProjectID:      projectTier(tenant.projectID),
		IssuerURL:      testIssuerURL,
	})
	require.NoError(t, err)
	require.Equal(t, wanted, got.ID)
}

func TestResolveIssuerByURL_OrganizationCallerSeesOnlyTheOrganizationTier(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	tenant := newTenant(t, conn)

	wanted := seedIssuer(t, conn, tenant.organizationID, organizationTier(), "org tier", testIssuerURL, epoch)
	seedIssuer(t, conn, tenant.organizationID, projectTier(tenant.projectID), "project tier", testIssuerURL, epoch)

	got, err := workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: tenant.organizationID,
		ProjectID:      organizationTier(),
		IssuerURL:      testIssuerURL,
	})
	require.NoError(t, err)
	require.Equal(t, wanted, got.ID)
}

// TestResolveIssuerByURL_ASiblingProjectsRowIsNotVisible covers the reason the
// project arm is narrow: one project must not inherit trust another configured,
// even inside the same organization.
func TestResolveIssuerByURL_ASiblingProjectsRowIsNotVisible(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	mine := newTenant(t, conn)
	sibling := newTenant(t, conn)

	seedIssuer(t, conn, mine.organizationID, projectTier(sibling.projectID), "sibling project", testIssuerURL, epoch)

	_, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: mine.organizationID,
		ProjectID:      projectTier(mine.projectID),
		IssuerURL:      testIssuerURL,
	})
	require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
}

// TestResolveIssuerByURL_AnotherOrganizationsRowNeverResolves asserts tenancy
// against the resolver with a real row present, so a predicate that dropped the
// organization arm would fail here rather than pass on an empty table.
func TestResolveIssuerByURL_AnotherOrganizationsRowNeverResolves(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	mine := newTenant(t, conn)
	theirs := newTenant(t, conn)

	seedIssuer(t, conn, theirs.organizationID, organizationTier(), "their org tier", testIssuerURL, epoch)
	seedIssuer(t, conn, theirs.organizationID, projectTier(theirs.projectID), "their project tier", testIssuerURL, epoch)

	_, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: mine.organizationID,
		ProjectID:      projectTier(mine.projectID),
		IssuerURL:      testIssuerURL,
	})
	require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)

	// And the row is genuinely there, so the miss above is tenancy rather than
	// an empty table.
	got, err := workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: theirs.organizationID,
		ProjectID:      projectTier(theirs.projectID),
		IssuerURL:      testIssuerURL,
	})
	require.NoError(t, err)
	require.Equal(t, theirs.organizationID, got.OrganizationID)
}

func TestResolveIssuerByURL_EmptyOrganizationMatchesNothing(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	tenant := newTenant(t, conn)

	seedIssuer(t, conn, tenant.organizationID, organizationTier(), "org tier", testIssuerURL, epoch)

	_, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: "",
		ProjectID:      organizationTier(),
		IssuerURL:      testIssuerURL,
	})
	require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
}

func TestResolveIssuerByURL_SoftDeletedDoesNotResolve(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	tenant := newTenant(t, conn)

	withdrawn := seedIssuer(t, conn, tenant.organizationID, organizationTier(), "withdrawn", testIssuerURL, epoch)
	softDelete(t, conn, withdrawn)

	_, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: tenant.organizationID,
		ProjectID:      organizationTier(),
		IssuerURL:      testIssuerURL,
	})
	require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
}

// TestResolveIssuerByURL_PathBearingIssuerResolves covers GitHub Enterprise
// Cloud, which lets an organization customize its issuer to carry a path.
// Canonicalization allows paths and treats their case as significant, so this
// should already work — asserted rather than inferred, since a preset for that
// platform depends on it.
func TestResolveIssuerByURL_PathBearingIssuerResolves(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	tenant := newTenant(t, conn)

	const stored = "https://token.actions.githubusercontent.com/acme"
	wanted := seedIssuer(t, conn, tenant.organizationID, organizationTier(), "ghec", stored, epoch)

	// Every spelling MatchCandidates expands the stored value into, plus the
	// stored spelling itself.
	for _, presented := range []string{
		stored,
		stored + "/",
		"https://token.actions.githubusercontent.com:443/acme",
		"https://TOKEN.ACTIONS.githubusercontent.com/acme",
	} {
		t.Run(presented, func(t *testing.T) {
			t.Parallel()

			got, err := workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
				OrganizationID: tenant.organizationID,
				ProjectID:      organizationTier(),
				IssuerURL:      presented,
			})
			require.NoError(t, err)
			require.Equal(t, wanted, got.ID)
		})
	}

	// Path case is deliberately significant: some issuers route on it, and
	// treating fewer things as equal is the safe direction to fail.
	_, err = workloadidentity.ResolveIssuerByURL(t.Context(), conn, workloadidentity.ResolveIssuerParams{
		OrganizationID: tenant.organizationID,
		ProjectID:      organizationTier(),
		IssuerURL:      "https://token.actions.githubusercontent.com/ACME",
	})
	require.ErrorIs(t, err, workloadidentity.ErrIssuerNotFound)
}
