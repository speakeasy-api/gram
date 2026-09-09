package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
)

func newNetworkIngressExecutor(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, clients *k8s.KubernetesClients, config networkingress.RuntimeConfig) (*networkingress.Executor, error) {
	providers := make(map[string]k8s.NetworkIngressProvisioner)
	if clients != nil && clients.Clientset != nil && clients.DynamicClient != nil && config.Tailscale.OperatorNamespace != "" {
		provider, err := k8s.NewTailscaleNetworkIngressProvisioner(clients.Clientset, clients.DynamicClient, config.Tailscale)
		if err != nil {
			return nil, fmt.Errorf("configure network ingress cleanup provider: %w", err)
		}
		providers[networkingress.ProviderTailscale] = provider
	}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(providers, logger, k8s.NewNetworkIngressMetrics(logger, meterProvider))
	if err != nil {
		return nil, fmt.Errorf("configure network ingress registry: %w", err)
	}
	return networkingress.NewExecutor(db, enc, registry, networkingress.ExecutorOptions{
		Queue: config.ReconcileTaskQueue, Image: config.AttestorImage,
		BackendService: config.BackendService, BackendPort: config.BackendPort,
		CanApply: func(context.Context) error {
			if !config.MutationReady() {
				return fmt.Errorf("network ingress provider mutations disabled")
			}
			return nil
		},
	}), nil
}
