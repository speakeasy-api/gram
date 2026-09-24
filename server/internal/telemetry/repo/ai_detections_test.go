package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
)

func TestUpsertAIDetectionsRejectsZeroSeenAt(t *testing.T) {
	t.Parallel()
	arg := UpsertAIDetectionParams{
		OrganizationID: "org_test",
		TargetID:       "cursor",
		DeviceSerial:   "serial-1",
		UserEmail:      "member@example.com",
		Signal:         "installed",
		Category:       "harness",
		Version:        "",
		SeenAt:         time.Time{},
		UpdatedAt:      time.Now().UTC(),
	}

	_, err := New(nil).UpsertAIDetections(t.Context(), []UpsertAIDetectionParams{arg})
	require.ErrorContains(t, err, "seen at is required")
}

func TestUpsertAIDetectionsRejectsInvalidFiniteValues(t *testing.T) {
	t.Parallel()
	valid := UpsertAIDetectionParams{
		OrganizationID: "org_test",
		TargetID:       "cursor",
		DeviceSerial:   "serial-1",
		UserEmail:      "member@example.com",
		Signal:         "installed",
		Category:       "harness",
		Version:        "",
		SeenAt:         time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	invalidSignal := valid
	invalidSignal.Signal = "stopped"
	_, signalErr := New(nil).UpsertAIDetections(t.Context(), []UpsertAIDetectionParams{invalidSignal})
	require.ErrorContains(t, signalErr, "invalid signal")

	invalidCategory := valid
	invalidCategory.Category = "other"
	_, categoryErr := New(nil).UpsertAIDetections(t.Context(), []UpsertAIDetectionParams{invalidCategory})
	require.ErrorContains(t, categoryErr, "invalid category")
}

// TestUpsertAIDetectionsAcceptsEveryKnownCategory holds this layer's category
// literals to aitargets.KnownCategories. The check is on the vocabulary rather
// than on a call because a valid row runs on past validation into ClickHouse,
// which New(nil) has no connection for. This layer cannot import aitargets in
// production without inverting the dependency, so the test carries the link.
func TestUpsertAIDetectionsAcceptsEveryKnownCategory(t *testing.T) {
	t.Parallel()
	known := make([]string, 0, len(aitargets.KnownCategories()))
	for _, category := range aitargets.KnownCategories() {
		known = append(known, string(category))
	}
	require.ElementsMatch(t, known, aiDetectionCategories,
		"the ClickHouse write path and the scan-report ingest must accept the same categories")
}

func TestBuildListAIDetectionUsersQueryRequiresTargetID(t *testing.T) {
	t.Parallel()

	_, _, err := buildListAIDetectionUsersQuery(ListAIDetectionUsersParams{
		OrganizationID:       "org_0123456789",
		TargetID:             "",
		CanonicalIdentityOrg: "",
	})
	require.ErrorContains(t, err, "target id is required")
}

// The per-user expansion buckets by email, which is exactly the read the
// canonical fold exists for: without it one employee's work, personal and
// case-variant emails would come back as separate people.
func TestBuildListAIDetectionUsersQueryFoldsEmailWhenRolledOut(t *testing.T) {
	t.Parallel()

	folded, _, err := buildListAIDetectionUsersQuery(ListAIDetectionUsersParams{
		OrganizationID:       "org_0123456789",
		TargetID:             "cursor",
		CanonicalIdentityOrg: "org_0123456789",
	})
	require.NoError(t, err)
	require.Contains(t, folded, "joinGet('identity_map', 'canonical_email', 'org_0123456789', lowerUTF8(user_email))")
	require.Contains(t, folded, "GROUP BY person_email")
	require.Contains(t, folded, "SETTINGS use_query_condition_cache = 0")

	plain, _, err := buildListAIDetectionUsersQuery(ListAIDetectionUsersParams{
		OrganizationID:       "org_0123456789",
		TargetID:             "cursor",
		CanonicalIdentityOrg: "",
	})
	require.NoError(t, err)
	require.NotContains(t, plain, "joinGet")
	require.Contains(t, plain, "user_email AS person_email")
}
