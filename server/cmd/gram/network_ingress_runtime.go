package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

// newNetworkIngressExecutor registers the Tailscale provider as a composite
// that routes by runtime: gateway ingresses to the tailnet gateway, every
// other ingress to the operator provider while it is still enabled. enc
// decrypts the customer's saved Tailscale credentials; the gateway's own
// secrets use organization encryption (orgEnc, orgKeys), and either may be nil
// when it is not configured.
//
// lockDB serves only the executor's reconcile connections, each held for a
// whole reconcile because it carries the reconcile lock; providers and
// organization encryption use db. A reconcile then never waits for a
// connection of the kind it holds, so reconciles cannot exhaust a pool between
// them and wait on each other for good.
func newNetworkIngressExecutor(logger *slog.Logger, meterProvider metric.MeterProvider, db, lockDB *pgxpool.Pool, enc *encryption.Client, orgEnc *orgencryption.Service, orgKeys *orgencryption.Provisioner, clients *k8s.KubernetesClients, config networkingress.RuntimeConfig, publicationRequester networkingress.PublicationRequester) (*networkingress.Executor, error) {
	providers := make(map[string]k8s.NetworkIngressProvisioner)
	var operator *k8s.TailscaleNetworkIngressProvisioner
	if config.OperatorProviderEnabled && clients != nil && clients.Clientset != nil && clients.DynamicClient != nil && config.Tailscale.OperatorNamespace != "" {
		provider, err := k8s.NewTailscaleNetworkIngressProvisioner(clients.Clientset, clients.DynamicClient, config.Tailscale)
		if err != nil {
			return nil, fmt.Errorf("configure network ingress cleanup provider: %w", err)
		}
		operator = provider
	}
	// The composite always routes by the ingress's runtime column. A missing
	// encryption key only disables Apply; observation and teardown still run,
	// and the executor's gate reports the gap.
	api, err := tailscaleapi.New(guardian.NewDefaultPolicy(otel.GetTracerProvider()), config.Gateway.APIURL)
	if err != nil {
		return nil, fmt.Errorf("configure Tailscale API client: %w", err)
	}
	tag := config.Gateway.Tag
	if tag == "" {
		tag = tailnetgateway.DefaultTag
	}
	slots := config.Gateway.Slots
	if slots <= 0 {
		slots = tailnetgateway.DefaultSlots
	}
	gateway, err := tailnetgateway.NewProvisioner(logger, db, orgEnc, gatewayKeyProvisioner(orgKeys), api, tailnetgateway.ProvisionerConfig{
		Tag: tag, Slots: slots, Namer: config.Gateway.Namer, AuthKeyTTL: 0, StaleAfter: 0, Now: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("configure tailnet gateway provider: %w", err)
	}
	var operatorProvider tailnetgateway.OperatorProvisioner
	if operator != nil {
		operatorProvider = operator
	}
	composite, err := tailnetgateway.NewCompositeProvisioner(operatorProvider, gateway, config.Gateway.Enabled)
	if err != nil {
		return nil, fmt.Errorf("configure network ingress provider: %w", err)
	}
	providers[networkingress.ProviderTailscale] = composite
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(providers, logger, k8s.NewNetworkIngressMetrics(logger, meterProvider))
	if err != nil {
		return nil, fmt.Errorf("configure network ingress registry: %w", err)
	}
	return networkingress.NewExecutor(lockDB, enc, registry, networkingress.ExecutorOptions{
		Queue: config.ReconcileTaskQueue, Image: config.AttestorImage,
		BackendService: config.BackendService, BackendPort: config.BackendPort,
		AttestorRequired:     !config.Gateway.Enabled,
		PublicationRequester: publicationRequester,
		PublicationActor:     "",
		Logger:               logger,
		CanApply: func(context.Context) error {
			if !config.MutationReady() {
				return fmt.Errorf("network ingress provider mutations disabled")
			}
			return nil
		},
	}), nil
}

// gatewayKeyProvisioner keeps a nil provisioner a nil interface.
func gatewayKeyProvisioner(provisioner *orgencryption.Provisioner) tailnetgateway.KeyProvisioner {
	if provisioner == nil {
		return nil
	}
	return provisioner
}

// newNetworkIngressLockDB opens the pool newNetworkIngressExecutor takes
// reconcile connections from, separate from the process's main pool.
func newNetworkIngressLockDB(ctx context.Context, logger *slog.Logger, meterProvider metric.MeterProvider, c *cli.Context) (*pgxpool.Pool, error) {
	db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{enableUnsafeLogging: c.Bool("unsafe-db-log")})
	if err != nil {
		return nil, fmt.Errorf("open network ingress reconcile pool: %w", err)
	}
	return db, nil
}
