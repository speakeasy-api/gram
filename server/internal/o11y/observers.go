package o11y

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y/repo"
)

const (
	observableOrganizationsCount                  = "organizations.count"
	observableProjectsCount                       = "projects.count"
	observableDeploymentsHttpSecuritySchemesCount = "deployments.http_security_schemes.count"
)

var recognizedSecuritySchemeType = map[string]struct{}{
	"apiKey":        {},
	"http":          {},
	"mutualTLS":     {},
	"oauth2":        {},
	"openIdConnect": {},
}

var recognizedHTTPSecurity = map[string]struct{}{
	"bearer": {},
	"basic":  {},
	"digest": {},
}

type observers struct {
	meter  metric.Meter
	gauges map[string]metric.Int64ObservableGauge

	db *pgxpool.Pool
}

func StartObservers(provider metric.MeterProvider, db *pgxpool.Pool) error {
	var err error
	var gauges = make(map[string]metric.Int64ObservableGauge)
	meter := provider.Meter("github.com/speakeasy-api/gram/internal/o11y")

	o := &observers{
		meter:  meter,
		gauges: gauges,
		db:     db,
	}

	if gauges[observableOrganizationsCount], err = meter.Int64ObservableGauge(
		observableOrganizationsCount,
		metric.WithDescription("Count of Gram organizations"),
		metric.WithUnit("{#}"),
		metric.WithInt64Callback(o.observeOrganizationsCount),
	); err != nil {
		return fmt.Errorf("create observable gauge %q: %w", observableOrganizationsCount, err)
	}

	if gauges[observableProjectsCount], err = meter.Int64ObservableGauge(
		observableProjectsCount,
		metric.WithDescription("Count of Gram projects that are not deleted"),
		metric.WithUnit("{#}"),
		metric.WithInt64Callback(o.observeProjectsCount),
	); err != nil {
		return fmt.Errorf("create observable gauge %q: %w", observableProjectsCount, err)
	}

	if gauges[observableDeploymentsHttpSecuritySchemesCount], err = meter.Int64ObservableGauge(
		observableDeploymentsHttpSecuritySchemesCount,
		metric.WithDescription("Count of HTTP security schemes across latest deployments"),
		metric.WithUnit("{#}"),
		metric.WithInt64Callback(o.observeDeploymentsHttpSecuritySchemesCount),
	); err != nil {
		return fmt.Errorf("create observable gauge %q: %w", observableDeploymentsHttpSecuritySchemesCount, err)
	}

	return nil
}

func (o *observers) observeDeploymentsHttpSecuritySchemesCount(ctx context.Context, observer metric.Int64Observer) error {
	r := repo.New(o.db)

	schemes, err := r.StatHTTPSecuritySchemes(ctx)
	if err != nil {
		return fmt.Errorf("observer: stat http security schemes: %w", err)
	}

	for _, scheme := range schemes {
		t := scheme.Type.String
		if _, ok := recognizedSecuritySchemeType[t]; !ok {
			t = "unrecognized"
		}

		if t == "http" {
			s := scheme.Scheme.String
			if _, ok := recognizedHTTPSecurity[s]; !ok {
				s = "unrecognized"
			}

			t = t + "-" + s
		}

		observer.Observe(scheme.Count, metric.WithAttributes(
			attr.SecurityType(t),
		))
	}

	return nil
}

func (o *observers) observeOrganizationsCount(ctx context.Context, observer metric.Int64Observer) error {
	r := repo.New(o.db)

	count, err := r.StatOrganizationsCount(ctx)
	if err != nil {
		return fmt.Errorf("observer: stat organizations count: %w", err)
	}

	observer.Observe(count)

	return nil
}

func (o *observers) observeProjectsCount(ctx context.Context, observer metric.Int64Observer) error {
	r := repo.New(o.db)

	count, err := r.StatProjectsCount(ctx)
	if err != nil {
		return fmt.Errorf("observer: stat projects count: %w", err)
	}

	observer.Observe(count)

	return nil
}
