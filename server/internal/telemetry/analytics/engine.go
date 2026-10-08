package analytics

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

// Engine is the query path without its transport: the catalog bound to its
// loaders, and the connection plans run on. The analytics RPC and the
// Platform MCP both answer through one, so a request is validated, compiled
// and run the same way whichever surface it came from, and the guardrails
// the compiler enforces hold on both.
type Engine struct {
	catalog *Catalog
	ch      Querier
}

// NewEngine binds the default catalog's lookups to their loaders over db and
// runs plans on ch. The catalog declares the lookups; the engine is what can
// load them. A declared lookup without a loader is a programming error of the
// same kind as a half-declared dataset, so it fails here rather than reading
// raw values in production.
func NewEngine(db *pgxpool.Pool, ch Querier) (*Engine, error) {
	catalog, err := Default.WithLoaders(map[string]LookupLoader{
		MCPServerDisplayNamesLookup: mcpServerDisplayNames(hooksRepo.New(db)),
	})
	if err != nil {
		return nil, fmt.Errorf("bind catalog lookups: %w", err)
	}
	return &Engine{catalog: catalog, ch: ch}, nil
}

// Catalog is the contract the engine answers: what describe serves.
func (e *Engine) Catalog() *Catalog {
	return e.catalog
}

// QueryResult is a run query: the plan that answered it and its rows.
type QueryResult struct {
	Dataset string
	// Plan is the stable diagnostic identifier of how the query was answered,
	// deliberately not a table name.
	Plan    string
	Columns []Column
	Rows    []Row
}

// Query loads the lookups the request reads, compiles it against the catalog
// and runs it. A request the compiler rejects comes back as an *Error naming
// what is wrong; any other error is a failure of the engine, not the request.
func (e *Engine) Query(ctx context.Context, tenant Tenant, req Request) (*QueryResult, error) {
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

// Values lists the values a dimension holds inside a window, most frequent
// first, read through the dimension's lookup the way a query reads it. Errors
// are as for Query.
func (e *Engine) Values(ctx context.Context, tenant Tenant, req ValuesRequest) ([]DimensionValue, error) {
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

// mcpServerDisplayNames loads a project's hook server-name overrides as the
// map mcp_server reads through: raw name to display name. It is one indexed
// Postgres read per request, since the overrides are mutable settings and
// a query must speak the names the page shows now.
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

// The int64 nanosecond range covers the years 1678 to 2262. UnixNano is
// undefined outside it, so a bound past it would reach the compiler as some
// other window; representable is checked before the conversion.
var (
	minUnixNanoTime = time.Unix(0, math.MinInt64)
	maxUnixNanoTime = time.Unix(0, math.MaxInt64)
)

func representable(t time.Time) bool {
	return !t.Before(minUnixNanoTime) && !t.After(maxUnixNanoTime)
}

// ParseWindow turns the RFC 3339 bounds of a request's window into the Unix
// nanoseconds the compiler takes. A bound that does not parse, or lies
// outside the years nanoseconds can hold, is an *Error with code
// invalid_time_range naming the bound, so every surface refuses a malformed
// window the same way the compiler refuses an empty one.
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
