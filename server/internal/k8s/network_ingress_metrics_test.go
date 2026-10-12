package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type telemetryNetworkIngressProvisioner struct {
	observation NetworkIngressObservation
	err         error
}

func (p telemetryNetworkIngressProvisioner) Apply(context.Context, NetworkIngressDesired) (NetworkIngressObservation, error) {
	return p.observation, p.err
}

func (p telemetryNetworkIngressProvisioner) Observe(context.Context, NetworkIngressResourceNames) (NetworkIngressObservation, error) {
	return p.observation, p.err
}

func (p telemetryNetworkIngressProvisioner) Delete(context.Context, NetworkIngressResourceNames) error {
	return p.err
}

func TestNetworkIngressDeletionTelemetry(t *testing.T) {
	t.Parallel()

	const sensitive = "private-provider-request-sentinel"
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		result string
	}{
		{"completed", nil, networkIngressErrorCodeNone, networkIngressResultSuccess},
		{"pending", ErrNetworkIngressDeletionPending, networkIngressErrorCodeDeletionPending, networkIngressResultPending},
		{"wrapped pending", fmt.Errorf("%s: %w", sensitive, ErrNetworkIngressDeletionPending), networkIngressErrorCodeDeletionPending, networkIngressResultPending},
		{"rejected credentials", fmt.Errorf("%s: %w", sensitive, ErrNetworkIngressProviderCredentialsRejected), NetworkIngressErrorProviderCredentialsRejected, networkIngressResultError},
		{"invalid desired state", fmt.Errorf("%s: %w", sensitive, ErrNetworkIngressInvalidDesiredState), NetworkIngressErrorInvalidDesiredState, networkIngressResultError},
		{"unsupported provider", fmt.Errorf("%s: %w", sensitive, ErrNetworkIngressUnsupportedProvider), NetworkIngressErrorUnsupportedProvider, networkIngressResultError},
		{"kubernetes API", fmt.Errorf("%s: %w", sensitive, k8serrors.NewForbidden(schema.GroupResource{Resource: "ingresses"}, "private-resource", errors.New(sensitive))), NetworkIngressErrorKubernetes, networkIngressResultError},
		{"unknown error", errors.New(sensitive), networkIngressErrorCodeInternal, networkIngressResultError},
		{"error text is not a classification", errors.New("deletion_pending: " + sensitive), networkIngressErrorCodeInternal, networkIngressResultError},
		{"pending with rejected credentials", errors.Join(ErrNetworkIngressDeletionPending, ErrNetworkIngressProviderCredentialsRejected), NetworkIngressErrorProviderCredentialsRejected, networkIngressResultError},
		{"pending with API failure", errors.Join(ErrNetworkIngressDeletionPending, k8serrors.NewTimeoutError(sensitive, 1)), NetworkIngressErrorKubernetes, networkIngressResultError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			provisioner := ObserveNetworkIngressProvisioner(NetworkIngressProviderTailscale,
				telemetryNetworkIngressProvisioner{err: tc.err}, logger, NewNetworkIngressMetrics(logger, provider))

			err := provisioner.Delete(t.Context(), NetworkIngressResourceNames{})
			require.ErrorIs(t, err, tc.err)

			var log map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &log))
			require.Equal(t, "network_ingress_provisioner", log[string(attr.ComponentKey)])
			require.Equal(t, NetworkIngressProviderTailscale, log[string(attr.ProviderKey)])
			require.Equal(t, networkIngressOperationDelete, log[string(attr.NetworkIngressOperationKey)])
			require.Equal(t, tc.result, log[string(attr.OutcomeKey)])
			require.Equal(t, tc.code, log[string(attr.NetworkIngressErrorCodeKey)])
			require.NotContains(t, logs.String(), sensitive)
			require.NotContains(t, logs.String(), "private-resource")
			switch tc.result {
			case networkIngressResultPending:
				require.Equal(t, "DEBUG", log["level"])
				require.Equal(t, "network ingress provisioner deletion pending", log["msg"])
				require.NotContains(t, log, "error.message")
			case networkIngressResultSuccess:
				require.Equal(t, "INFO", log["level"])
				require.Equal(t, "network ingress provisioner operation completed", log["msg"])
				require.NotContains(t, log, "error.message")
			default:
				require.Equal(t, "ERROR", log["level"])
				require.Equal(t, "network ingress provisioner operation failed", log["msg"])
				require.Equal(t, "network ingress provider: "+tc.code, log["error.message"])
			}

			var metrics metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &metrics))
			require.Len(t, metrics.ScopeMetrics, 1)
			require.Len(t, metrics.ScopeMetrics[0].Metrics, 2)
			wantAttributes := attribute.NewSet(
				attr.Provider(NetworkIngressProviderTailscale),
				attr.NetworkIngressOperation(networkIngressOperationDelete),
				attr.NetworkIngressResult(tc.result),
				attr.NetworkIngressErrorCode(tc.code),
			)
			for _, metric := range metrics.ScopeMetrics[0].Metrics {
				switch metric.Name {
				case networkIngressOperationsMetric:
					sum, ok := metric.Data.(metricdata.Sum[int64])
					require.True(t, ok)
					require.Len(t, sum.DataPoints, 1)
					require.Equal(t, int64(1), sum.DataPoints[0].Value)
					require.Equal(t, wantAttributes, sum.DataPoints[0].Attributes)
				case networkIngressDurationMetric:
					histogram, ok := metric.Data.(metricdata.Histogram[float64])
					require.True(t, ok)
					require.Len(t, histogram.DataPoints, 1)
					require.Equal(t, uint64(1), histogram.DataPoints[0].Count)
					require.Equal(t, wantAttributes, histogram.DataPoints[0].Attributes)
				default:
					t.Fatalf("unexpected metric %s", metric.Name)
				}
			}
		})
	}
}

func TestNetworkIngressObservationErrorTelemetry(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{networkIngressOperationApply, networkIngressOperationObserve} {
		for _, tc := range []struct {
			name            string
			err             error
			observationCode string
			code            string
		}{
			{"bounded observation", errors.New("sensitive"), NetworkIngressErrorInvalidCredentials, NetworkIngressErrorInvalidCredentials},
			{"unknown observation", errors.New("sensitive"), "sensitive", networkIngressErrorCodeInternal},
			{"none with an error", errors.New("sensitive"), networkIngressErrorCodeNone, networkIngressErrorCodeInternal},
			{"untyped pending observation", errors.New("sensitive"), networkIngressErrorCodeDeletionPending, networkIngressErrorCodeInternal},
			{"typed error takes precedence", ErrNetworkIngressProviderCredentialsRejected, NetworkIngressErrorInvalidCredentials, NetworkIngressErrorProviderCredentialsRejected},
			{"replacement pending", ErrNetworkIngressReplacementPending, "", networkIngressErrorCodeReplacementPending},
			{"deletion pending outside delete", ErrNetworkIngressDeletionPending, "", networkIngressErrorCodeDeletionPending},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				var logs bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logs, nil))
				provisioner := ObserveNetworkIngressProvisioner(NetworkIngressProviderTailscale,
					telemetryNetworkIngressProvisioner{observation: NetworkIngressObservation{ErrorCode: tc.observationCode}, err: tc.err}, logger, nil)
				var err error
				if operation == networkIngressOperationApply {
					_, err = provisioner.Apply(t.Context(), NetworkIngressDesired{})
				} else {
					_, err = provisioner.Observe(t.Context(), NetworkIngressResourceNames{})
				}
				require.ErrorIs(t, err, tc.err)
				var log map[string]any
				require.NoError(t, json.Unmarshal(logs.Bytes(), &log))
				require.Equal(t, "ERROR", log["level"])
				require.Equal(t, networkIngressResultError, log[string(attr.OutcomeKey)])
				require.Equal(t, tc.code, log[string(attr.NetworkIngressErrorCodeKey)])
				require.NotContains(t, logs.String(), "sensitive")
			})
		}
	}
}
