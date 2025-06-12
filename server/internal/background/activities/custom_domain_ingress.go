package activities

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/conv"
	customdomainsRepo "github.com/speakeasy-api/gram/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/internal/k8s"
	"github.com/speakeasy-api/gram/internal/oops"
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
			slog.String("ingress_name", ingressName),
			slog.String("secret_name", secretName),
		)

		err = c.k8s.CreateOrUpdateIngress(ctx, ingressName, ingress)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create or update custom domain ingress").Log(ctx, c.logger)
		}

		// Wait for ingress to be created
		time.Sleep(60 * time.Second)

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
