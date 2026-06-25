package risk_export

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_export/repo"
)

func TestMapFindingCentricRow_WithFinding(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	msgID := uuid.New()
	findingID := uuid.New()
	policyID := uuid.New()

	row := repo.ExportFindingCentricRow{
		ChatID:             chatID,
		MessageID:          msgID,
		Seq:                7,
		Generation:         2,
		Rn:                 3,
		Total:              10,
		Role:               "user",
		Content:            "my password is hunter2",
		ContentRaw:         []byte(`{"parts":["x"]}`),
		ContentAssetUrl:    pgtype.Text{},
		Model:              pgtype.Text{String: "gpt-4", Valid: true},
		ToolCalls:          nil,
		ToolUrn:            pgtype.Text{},
		Source:             pgtype.Text{String: "Playground", Valid: true},
		ExternalUserID:     pgtype.Text{},
		CreatedAt:          pgtype.Timestamptz{},
		IsSeed:             true,
		FindingID:          uuid.NullUUID{UUID: findingID, Valid: true},
		RiskPolicyID:       uuid.NullUUID{UUID: policyID, Valid: true},
		RiskPolicyVersion:  pgtype.Int8{Int64: 4, Valid: true},
		FindingRuleID:      pgtype.Text{String: "secret.generic", Valid: true},
		FindingSource:      pgtype.Text{String: "gitleaks", Valid: true},
		FindingDescription: pgtype.Text{},
		FindingMatch:       pgtype.Text{String: "hunter2", Valid: true},
		StartPos:           pgtype.Int4{Int32: 15, Valid: true},
		EndPos:             pgtype.Int4{Int32: 22, Valid: true},
		Confidence:         pgtype.Float8{Float64: 0.9, Valid: true},
		Tags:               []string{"secret"},
		Spans:              []byte(`[{"match":"hunter2"}]`),
		PolicyName:         pgtype.Text{String: "Secrets", Valid: true},
		PolicyType:         pgtype.Text{String: "standard", Valid: true},
		PolicyAction:       pgtype.Text{String: "flag", Valid: true},
		RuleTitle:          pgtype.Text{},
		RuleSeverity:       pgtype.Text{String: "high", Valid: true},
	}

	rec := mapFindingCentricRow(row)
	require.NotNil(t, rec.Finding)
	require.Equal(t, findingID.String(), rec.Finding.FindingID)
	require.NotNil(t, rec.IsSeed)
	require.True(t, *rec.IsSeed)
	require.NotNil(t, rec.Rn)
	require.EqualValues(t, 3, *rec.Rn)

	data, err := json.Marshal(rec)
	require.NoError(t, err)
	out := string(data)

	// jsonb columns pass through as raw JSON, not base64-encoded strings.
	require.Contains(t, out, `"content_raw":{"parts":["x"]}`)
	require.Contains(t, out, `"spans":[{"match":"hunter2"}]`)
	require.Contains(t, out, `"finding":{`)
	require.Contains(t, out, `"rule_id":"secret.generic"`)
	// Null/absent optional fields are omitted.
	require.NotContains(t, out, `"content_asset_url"`)
	require.NotContains(t, out, `"tool_calls"`)
	require.NotContains(t, out, `"description"`)
}

func TestMapFindingCentricRow_ContextMessageHasNoFinding(t *testing.T) {
	t.Parallel()

	row := repo.ExportFindingCentricRow{
		ChatID:     uuid.New(),
		MessageID:  uuid.New(),
		Seq:        1,
		Generation: 0,
		Rn:         1,
		Total:      5,
		Role:       "assistant",
		Content:    "sure, here you go",
		IsSeed:     false,
		FindingID:  uuid.NullUUID{},
	}

	rec := mapFindingCentricRow(row)
	require.Nil(t, rec.Finding)

	data, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NotContains(t, string(data), `"finding"`)
}

func TestNonNilStrings(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{}, nonNilStrings(nil))
	require.Equal(t, []string{"a"}, nonNilStrings([]string{"a"}))
}
