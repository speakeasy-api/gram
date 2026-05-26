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
)

type CustomDomainIngress struct {
	domains            *customdomainsRepo.Queries
	logger             *slog.Logger
	k8sFactory         k8s.ProvisionerFactory
	defaultProvisioner k8s.ProvisionerKind
	setupSleep         time.Duration
}

func NewCustomDomainIngress(logger *slog.Logger, db *pgxpool.Pool, k8sClient k8s.ProvisionerFactory, defaultProvisioner k8s.ProvisionerKind) *CustomDomainIngress {
	return &CustomDomainIngress{
		domains:            customdomainsRepo.New(db),
		logger:             logger,
		k8sFactory:         k8sClient,
		defaultProvisioner: defaultProvisioner,
		setupSleep:         120 * time.Second,
	}
}

// SetSetupSleep overrides the post-Setup convergence wait. Intended for tests.
func (c *CustomDomainIngress) SetSetupSleep(d time.Duration) {
	c.setupSleep = d
}

type CustomDomainIngressArgs struct {
	OrgID           string
	Domain          string
	Action          CustomDomainIngressAction
	IngressName     string // Legacy field — kept for in-flight workflow compat. Prefer ResourceName when non-empty.
	ResourceName    string // Generic resource name (Ingress or HTTPRoute). Preferred over IngressName.
	CertSecretName  string
	ProvisionerKind k8s.ProvisionerKind // Empty = use activity default.
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
	provisioner := c.k8sFactory.Provisioner(kind)

	if args.Action == CustomDomainIngressActionDelete {
		resourceName := args.ResourceName
		if resourceName == "" {
			resourceName = args.IngressName
		}
		if resourceName == "" {
			return oops.E(oops.CodeUnexpected, errors.New("resource name is empty"), "resource name is empty").Log(ctx, c.logger)
		}

		if err := provisioner.Delete(ctx, resourceName, args.CertSecretName); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete custom domain resource").Log(ctx, c.logger)
		}

		return nil
	}

	customDomain, err := c.domains.GetCustomDomainByDomain(ctx, args.Domain)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get custom domain").Log(ctx, c.logger)
	}

	if customDomain.OrganizationID != args.OrgID {
		return oops.E(oops.CodeUnauthorized, errors.New("custom domain does not belong to organization"), "custom domain does not belong to organization").Log(ctx, c.logger)
	}

	if args.Action == CustomDomainIngressActionSetup {
		c.logger.InfoContext(ctx, "provisioning custom domain resource",
			attr.SlogProvisionerKind(string(kind)),
			attr.SlogURLDomain(customDomain.Domain),
		)

		result, err := provisioner.Setup(ctx, customDomain.Domain)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to provision custom domain resource").Log(ctx, c.logger)
		}

		// Wait for resource convergence — cert issuance and LB propagation.
		// Both Ingress and Gateway kinds keep this sleep until status-condition polling is implemented.
		time.Sleep(c.setupSleep)

		if err := provisioner.Get(ctx, result.ResourceName); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to verify custom domain resource exists").Log(ctx, c.logger)
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
			return oops.E(oops.CodeUnexpected, err, "failed to update custom domain").Log(ctx, c.logger)
		}

		return nil
	}

	return nil
}
