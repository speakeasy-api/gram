package analytics

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	agentsRepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

// Engine is the query path without its transport, shared by the analytics
// RPC and the Platform MCP: the catalog bound to its loaders and the
// connection plans run on.
type Engine struct {
	catalog *Catalog
	ch      Querier
}

// NewEngine binds the default catalog's lookups to their loaders over db and
// runs plans on ch; a lookup without a loader is a programming error.
func NewEngine(db *pgxpool.Pool, ch Querier) (*Engine, error) {
	catalog, err := Default.WithLoaders(map[string]LookupLoader{
		MCPServerDisplayNamesLookup: mcpServerDisplayNames(hooksRepo.New(db)),
		AgentNamesLookup:            agentNames(agentsRepo.New(db)),
		ProductFamiliesLookup:       productAttribute(aivendors.Product.FamilyID),
		ProductVendorsLookup:        productAttribute(func(p aivendors.Product) string { return p.VendorKey }),
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
	// Plan is a stable diagnostic identifier, not a table name.
	Plan    string
	Columns []Column
	Rows    []Row
}

// Query loads the lookups the request reads, compiles it and runs it. A
// request the compiler rejects comes back as an *Error.
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

// Values lists a dimension's values inside a window, most frequent first,
// read through its lookup like a query. Errors are as for Query.
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

// agentNames loads the organization's agents, id to name.
func agentNames(agents *agentsRepo.Queries) LookupLoader {
	return func(ctx context.Context, tenant Tenant) (map[string]string, error) {
		rows, err := agents.ListAgentNames(ctx, tenant.OrganizationID)
		if err != nil {
			return nil, fmt.Errorf("list agent names: %w", err)
		}
		pairs := make(map[string]string, len(rows))
		for _, row := range rows {
			pairs[row.ID.String()] = row.Name
		}
		return uniqueAgentNames(pairs), nil
	}
}

// productAttribute maps every catalog product id to one of its attributes; the catalog is compiled in, so the tenant is unused.
func productAttribute(attribute func(aivendors.Product) string) LookupLoader {
	products := aivendors.Products()
	values := make(map[string]string, len(products))
	for _, product := range products {
		values[product.ID] = attribute(product)
	}
	return func(context.Context, Tenant) (map[string]string, error) {
		return values, nil
	}
}

// uniqueAgentNames suffixes a shared name with each agent's short id, so reads never merge two agents.
func uniqueAgentNames(names map[string]string) map[string]string {
	shared := make(map[string]int, len(names))
	for _, name := range names {
		shared[name]++
	}
	unique := make(map[string]string, len(names))
	for id, name := range names {
		if shared[name] > 1 {
			name = name + " (" + id[:min(len(id), 8)] + ")"
		}
		unique[id] = name
	}
	return unique
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
