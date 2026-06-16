package activities

import (
	"context"
	"errors"
	"log/slog"
	"time"

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
	// CustomDomainIngressActionReapply re-applies the current IP allowlist to an
	// already-provisioned resource. Used by the edit flow. It skips the
	// convergence wait and the verified/activated DB flip since the resource and
	// its cert already exist.
	CustomDomainIngressActionReapply CustomDomainIngressAction = "reapply"
)

type CustomDomainIngress struct {
	domains            *customdomainsRepo.Queries
	logger             *slog.Logger
	provisionerFactory k8s.ProvisionerFactory
	defaultProvisioner k8s.ProvisionerKind
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

func NewCustomDomainIngress(logger *slog.Logger, db *pgxpool.Pool, k8sClient k8s.ProvisionerFactory, defaultProvisioner k8s.ProvisionerKind, opts ...CustomDomainIngressOption) *CustomDomainIngress {
	c := &CustomDomainIngress{
		domains:            customdomainsRepo.New(db),
		logger:             logger,
		provisionerFactory: k8sClient,
		defaultProvisioner: defaultProvisioner,
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
	ResourceName    string // Generic resource name (Ingress or HTTPRoute). Preferred over IngressName.
	CertSecretName  string
	ProvisionerKind k8s.ProvisionerKind // Empty = use activity default.
	// IPAllowlist is the allowlist to apply on the Reapply action. It is passed
	// explicitly (not read from the DB) so the caller can reconcile k8s before
	// persisting. Unused by Setup (which reads the persisted value) and Delete.
	IPAllowlist []string
}

func (c *CustomDomainIngress) resolveKind(args CustomDomainIngressArgs) k8s.ProvisionerKind {
	if args.ProvisionerKind != "" {
		return args.ProvisionerKind
	}
	if c.defaultProvisioner != "" {
		return c.defaultProvisioner
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

	if args.Action == CustomDomainIngressActionReapply {
		c.logger.InfoContext(ctx, "re-applying custom domain ip allowlist",
			attr.SlogCustomDomainProvisionerKind(string(kind)),
			attr.SlogURLDomain(customDomain.Domain),
		)

		// Setup is idempotent (create-or-update) and applies the allowlist passed
		// in args (the caller persists it only after this succeeds). The resource
		// and its cert already exist, so there is no convergence wait and no
		// verified/activated flip.
		if _, err := provisioner.Setup(ctx, customDomain.Domain, args.IPAllowlist); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to re-apply custom domain ip allowlist").LogError(ctx, c.logger)
		}

		return nil
	}

	if args.Action == CustomDomainIngressActionSetup {
		c.logger.InfoContext(ctx, "provisioning custom domain resource",
			attr.SlogCustomDomainProvisionerKind(string(kind)),
			attr.SlogURLDomain(customDomain.Domain),
		)

		result, err := provisioner.Setup(ctx, customDomain.Domain, customDomain.IpAllowlist)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to provision custom domain resource").LogError(ctx, c.logger)
		}

		// Wait for resource convergence — cert issuance and LB propagation.
		// Both Ingress and Gateway kinds keep this sleep until status-condition polling is implemented.
		time.Sleep(c.setupSleep)

		if err := provisioner.Get(ctx, result.ResourceName); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to verify custom domain resource exists").LogError(ctx, c.logger)
		}

		_, err = c.domains.UpdateCustomDomain(ctx, customdomainsRepo.UpdateCustomDomainParams{
			ID:              customDomain.ID,
			Verified:        true,
			Activated:       true,
			IngressName:     conv.ToPGText(result.ResourceName),
			CertSecretName:  conv.PtrToPGText(conv.PtrEmpty(result.SecretName)),
			ProvisionerKind: string(kind),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to update custom domain").LogError(ctx, c.logger)
		}

		return nil
	}

	return nil
}
