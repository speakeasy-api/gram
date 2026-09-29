package platformmcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestParseRiskFindingIDsValidatesAndDeduplicates(t *testing.T) {
	t.Parallel()

	first, second := uuid.New(), uuid.New()
	ids, err := parseRiskFindingIDs([]string{first.String(), " " + second.String() + " ", first.String()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, ids, "duplicates collapse and first-seen order is kept")

	for name, input := range map[string][]string{
		"empty":     {},
		"malformed": {first.String(), "not-a-uuid"},
		"nil uuid":  {uuid.Nil.String()},
		"over cap":  repeatedFindingIDs(maxRiskFindingFalsePositiveBatch + 1),
	} {
		_, err := parseRiskFindingIDs(input)
		requireRiskMutationErrorCode(t, err, "invalid_request", name)
	}
	_, err = parseRiskFindingIDs(repeatedFindingIDs(maxRiskFindingFalsePositiveBatch))
	require.NoError(t, err, "the cap itself is accepted")
}

func TestPartitionRiskFindingOutcomesAssignsEveryIDOnce(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Example", Slug: "example"}
	changed, already, missing := uuid.New(), uuid.New(), uuid.New()
	existing := []riskrepo.RiskResult{{ID: changed}, {ID: already}}
	requested := []uuid.UUID{missing, already, changed}

	dismissed := partitionRiskFindingOutcomes(project, requested, existing, []riskrepo.RiskResult{{ID: changed}}, false)
	require.Equal(t, riskReceiptProject(project), dismissed.Project)
	require.Equal(t, []string{changed.String()}, dismissed.ChangedFindingIDs)
	require.Equal(t, []string{already.String()}, dismissed.AlreadyInRequestedStateFindingIDs)
	require.Equal(t, []string{missing.String()}, dismissed.NotFoundFindingIDs)
	require.Equal(t, riskFindingResultCategoryDismissed, dismissed.ResultCategory)

	restored := partitionRiskFindingOutcomes(project, requested, existing, []riskrepo.RiskResult{{ID: changed}}, true)
	require.Equal(t, riskFindingResultCategoryRestored, restored.ResultCategory)

	unchanged := partitionRiskFindingOutcomes(project, requested, existing, nil, false)
	require.Empty(t, unchanged.ChangedFindingIDs)
	require.NotNil(t, unchanged.ChangedFindingIDs, "empty buckets encode as [] rather than null")
	require.ElementsMatch(t, []string{already.String(), changed.String()}, unchanged.AlreadyInRequestedStateFindingIDs)
	require.Equal(t, riskFindingResultCategoryNoChange, unchanged.ResultCategory)

	encoded, err := json.Marshal(unchanged)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"changed_finding_ids":[]`)
}

func TestRiskFindingFalsePositiveReceiptResultsStayClosed(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Example", Slug: "example"}
	valid := RiskFindingFalsePositiveReceipt{
		Project: riskReceiptProject(project), ChangedFindingIDs: []string{uuid.NewString()},
		AlreadyInRequestedStateFindingIDs: []string{}, NotFoundFindingIDs: []string{uuid.NewString()},
		ResultCategory: riskFindingResultCategoryDismissed,
	}
	payload, err := encodeRiskMutationResult(operationMarkRiskFindingsFalsePositive, MarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: valid})
	require.NoError(t, err)
	var decoded RiskFindingFalsePositiveReceipt
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Equal(t, valid, decoded)

	_, err = encodeRiskMutationResult(operationUnmarkRiskFindingsFalsePositive, MarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: valid})
	require.Error(t, err, "a mark projection cannot be stored under the unmark operation")

	restored := valid
	restored.ResultCategory = riskFindingResultCategoryRestored
	_, err = encodeRiskMutationResult(operationUnmarkRiskFindingsFalsePositive, UnmarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: restored})
	require.NoError(t, err)
	_, err = encodeRiskMutationResult(operationMarkRiskFindingsFalsePositive, MarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: restored})
	require.Error(t, err, "changed ids on a mark must be categorised as dismissed")

	for name, mutate := range map[string]func(*RiskFindingFalsePositiveReceipt){
		"nil bucket":        func(r *RiskFindingFalsePositiveReceipt) { r.AlreadyInRequestedStateFindingIDs = nil },
		"id in two buckets": func(r *RiskFindingFalsePositiveReceipt) { r.NotFoundFindingIDs = []string{r.ChangedFindingIDs[0]} },
		"id twice in bucket": func(r *RiskFindingFalsePositiveReceipt) {
			r.ChangedFindingIDs = append(r.ChangedFindingIDs, r.ChangedFindingIDs[0])
		},
		"malformed id": func(r *RiskFindingFalsePositiveReceipt) { r.NotFoundFindingIDs = []string{"not-a-uuid"} },
		"no ids": func(r *RiskFindingFalsePositiveReceipt) {
			r.ChangedFindingIDs, r.NotFoundFindingIDs = []string{}, []string{}
		},
		"wrong no-change": func(r *RiskFindingFalsePositiveReceipt) { r.ChangedFindingIDs = []string{} },
		"over cap": func(r *RiskFindingFalsePositiveReceipt) {
			r.NotFoundFindingIDs = repeatedFindingIDs(maxRiskFindingFalsePositiveBatch)
		},
		"malformed project":  func(r *RiskFindingFalsePositiveReceipt) { r.Project.Slug = "Not A Slug" },
		"unknown category":   func(r *RiskFindingFalsePositiveReceipt) { r.ResultCategory = "maybe" },
		"empty result table": func(r *RiskFindingFalsePositiveReceipt) { r.Project = RiskMutationReceiptProject{} },
	} {
		receipt := valid
		mutate(&receipt)
		_, err := encodeRiskMutationResult(operationMarkRiskFindingsFalsePositive, MarkRiskFindingsFalsePositiveReceiptResult{RiskFindingFalsePositiveReceipt: receipt})
		require.Error(t, err, name)
	}
}

// The confirmation gate runs before the principal is read: an unconfirmed
// call from a context with no principal is refused as confirmation_required,
// not as unauthorized, and the disabled service is never consulted.
func TestRiskFindingFalsePositiveToolsRefuseBeforeReadingPrincipal(t *testing.T) {
	t.Parallel()

	service := newRiskFindingFalsePositiveService(nil, nil)
	unconfirmed := map[string]any{"project_slug": "example", "finding_ids": []any{uuid.NewString()}, "confirmed": false, "idempotency_key": "key"}

	_, _, err := service.markTool(t.Context(), nil, unconfirmed)
	requireRiskMutationRefusal(t, err, "confirmation_required")
	_, _, err = service.unmarkTool(t.Context(), nil, unconfirmed)
	requireRiskMutationRefusal(t, err, "confirmation_required")

	confirmed := cloneRiskMutationInput(unconfirmed)
	confirmed["confirmed"] = true
	_, _, err = service.markTool(t.Context(), nil, confirmed)
	require.ErrorIs(t, err, ErrUnauthorized, "a confirmed call still needs an attributable principal")

	withReason := cloneRiskMutationInput(confirmed)
	withReason["reason"] = "not for restores"
	_, _, err = service.unmarkTool(t.Context(), nil, withReason)
	requireRiskMutationRefusal(t, err, "invalid_request")

	_, _, err = service.markTool(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), nil, confirmed)
	requireRiskMutationRefusal(t, err, unavailableCode)

	_, _, err = service.markTool(t.Context(), nil, map[string]any{"project_slug": "example", "finding_ids": []any{uuid.NewString()}, "confirmed": true, "idempotency_key": "key", "unexpected": true})
	requireRiskMutationRefusal(t, err, "invalid_request")
}

func TestRiskFindingFalsePositiveToolSchemasBoundInput(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-findings-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	registerUnavailableRiskTools(reg)

	for _, name := range []string{operationMarkRiskFindingsFalsePositive, operationUnmarkRiskFindingsFalsePositive} {
		descriptor := descriptorByName(t, reg, name)
		require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.NotNil(t, descriptor.Annotations)
		require.False(t, descriptor.Annotations.ReadOnlyHint)
		require.True(t, descriptor.Annotations.IdempotentHint)
		require.NotNil(t, descriptor.Annotations.DestructiveHint)
		require.Equal(t, name == operationMarkRiskFindingsFalsePositive, *descriptor.Annotations.DestructiveHint, "dismissing hides findings; restoring does not")
		require.Contains(t, descriptor.Description, "not enabled")

		ctx := ContextWithPrincipal(t.Context(), testRiskPrincipal("user"))
		ids := `["` + uuid.NewString() + `"]`
		for name, arguments := range map[string]string{
			"empty ids":     `{"project_slug":"project","finding_ids":[],"confirmed":true,"idempotency_key":"key"}`,
			"over cap":      `{"project_slug":"project","finding_ids":` + jsonFindingIDs(maxRiskFindingFalsePositiveBatch+1) + `,"confirmed":true,"idempotency_key":"key"}`,
			"missing gate":  `{"project_slug":"project","finding_ids":` + ids + `,"idempotency_key":"key"}`,
			"unknown field": `{"project_slug":"project","finding_ids":` + ids + `,"confirmed":true,"idempotency_key":"key","extra":true}`,
			"no project":    `{"finding_ids":` + ids + `,"confirmed":true,"idempotency_key":"key"}`,
		} {
			_, err := descriptor.Invoke(ctx, json.RawMessage(arguments))
			require.ErrorContains(t, err, "arguments do not match the tool schema", name)
		}

		_, err := descriptor.Invoke(ctx, json.RawMessage(`{"project_slug":"project","finding_ids":`+ids+`,"confirmed":true,"idempotency_key":"key"}`))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
	}

	_, err := descriptorByName(t, reg, operationUnmarkRiskFindingsFalsePositive).Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","finding_ids":["`+uuid.NewString()+`"],"confirmed":true,"idempotency_key":"key","reason":"why"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema", "restores take no reason")
}

func TestRiskFindingFalsePositiveTelemetryClassification(t *testing.T) {
	t.Parallel()

	fresh := riskMutationSuccessEvent(operationMarkRiskFindingsFalsePositive, MarkRiskFindingsFalsePositiveToolOutput{Receipt: RiskMutationToolReceipt{Replayed: false}})
	require.Equal(t, riskTelemetryFresh, fresh.Replay)
	replayed := riskMutationSuccessEvent(operationUnmarkRiskFindingsFalsePositive, UnmarkRiskFindingsFalsePositiveToolOutput{Receipt: RiskMutationToolReceipt{Replayed: true}})
	require.Equal(t, riskTelemetryReceiptReplay, replayed.Replay)
	require.True(t, validRiskTelemetryTool(operationMarkRiskFindingsFalsePositive))
	require.True(t, validRiskTelemetryTool(operationUnmarkRiskFindingsFalsePositive))

	_, _, err := riskMutationToolRefusal[MarkRiskFindingsFalsePositiveToolOutput](&RiskMutationError{Code: "confirmation_required", Message: "confirm", Cause: ErrRiskMutationInvalid})
	require.Equal(t, "confirmation_required", riskMutationTelemetryOutcome(err))
}

func requireRiskMutationErrorCode(t *testing.T, err error, code string, msgAndArgs ...any) {
	t.Helper()
	var mutation *RiskMutationError
	require.ErrorAs(t, err, &mutation, msgAndArgs...)
	require.Equal(t, code, mutation.Code, msgAndArgs...)
}

func repeatedFindingIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	return ids
}

func jsonFindingIDs(count int) string {
	quoted := make([]string, count)
	for i := range quoted {
		quoted[i] = fmt.Sprintf("%q", uuid.NewString())
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
