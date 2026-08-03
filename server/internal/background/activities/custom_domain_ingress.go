package activities

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type CustomDomainIngressAction string

const (
	CustomDomainIngressActionSetup  CustomDomainIngressAction = "setup"
	CustomDomainIngressActionDelete CustomDomainIngressAction = "delete"
	// CustomDomainIngressActionReapply is retained for Temporal compatibility.
	CustomDomainIngressActionReapply CustomDomainIngressAction = "reapply"
)

type CustomDomainIngress struct {
	domains            *customdomainsRepo.Queries
	logger             *slog.Logger
	provisionerFactory k8s.ProvisionerFactory
	setupSleep         time.Duration
}

// CustomDomainIngressOption configures a CustomDomainIngress.
type CustomDomainIngressOption func(*CustomDomainIngress)

// WithSetupSleep overrides the post-Setup convergence wait. Intended for tests.
func WithSetupSleep(d time.Duration) CustomDomainIngressOption {
	return func(c *CustomDomainIngress) {
		c.setupSleep = d
	}
}

func NewCustomDomainIngress(logger *slog.Logger, db *pgxpool.Pool, k8sClient k8s.ProvisionerFactory, opts ...CustomDomainIngressOption) *CustomDomainIngress {
	c := &CustomDomainIngress{
		domains:            customdomainsRepo.New(db),
		logger:             logger,
		provisionerFactory: k8sClient,
		setupSleep:         120 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type CustomDomainIngressArgs struct {
	OrgID  string
	Domain string
	Action CustomDomainIngressAction
	// TODO: Remove IngressName in a follow-up release once all in-flight workflows have drained.
	IngressName     string // Legacy field — kept for in-flight workflow compat. Prefer ResourceName when non-empty.
	ResourceName    string // Generic resource name. Preferred over IngressName.
	CertSecretName  string
	ProvisionerKind k8s.ProvisionerKind // Empty = use activity default.
	// IPAllowlist is retained only for Temporal compatibility with workflow
	// histories created before reconciliation became DB-derived.
	IPAllowlist []string
}

type ReconcileCustomDomainArgs struct {
	CustomDomainID uuid.UUID
}

func (c *CustomDomainIngress) resolveKind(args CustomDomainIngressArgs) k8s.ProvisionerKind {
	if args.ProvisionerKind != "" {
		return args.ProvisionerKind
	}
	return k8s.ProvisionerKindIngress
}

func (c *CustomDomainIngress) Do(ctx context.Context, args CustomDomainIngressArgs) error {
	kind := c.resolveKind(args)
	provisioner := c.provisionerFactory.Provisioner(kind)

	if args.Action == CustomDomainIngressActionDelete {
		resourceName := args.ResourceName
		if resourceName == "" {
			resourceName = args.IngressName
		}
		if resourceName == "" {
			return oops.E(oops.CodeUnexpected, errors.New("resource name is empty"), "resource name is empty").LogError(ctx, c.logger)
		}

		if err := provisioner.Delete(ctx, resourceName, args.CertSecretName); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete custom domain resource").LogError(ctx, c.logger)
		}

		return nil
	}

	customDomain, err := c.domains.GetCustomDomainByDomain(ctx, args.Domain)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get custom domain").LogError(ctx, c.logger)
	}

	if customDomain.OrganizationID != args.OrgID {
		return oops.E(oops.CodeUnauthorized, errors.New("custom domain does not belong to organization"), "custom domain does not belong to organization").LogError(ctx, c.logger)
	}

	if args.Action == CustomDomainIngressActionSetup || args.Action == CustomDomainIngressActionReapply {
		return c.ReconcileCustomDomain(ctx, ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID})
	}

	return nil
}

// ReconcileCustomDomain is the single desired-state write path for custom
// domain Kubernetes resources. The domain-scoped Temporal workflow serializes
// calls and schedules another pass when desired state changes during Apply.
func (c *CustomDomainIngress) ReconcileCustomDomain(ctx context.Context, args ReconcileCustomDomainArgs) error {
	desired, err := c.domains.GetCustomDomainRouteConfig(ctx, args.CustomDomainID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load custom domain route config").LogError(ctx, c.logger)
	}

	kind := k8s.ProvisionerKind(desired.ProvisionerKind)
	if kind == "" {
		kind = k8s.ProvisionerKindIngress
	}

	var rootTarget *string
	if desired.RootSlug != "" {
		target := "/mcp/" + desired.RootSlug
		rootTarget = &target
	}
	config := k8s.RouteConfig{
		Domain:      desired.Domain,
		IPAllowlist: desired.IpAllowlist,
		RootTarget:  rootTarget,
	}
	c.logger.InfoContext(ctx, "reconciling custom domain resource",
		attr.SlogCustomDomainProvisionerKind(string(kind)),
		attr.SlogURLDomain(desired.Domain),
	)

	provisioner := c.provisionerFactory.Provisioner(kind)
	if desired.Deleted {
		// No names on tombstone = never applied or already cleaned: DeleteDomain checkpoints derived identity before tombstoning. Never re-derive here — hostname reuse would delete a successor domain's live resources.
		if !desired.IngressName.Valid || desired.IngressName.String == "" {
			return nil
		}
		if err := provisioner.Delete(ctx, desired.IngressName.String, desired.CertSecretName.String); err != nil {
			return oops.E(oops.CodeUnexpected, err, "delete custom domain resource").LogError(ctx, c.logger)
		}
		if err := c.domains.ClearDeletedCustomDomainResourceNames(ctx, desired.ID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "clear deleted custom domain resource names").LogError(ctx, c.logger)
		}
		return nil
	}

	result, err := provisioner.Apply(ctx, config)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "apply custom domain route").LogError(ctx, c.logger)
	}
	persisted, err := c.domains.UpdateCustomDomainResourceNames(ctx, customdomainsRepo.UpdateCustomDomainResourceNamesParams{
		IngressName:     conv.ToPGText(result.ResourceName),
		CertSecretName:  conv.PtrToPGText(conv.PtrEmpty(result.SecretName)),
		ProvisionerKind: string(kind),
		ID:              desired.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "persist custom domain resource names").LogError(ctx, c.logger)
	}
	if persisted.Deleted {
		c.logger.InfoContext(ctx, "custom domain deletion won during apply; removing applied resource",
			attr.SlogCustomDomainProvisionerKind(string(kind)),
			attr.SlogURLDomain(desired.Domain),
		)
		if err := provisioner.Delete(ctx, result.ResourceName, result.SecretName); err != nil {
			return oops.E(oops.CodeUnexpected, err, "delete custom domain resource applied after deletion").LogError(ctx, c.logger)
		}
		if err := c.domains.ClearDeletedCustomDomainResourceNames(ctx, desired.ID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "clear deleted custom domain resource names").LogError(ctx, c.logger)
		}
		return nil
	}
	if desired.Activated {
		return nil
	}

	timer := time.NewTimer(c.setupSleep)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return oops.E(oops.CodeUnexpected, ctx.Err(), "wait for custom domain resource convergence").LogError(ctx, c.logger)
	case <-timer.C:
	}

	if err := provisioner.Get(ctx, result.ResourceName); err != nil {
		return oops.E(oops.CodeUnexpected, err, "verify custom domain resource exists").LogError(ctx, c.logger)
	}
	if _, err := c.domains.UpdateCustomDomain(ctx, customdomainsRepo.UpdateCustomDomainParams{
		ID:              desired.ID,
		Verified:        true,
		Activated:       true,
		IngressName:     conv.ToPGText(result.ResourceName),
		CertSecretName:  conv.PtrToPGText(conv.PtrEmpty(result.SecretName)),
		ProvisionerKind: string(kind),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.logger.InfoContext(ctx, "custom domain deletion won during resource convergence; skipping activation",
				attr.SlogCustomDomainProvisionerKind(string(kind)),
				attr.SlogURLDomain(desired.Domain),
			)
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "reconcile custom domain").LogError(ctx, c.logger)
	}
	return nil
}
