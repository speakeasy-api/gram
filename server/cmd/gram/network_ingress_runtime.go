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

// newNetworkIngressExecutor registers the tailnet gateway as the Tailscale
// provider. enc decrypts the customer's saved Tailscale credentials; the
// gateway's own secrets use organization encryption (orgEnc, orgKeys), and
// either may be nil when it is not configured.
//
// lockDB serves only the executor's reconcile connections, each held for a
// whole reconcile because it carries the reconcile lock; providers and
// organization encryption use db. A reconcile then never waits for a
// connection of the kind it holds, so reconciles cannot exhaust a pool between
// them and wait on each other for good.
func newNetworkIngressExecutor(logger *slog.Logger, meterProvider metric.MeterProvider, db, lockDB *pgxpool.Pool, enc *encryption.Client, orgEnc *orgencryption.Service, orgKeys *orgencryption.Provisioner, config networkingress.RuntimeConfig, publicationRequester networkingress.PublicationRequester) (*networkingress.Executor, error) {
	// A missing encryption key only disables Apply; observation and teardown
	// still run, and the executor's gate reports the gap.
	api, err := tailscaleapi.New(guardian.NewDefaultPolicy(otel.GetTracerProvider()), config.Gateway.APIURL)
	if err != nil {
		return nil, fmt.Errorf("configure Tailscale API client: %w", err)
	}
	tag := config.Gateway.Tag
	if tag == "" {
		tag = networkingress.DefaultGatewayTag
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
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{
		networkingress.ProviderTailscale: gateway,
	}, logger, k8s.NewNetworkIngressMetrics(logger, meterProvider))
	if err != nil {
		return nil, fmt.Errorf("configure network ingress registry: %w", err)
	}
	return networkingress.NewExecutor(lockDB, enc, registry, networkingress.ExecutorOptions{
		Queue:                config.ReconcileTaskQueue,
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
