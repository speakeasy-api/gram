package analytics

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	sigintRepo "github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

// FeatureChecker says whether an organization has a product feature.
type FeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// CatalogReader serves the catalog one tenant may read: every dataset
// except those behind a product feature its organization lacks. Engine is
// the implementation; widget and dashboard validation read through it so a
// saved question can name only what a query could run.
type CatalogReader interface {
	Catalog(ctx context.Context, tenant Tenant) (*Catalog, error)
}

var _ CatalogReader = (*Engine)(nil)

// Engine is the query path without its transport, shared by the analytics
// RPC, the Explore page and the Platform MCP: the catalog bound to its
// loaders, the product features that gate datasets, and the connection plans
// run on.
type Engine struct {
	catalog  *Catalog
	features FeatureChecker
	ch       Querier
}

// NewEngine binds the default catalog's lookups to their loaders over db and
// runs plans on ch; a lookup without a loader is a programming error. A nil
// features hides every dataset behind a product feature.
func NewEngine(db *pgxpool.Pool, ch Querier, features FeatureChecker) (*Engine, error) {
	sigint := sigintRepo.New(db)
	catalog, err := Default.WithLoaders(map[string]LookupLoader{
		MCPServerDisplayNamesLookup: mcpServerDisplayNames(hooksRepo.New(db)),
		SensorNamesLookup:           sensorNames(sigint),
		SignalNamesLookup:           signalNames(sigint),
		SensorMinConfidenceLookup:   sensorMinConfidences(sigint),
	})
	if err != nil {
		return nil, fmt.Errorf("bind catalog lookups: %w", err)
	}
	return &Engine{catalog: catalog, features: features, ch: ch}, nil
}

// Catalog is the contract the engine answers for a tenant: what describe
// serves. A dataset behind a product feature the organization lacks is left
// out, as if it did not exist.
func (e *Engine) Catalog(ctx context.Context, tenant Tenant) (*Catalog, error) {
	datasets := make([]*Dataset, 0, len(e.catalog.datasets))
	for _, ds := range e.catalog.datasets {
		available, err := e.available(ctx, tenant, ds)
		if err != nil {
			return nil, err
		}
		if available {
			datasets = append(datasets, ds)
		}
	}
	return &Catalog{datasets: datasets, lookups: e.catalog.lookups}, nil
}

// available says whether the tenant's organization may read a dataset.
func (e *Engine) available(ctx context.Context, tenant Tenant, ds *Dataset) (bool, error) {
	if ds.Feature == "" {
		return true, nil
	}
	if e.features == nil {
		return false, nil
	}
	enabled, err := e.features.IsFeatureEnabled(ctx, tenant.OrganizationID, ds.Feature)
	if err != nil {
		return false, fmt.Errorf("check %s for dataset %s: %w", ds.Feature, ds.Name, err)
	}
	return enabled, nil
}

// admit refuses a dataset the tenant's organization may not read the way the
// compiler refuses an unknown one, so a refusal does not reveal it. A name
// the catalog does not know is left for the compiler.
func (e *Engine) admit(ctx context.Context, tenant Tenant, dataset string) error {
	ds, ok := e.catalog.Dataset(dataset)
	if !ok {
		return nil
	}
	available, err := e.available(ctx, tenant, ds)
	if err != nil {
		return err
	}
	if !available {
		return newError(ErrUnknownDataset, dataset, "dataset", dataset, fmt.Sprintf("dataset %q does not exist", dataset))
	}
	return nil
}

// QueryResult is a run query: the plan that answered it and its rows.
type QueryResult struct {
	Dataset string
	// Plan is a stable diagnostic identifier, not a table name.
	Plan    string
	Columns []Column
	Rows    []Row
}

// Query loads the lookups the request reads, compiles it and runs it. A
// request the compiler rejects comes back as an *Error.
func (e *Engine) Query(ctx context.Context, tenant Tenant, req Request) (*QueryResult, error) {
	if err := e.admit(ctx, tenant, req.Dataset); err != nil {
		return nil, fmt.Errorf("admit analytics query: %w", err)
	}
	lookups, err := e.catalog.LoadLookups(ctx, tenant, req.Dataset, req.Reads())
	if err != nil {
		return nil, fmt.Errorf("load the dataset's lookups: %w", err)
	}
	plan, err := Compile(e.catalog, tenant, lookups, req)
	if err != nil {
		return nil, fmt.Errorf("compile analytics query: %w", err)
	}
	rows, err := plan.Run(ctx, e.ch)
	if err != nil {
		return nil, fmt.Errorf("run analytics query: %w", err)
	}
	return &QueryResult{Dataset: plan.Dataset, Plan: plan.Name, Columns: plan.Columns, Rows: rows}, nil
}

// Values lists a dimension's values inside a window, most frequent first,
// read through its lookup like a query. Errors are as for Query.
func (e *Engine) Values(ctx context.Context, tenant Tenant, req ValuesRequest) ([]DimensionValue, error) {
	if err := e.admit(ctx, tenant, req.Dataset); err != nil {
		return nil, fmt.Errorf("admit dimension values query: %w", err)
	}
	lookups, err := e.catalog.LoadLookups(ctx, tenant, req.Dataset, []string{req.Dimension})
	if err != nil {
		return nil, fmt.Errorf("load the dataset's lookups: %w", err)
	}
	plan, err := CompileValues(e.catalog, tenant, lookups, req)
	if err != nil {
		return nil, fmt.Errorf("compile dimension values query: %w", err)
	}
	values, err := plan.RunValues(ctx, e.ch)
	if err != nil {
		return nil, fmt.Errorf("list dimension values: %w", err)
	}
	return values, nil
}

// mcpServerDisplayNames loads a project's hook server-name overrides, raw
// name to display name, on every request so a query speaks the names the
// page shows now.
func mcpServerDisplayNames(hooks *hooksRepo.Queries) LookupLoader {
	return func(ctx context.Context, tenant Tenant) (map[string]string, error) {
		projectID, err := uuid.Parse(tenant.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("parse project id: %w", err)
		}
		overrides, err := hooks.ListHooksServerNameOverrides(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("list hook server name overrides: %w", err)
		}
		names := make(map[string]string, len(overrides))
		for _, override := range overrides {
			names[override.RawServerName] = override.DisplayName
		}
		return names, nil
	}
}

// sensorNames loads a project's sensor names by id, deleted sensors included.
func sensorNames(sigint *sigintRepo.Queries) LookupLoader {
	return func(ctx context.Context, tenant Tenant) (map[string]string, error) {
		rows, err := listSensorLabels(ctx, sigint, tenant)
		if err != nil {
			return nil, err
		}
		names := make(map[string]string, len(rows))
		for _, row := range rows {
			names[row.ID.String()] = row.Name
		}
		return names, nil
	}
}

// sensorMinConfidences loads each sensor's minimum classifier confidence by
// id, as the shortest decimal that reads back as the stored value.
func sensorMinConfidences(sigint *sigintRepo.Queries) LookupLoader {
	return func(ctx context.Context, tenant Tenant) (map[string]string, error) {
		rows, err := listSensorLabels(ctx, sigint, tenant)
		if err != nil {
			return nil, err
		}
		thresholds := make(map[string]string, len(rows))
		for _, row := range rows {
			if row.MinConfidence.Valid {
				thresholds[row.ID.String()] = strconv.FormatFloat(row.MinConfidence.Float64, 'g', -1, 64)
			}
		}
		return thresholds, nil
	}
}

func listSensorLabels(ctx context.Context, sigint *sigintRepo.Queries, tenant Tenant) ([]sigintRepo.ListSensorAnalyticsLabelsRow, error) {
	projectID, err := uuid.Parse(tenant.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}
	rows, err := sigint.ListSensorAnalyticsLabels(ctx, sigintRepo.ListSensorAnalyticsLabelsParams{ProjectID: projectID, OrganizationID: tenant.OrganizationID})
	if err != nil {
		return nil, fmt.Errorf("list sensor analytics labels: %w", err)
	}
	return rows, nil
}

// signalNames loads a project's signal names by id, deleted signals included.
func signalNames(sigint *sigintRepo.Queries) LookupLoader {
	return func(ctx context.Context, tenant Tenant) (map[string]string, error) {
		projectID, err := uuid.Parse(tenant.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("parse project id: %w", err)
		}
		rows, err := sigint.ListSignalAnalyticsLabels(ctx, sigintRepo.ListSignalAnalyticsLabelsParams{ProjectID: projectID, OrganizationID: tenant.OrganizationID})
		if err != nil {
			return nil, fmt.Errorf("list signal analytics labels: %w", err)
		}
		names := make(map[string]string, len(rows))
		for _, row := range rows {
			names[row.ID.String()] = row.Name
		}
		return names, nil
	}
}

// UnixNano is undefined outside the years 1678 to 2262, so a bound is checked
// against the int64 range before the conversion.
var (
	minUnixNanoTime = time.Unix(0, math.MinInt64)
	maxUnixNanoTime = time.Unix(0, math.MaxInt64)
)

func representable(t time.Time) bool {
	return !t.Before(minUnixNanoTime) && !t.After(maxUnixNanoTime)
}

// ParseWindow turns a request's RFC 3339 bounds into Unix nanoseconds; a
// bound that does not parse or lies outside that range is an *Error naming it.
func ParseWindow(from, to string) (fromUnixNano, toUnixNano int64, err error) {
	start, err := parseBound("from", from)
	if err != nil {
		return 0, 0, err
	}
	end, err := parseBound("to", to)
	if err != nil {
		return 0, 0, err
	}
	return start.UnixNano(), end.UnixNano(), nil
}

func parseBound(name, value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, newError(ErrInvalidTimeRange, "", name, value, name+" is not an RFC 3339 time")
	}
	if !representable(t) {
		return time.Time{}, newError(ErrInvalidTimeRange, "", name, value, name+" is outside the years 1678 to 2262")
	}
	return t, nil
}
