package activities

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	customdomainsRepo "github.com/speakeasy-api/gram/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/internal/k8s"
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
	OrgID    string
	DomainID string
	Action   CustomDomainIngressAction
}

func (c *CustomDomainIngress) Do(ctx context.Context, args CustomDomainIngressArgs) error {
	return nil
}
