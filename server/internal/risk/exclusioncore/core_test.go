package exclusioncore

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestValidateMatchValue(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateMatchValue("exact", "value"))
	require.NoError(t, ValidateMatchValue("regex", `^value$`))

	var validationErr *ValidationError
	err := ValidateMatchValue("exact", "")
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "match_value must not be empty", validationErr.Message)
	require.NoError(t, validationErr.Cause)

	err = ValidateMatchValue("regex", strings.Repeat("x", RegexMaxLength+1))
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "regex pattern too long (max 512 characters)", validationErr.Message)

	err = ValidateMatchValue("regex", "[")
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "invalid regex pattern", validationErr.Message)
	require.Error(t, validationErr.Cause)
}

func TestAuditSnapshotRedactsSensitiveFields(t *testing.T) {
	t.Parallel()

	exclusion := Exclusion{
		ID:             uuid.New(),
		ProjectID:      uuid.New(),
		OrganizationID: "<ORG_ID>",
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "exact",
		MatchValue:     "sensitive-value",
		RuleIDFilter:   "sensitive-rule",
		SourceFilter:   "sensitive-source",
		Enabled:        true,
		CreatedAt:      time.Time{},
		UpdatedAt:      time.Time{},
	}

	snapshot := AuditSnapshot(exclusion)
	require.Equal(t, RedactValue("sensitive-value"), snapshot.MatchValue)
	require.Equal(t, RedactValue("sensitive-rule"), snapshot.RuleIDFilter)
	require.Equal(t, RedactValue("sensitive-source"), snapshot.SourceFilter)
	require.NotContains(t, DisplayName(exclusion), exclusion.MatchValue)

	// Redaction returns a copy; the caller's response projection stays intact.
	require.Equal(t, "sensitive-value", exclusion.MatchValue)
	require.Equal(t, "sensitive-rule", exclusion.RuleIDFilter)
	require.Equal(t, "sensitive-source", exclusion.SourceFilter)
}

func TestProjectPreservesReadShape(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.August, 25, 1, 2, 3, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	policyID := uuid.New()
	row := repo.RiskExclusion{
		ID:             uuid.New(),
		ProjectID:      uuid.New(),
		OrganizationID: "<ORG_ID>",
		RiskPolicyID:   uuid.NullUUID{UUID: policyID, Valid: true},
		MatchType:      "rule_id",
		MatchValue:     "rule",
		RuleIDFilter:   pgtype.Text{String: "filter", Valid: true},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:      pgtype.Timestamptz{Time: updatedAt, Valid: true},
		DeletedAt:      pgtype.Timestamptz{},
		Deleted:        false,
	}

	got := Project(row)
	require.Equal(t, row.ID, got.ID)
	require.Equal(t, policyID, got.RiskPolicyID.UUID)
	require.True(t, got.RiskPolicyID.Valid)
	require.Equal(t, "filter", got.RuleIDFilter)
	require.Empty(t, got.SourceFilter)
	require.Equal(t, createdAt, got.CreatedAt)
	require.Equal(t, updatedAt, got.UpdatedAt)
}
