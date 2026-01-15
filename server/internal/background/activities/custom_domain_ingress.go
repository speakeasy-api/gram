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
	domains *customdomainsRepo.Queries
	logger  *slog.Logger
	k8s     *k8s.KubernetesClients
}

func NewCustomDomainIngress(logger *slog.Logger, db *pgxpool.Pool, k8sClient *k8s.KubernetesClients) *CustomDomainIngress {
	return &CustomDomainIngress{
		domains: customdomainsRepo.New(db),
		logger:  logger,
		k8s:     k8sClient,
	}
}

type CustomDomainIngressArgs struct {
	OrgID  string
	Domain string
	Action CustomDomainIngressAction
}

type EnsureCustomDomainIngressArgs struct {
	Domain string
}

func (c *CustomDomainIngress) Do(ctx context.Context, args CustomDomainIngressArgs) error {
	customDomain, err := c.domains.GetCustomDomainByDomain(ctx, args.Domain)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get custom domain").Log(ctx, c.logger)
	}

	if customDomain.OrganizationID != args.OrgID {
		return oops.E(oops.CodeUnauthorized, errors.New("custom domain does not belong to organization"), "custom domain does not belong to organization").Log(ctx, c.logger)
	}

	if args.Action == CustomDomainIngressActionSetup {
		ingressName, secretName, ingress, err := c.k8s.CreateCustomDomainIngressCharts(customDomain.Domain)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create custom domain ingress").Log(ctx, c.logger)
		}

		c.logger.InfoContext(ctx, "custom domain ingress",
			attr.SlogIngressName(ingressName),
			attr.SlogSecretName(secretName),
		)

		err = c.k8s.CreateOrUpdateIngress(ctx, ingressName, ingress)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create or update custom domain ingress").Log(ctx, c.logger)
		}

		// Wait for ingress to be created
		time.Sleep(120 * time.Second)

		_, err = c.k8s.GetIngress(ctx, ingressName)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to get custom domain ingress").Log(ctx, c.logger)
		}

		_, err = c.domains.UpdateCustomDomain(ctx, customdomainsRepo.UpdateCustomDomainParams{
			ID:             customDomain.ID,
			Verified:       true,
			Activated:      true,
			IngressName:    conv.ToPGText(ingressName),
			CertSecretName: conv.ToPGText(secretName),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to update custom domain").Log(ctx, c.logger)
		}

		return nil
	}

	if args.Action == CustomDomainIngressActionDelete {
		ingressName := conv.FromPGText[string](customDomain.IngressName)
		secretName := conv.FromPGText[string](customDomain.CertSecretName)
		if ingressName == nil || secretName == nil {
			return oops.E(oops.CodeUnexpected, errors.New("ingress name or secret name is nil"), "ingress name or secret name is nil").Log(ctx, c.logger)
		}

		err := c.k8s.DeleteIngress(ctx, *ingressName, *secretName)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete custom domain ingress").Log(ctx, c.logger)
		}
	}

	return nil
}

// Ensure reconciles ingress state for an activated domain. Idempotent; does not write DB.
func (c *CustomDomainIngress) Ensure(ctx context.Context, args EnsureCustomDomainIngressArgs) error {
	if c.k8s == nil || !c.k8s.Enabled() {
		c.logger.InfoContext(ctx, "k8s client not enabled, skipping ingress ensure",
			attr.SlogURLDomain(args.Domain))
		return nil
	}

	customDomain, err := c.domains.GetCustomDomainByDomain(ctx, args.Domain)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get custom domain").Log(ctx, c.logger)
	}

	if !customDomain.Activated {
		c.logger.InfoContext(ctx, "skipping non-activated domain",
			attr.SlogURLDomain(args.Domain))
		return nil
	}

	ingressName, secretName, ingress, err := c.k8s.CreateCustomDomainIngressCharts(customDomain.Domain)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create ingress charts").Log(ctx, c.logger)
	}

	c.logger.InfoContext(ctx, "ensuring custom domain ingress exists",
		attr.SlogURLDomain(args.Domain),
		attr.SlogIngressName(ingressName),
		attr.SlogSecretName(secretName),
	)

	if err := c.k8s.CreateOrUpdateIngress(ctx, ingressName, ingress); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to ensure ingress").Log(ctx, c.logger)
	}

	return nil
}
