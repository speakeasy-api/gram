package killswitches

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

type evaluationQueryFunc func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error)

func (f evaluationQueryFunc) EvaluateCurrentPrescriptions(ctx context.Context, params repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
	return f(ctx, params)
}

func TestEvaluatorRequiresBoundedTimeoutAndCandidates(t *testing.T) {
	t.Parallel()
	registry := evaluationRegistry(t)
	query := evaluationQueryFunc(func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
		return repo.EvaluateCurrentPrescriptionsRow{}, pgx.ErrNoRows
	})

	_, err := newEvaluator(query, registry, 0, nil)
	require.EqualError(t, err, "kill-switch evaluator timeout must be positive")

	evaluator, err := newEvaluator(query, registry, time.Second, nil)
	require.NoError(t, err)
	request := evaluationRequest("block-tools")
	request.DefinitionKeys = make([]DefinitionKey, MaxEvaluationDefinitionCandidates+1)
	result := evaluator.Evaluate(t.Context(), request)
	require.Equal(t, EvaluationResultInfrastructureFailure, result.Kind())
	kind, ok := result.InfrastructureFailureKind()
	require.True(t, ok)
	require.Equal(t, InfrastructureFailureInvalidRequest, kind)
	policy, ok := result.FailurePolicy()
	require.True(t, ok)
	require.Equal(t, FailurePolicyFailClosed, policy)
}

func TestEvaluatorUsesOneQueryAndRetainsWinningPolicy(t *testing.T) {
	t.Parallel()
	registry := evaluationRegistry(t)
	var calls atomic.Int32
	prescriptionID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	query := evaluationQueryFunc(func(_ context.Context, params repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
		calls.Add(1)
		require.Equal(t, []string{"closed-tools", "block-tools"}, params.DefinitionKeys)
		require.Equal(t, []string{"user"}, params.PrincipalKinds)
		require.Equal(t, []string{"user:alpha"}, params.PrincipalKeys)
		return repo.EvaluateCurrentPrescriptionsRow{PrescriptionID: prescriptionID, DefinitionKey: "closed-tools", ExternalNote: "Exact public note."}, nil
	})
	evaluator, err := newEvaluator(query, registry, time.Second, nil)
	require.NoError(t, err)

	result := evaluator.Evaluate(t.Context(), evaluationRequest("closed-tools", "block-tools"))
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, EvaluationResultMatch, result.Kind())
	note, ok := result.ExternalNote()
	require.True(t, ok)
	require.Equal(t, "Exact public note.", note)
	policy, ok := result.FailurePolicy()
	require.True(t, ok)
	require.Equal(t, FailurePolicyFailClosed, policy)
}

func TestResolveTransportDispositionUsesAuthoritativeEmbeddedPolicy(t *testing.T) {
	t.Parallel()

	classified, err := NewInfrastructureFailureResultWithPolicy(errors.New("evaluation unavailable"), FailurePolicyFailClosed, InfrastructureFailureDatabase)
	require.NoError(t, err)
	disposition, err := ResolveTransportDisposition(classified, FailurePolicyFailOpen)
	require.NoError(t, err)
	require.Equal(t, TransportDispositionInfrastructureRejection, disposition.Kind())

	legacy, err := NewInfrastructureFailureResult(errors.New("evaluation unavailable"))
	require.NoError(t, err)
	disposition, err = ResolveTransportDisposition(legacy, FailurePolicyFailOpen)
	require.NoError(t, err)
	require.Equal(t, TransportDispositionContinue, disposition.Kind())
}

func TestEvaluatorDistinguishesFailuresAndAppliesCandidatePolicies(t *testing.T) {
	t.Parallel()
	registry := evaluationRegistry(t)
	tests := []struct {
		name        string
		definitions []DefinitionKey
		query       evaluationQueryFunc
		wantKind    InfrastructureFailureKind
		wantPolicy  FailurePolicy
		wantError   error
	}{
		{
			name: "fail open database failure", definitions: []DefinitionKey{"block-tools"},
			query: func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
				return repo.EvaluateCurrentPrescriptionsRow{}, errors.New("database unavailable")
			},
			wantKind: InfrastructureFailureDatabase, wantPolicy: FailurePolicyFailOpen,
		},
		{
			name: "any fail closed definition dominates database failure", definitions: []DefinitionKey{"block-tools", "closed-tools"},
			query: func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
				return repo.EvaluateCurrentPrescriptionsRow{}, errors.New("database unavailable")
			},
			wantKind: InfrastructureFailureDatabase, wantPolicy: FailurePolicyFailClosed,
		},
		{
			name: "evaluator timeout", definitions: []DefinitionKey{"block-tools"},
			query: func(ctx context.Context, _ repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
				<-ctx.Done()
				return repo.EvaluateCurrentPrescriptionsRow{}, ctx.Err()
			},
			wantKind: InfrastructureFailureTimeout, wantPolicy: FailurePolicyFailOpen, wantError: ErrEvaluatorTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			evaluator, err := newEvaluator(test.query, registry, time.Millisecond, nil)
			require.NoError(t, err)
			request := evaluationRequest(test.definitions...)
			result := evaluator.Evaluate(t.Context(), request)
			require.Equal(t, EvaluationResultInfrastructureFailure, result.Kind())
			kind, ok := result.InfrastructureFailureKind()
			require.True(t, ok)
			require.Equal(t, test.wantKind, kind)
			policy, ok := result.FailurePolicy()
			require.True(t, ok)
			require.Equal(t, test.wantPolicy, policy)
			if test.wantError != nil {
				require.ErrorIs(t, result.InfrastructureError(), test.wantError)
			}
			disposition, err := ResolveTransportDisposition(result, policy)
			require.NoError(t, err)
			if policy == FailurePolicyFailOpen {
				require.Equal(t, TransportDispositionContinue, disposition.Kind())
			} else {
				require.Equal(t, TransportDispositionInfrastructureRejection, disposition.Kind())
			}
		})
	}
}

func TestEvaluatorDistinguishesInFlightParentDeadlineFromEvaluatorTimeout(t *testing.T) {
	t.Parallel()

	registry := evaluationRegistry(t)
	var queryStarted atomic.Bool
	query := evaluationQueryFunc(func(ctx context.Context, _ repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
		queryStarted.Store(true)
		<-ctx.Done()
		return repo.EvaluateCurrentPrescriptionsRow{}, ctx.Err()
	})
	evaluator, err := newEvaluator(query, registry, time.Second, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	result := evaluator.Evaluate(ctx, evaluationRequest("block-tools"))
	require.True(t, queryStarted.Load())
	kind, ok := result.InfrastructureFailureKind()
	require.True(t, ok)
	require.Equal(t, InfrastructureFailureParentCancellation, kind)
	require.ErrorIs(t, result.InfrastructureError(), context.DeadlineExceeded)
	require.NotErrorIs(t, result.InfrastructureError(), ErrEvaluatorTimeout)
}

func TestEvaluatorPreservesParentCancellationAndSkipsUnsupportedCandidates(t *testing.T) {
	t.Parallel()
	registry := evaluationRegistry(t)
	var calls atomic.Int32
	query := evaluationQueryFunc(func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
		calls.Add(1)
		return repo.EvaluateCurrentPrescriptionsRow{}, pgx.ErrNoRows
	})
	evaluator, err := newEvaluator(query, registry, time.Second, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := evaluator.Evaluate(ctx, evaluationRequest("block-tools"))
	require.ErrorIs(t, result.InfrastructureError(), context.Canceled)
	kind, ok := result.InfrastructureFailureKind()
	require.True(t, ok)
	require.Equal(t, InfrastructureFailureParentCancellation, kind)
	require.Zero(t, calls.Load())

	request := evaluationRequest("block-tools")
	request.PrincipalCandidates = nil
	result = evaluator.Evaluate(t.Context(), request)
	require.Equal(t, EvaluationResultNoMatch, result.Kind())
	reason, ok := result.NoMatchReason()
	require.True(t, ok)
	require.Equal(t, NoMatchReasonUnsupportedIdentity, reason)

	request = evaluationRequest("block-tools")
	request.PrincipalCandidates = []PrincipalCandidate{{Kind: "unknown", Key: "unknown:alpha"}}
	result = evaluator.Evaluate(t.Context(), request)
	require.Equal(t, EvaluationResultInfrastructureFailure, result.Kind())

	request = evaluationRequest("block-tools")
	request.ResourceKind = ""
	request.ResourceKey = ""
	result = evaluator.Evaluate(t.Context(), request)
	require.Equal(t, EvaluationResultNoMatch, result.Kind())
	reason, ok = result.NoMatchReason()
	require.True(t, ok)
	require.Equal(t, NoMatchReasonUnsupportedResource, reason)
	require.Zero(t, calls.Load())
}

func TestKillswitchEvaluationMetricsHaveClosedAttributes(t *testing.T) {
	t.Parallel()
	registry := evaluationRegistry(t)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := newEvaluationMetrics(provider, nil)
	prescriptionID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	queries := []evaluationQueryFunc{
		func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
			return repo.EvaluateCurrentPrescriptionsRow{PrescriptionID: prescriptionID, DefinitionKey: "block-tools", ExternalNote: "Public note."}, nil
		},
		func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
			return repo.EvaluateCurrentPrescriptionsRow{}, pgx.ErrNoRows
		},
		func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
			return repo.EvaluateCurrentPrescriptionsRow{}, errors.New("database unavailable")
		},
	}
	for _, query := range queries {
		evaluator, err := newEvaluator(query, registry, time.Second, metrics)
		require.NoError(t, err)
		evaluator.Evaluate(t.Context(), evaluationRequest("block-tools"))
	}

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	var outcomes []string
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, collected := range scope.Metrics {
			if collected.Name != meterKillswitchEvaluationDuration {
				continue
			}
			histogram, ok := collected.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, point := range histogram.DataPoints {
				require.Equal(t, 1, point.Attributes.Len())
				value, ok := point.Attributes.Value(attr.OutcomeKey)
				require.True(t, ok)
				outcomes = append(outcomes, value.AsString())
			}
		}
	}
	require.ElementsMatch(t, []string{killswitchEvaluationOutcomeMatched, killswitchEvaluationOutcomeUnmatched, killswitchEvaluationOutcomeEvaluatorFailure}, outcomes)
}

func evaluationRequest(definitions ...DefinitionKey) EvaluationRequest {
	return EvaluationRequest{
		OrganizationID: "org:test", DefinitionKeys: definitions,
		PrincipalCandidates: []PrincipalCandidate{{Kind: "user", Key: "user:alpha"}},
		ResourceKind:        "tool", ResourceKey: "org:test:tool:alpha",
	}
}

func evaluationRegistry(t *testing.T) *Registry {
	t.Helper()
	registration := validRegistration()
	closed := registration.Definitions[0]
	closed.Key = "closed-tools"
	closed.FailurePolicy = FailurePolicyFailClosed
	registration.Definitions = append(registration.Definitions, closed)
	for _, coverage := range append([]CoverageContract(nil), registration.Coverage...) {
		coverage.Definition = closed.Key
		coverage.FailurePolicy = closed.FailurePolicy
		registration.Coverage = append(registration.Coverage, coverage)
	}
	registry, err := BuildRegistry(registration)
	require.NoError(t, err)
	return registry
}
