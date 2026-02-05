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
	OrgID          string
	Domain         string
	Action         CustomDomainIngressAction
	IngressName    string // Used for delete action to avoid DB lookup
	CertSecretName string // Used for delete action to avoid DB lookup
}

func (c *CustomDomainIngress) Do(ctx context.Context, args CustomDomainIngressArgs) error {
	// Delete action uses pre-supplied ingress details to avoid reading a soft-deleted record.
	if args.Action == CustomDomainIngressActionDelete {
		if args.IngressName == "" || args.CertSecretName == "" {
			return oops.E(oops.CodeUnexpected, errors.New("ingress name or cert secret name is empty"), "ingress name or cert secret name is empty").Log(ctx, c.logger)
		}

		err := c.k8s.DeleteIngress(ctx, args.IngressName, args.CertSecretName)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete custom domain ingress").Log(ctx, c.logger)
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

	return nil
}
