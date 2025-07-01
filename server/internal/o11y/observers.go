package o11y

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/internal/o11y/repo"
)

const (
	observableDeploymentsHttpSecuritySchemes = "deployments.http_security_schemes"
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
	meter := provider.Meter("gram")

	o := &observers{
		meter:  meter,
		gauges: gauges,
		db:     db,
	}

	if gauges[observableDeploymentsHttpSecuritySchemes], err = meter.Int64ObservableGauge(
		observableDeploymentsHttpSecuritySchemes,
		metric.WithDescription("Count of HTTP security schemes across latest deployments"),
		metric.WithUnit("By"),
		metric.WithInt64Callback(o.observeDeploymentsHttpSecuritySchemes),
	); err != nil {
		return fmt.Errorf("create observable gauge %q: %w", observableDeploymentsHttpSecuritySchemes, err)
	}

	return nil
}

func (o *observers) observeDeploymentsHttpSecuritySchemes(ctx context.Context, observer metric.Int64Observer) error {
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
			attribute.String("http.security.type", t),
		))
	}

	return nil
}
