package activities

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	funcrepo "github.com/speakeasy-api/gram/server/internal/functions/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AutoSyncToolsetsRequest is the input to the AutoSyncToolsets activity. It
// runs once per completed deployment, against the toolsets whose
// auto_sync_sources subscription overlaps the deployment's function sources.
type AutoSyncToolsetsRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
}

// AutoSyncToolsetsResult reports per-toolset summary information for
// observability and tests. Empty fields when no toolsets were touched.
type AutoSyncToolsetsResult struct {
	ToolsetsExtended int
	AddedByToolset   map[string][]string // toolset slug -> URNs added
}

// AutoSyncToolsets is a Temporal activity. It diffs the function tool URNs
// introduced by a deployment against each toolset subscribed (via
// auto_sync_sources) to one of the deployment's function sources, and
// extends those toolsets with the missing URNs by appending a new
// toolset_versions row. Never removes URNs.
//
// Idempotent: a replay against the same deployment becomes a no-op because
// the second pass finds no new URNs to add.
type AutoSyncToolsets struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	audit  *audit.Logger
}

func NewAutoSyncToolsets(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger) *AutoSyncToolsets {
	return &AutoSyncToolsets{
		logger: logger.With(attr.SlogComponent("auto_sync_toolsets")),
		db:     db,
		audit:  auditLogger,
	}
}

// systemActor identifies system-driven audit events. Mirrors the convention
// established in background/triggers/app.go where Temporal-fired writes use
// "user:system" as the principal.
var systemActor = urn.NewPrincipal(urn.PrincipalTypeUser, "system")

func (a *AutoSyncToolsets) Do(ctx context.Context, req AutoSyncToolsetsRequest) (*AutoSyncToolsetsResult, error) {
	logger := a.logger.With(
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogDeploymentID(req.DeploymentID.String()),
	)

	funcRepo := funcrepo.New(a.db)
	urnRows, err := funcRepo.ListFunctionToolURNsByDeployment(ctx, req.DeploymentID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list function tool URNs for deployment").Log(ctx, logger)
	}

	if len(urnRows) == 0 {
		logger.DebugContext(ctx, "no function tools in deployment; auto-sync is a no-op")
		return &AutoSyncToolsetsResult{}, nil
	}

	// Bucket URNs by their function-source slug. The prefixed subscription
	// keys ("function:<slug>") will then drive the toolset lookup.
	urnsBySource := make(map[string][]string)
	for _, row := range urnRows {
		urnsBySource[row.FunctionSlug] = append(urnsBySource[row.FunctionSlug], row.ToolUrn.String())
	}

	subscriptionKeys := make([]string, 0, len(urnsBySource))
	for slug := range urnsBySource {
		subscriptionKeys = append(subscriptionKeys, fmt.Sprintf("%s:%s", urn.ToolKindFunction, slug))
	}
	sort.Strings(subscriptionKeys) // deterministic ordering for tests/logs

	tsRepo := toolsetsRepo.New(a.db)
	candidates, err := tsRepo.ListToolsetsByAutoSyncSource(ctx, toolsetsRepo.ListToolsetsByAutoSyncSourceParams{
		ProjectID: req.ProjectID,
		Sources:   subscriptionKeys,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list subscribed toolsets").Log(ctx, logger)
	}

	result := &AutoSyncToolsetsResult{
		AddedByToolset: make(map[string][]string),
	}

	for _, ts := range candidates {
		added, err := a.extendOne(ctx, logger, ts, urnsBySource, req.DeploymentID)
		if err != nil {
			// One bad toolset doesn't fail the others. The Temporal retry
			// machinery will pick the activity up again if all errored.
			logger.ErrorContext(ctx, "failed to extend toolset", attr.SlogError(err), attr.SlogToolsetID(ts.ID.String()))
			continue
		}
		if len(added) > 0 {
			result.AddedByToolset[ts.Slug] = added
			result.ToolsetsExtended++
		}
	}

	return result, nil
}

// extendOne handles a single toolset under its own transaction. The toolset
// row is locked FOR UPDATE so a concurrent run from another deployment
// completion can't compute a stale "latest version" and collide on the
// unique (toolset_id, version) constraint.
func (a *AutoSyncToolsets) extendOne(
	ctx context.Context,
	logger *slog.Logger,
	ts toolsetsRepo.Toolset,
	urnsBySource map[string][]string,
	deploymentID uuid.UUID,
) ([]string, error) {
	tsLogger := logger.With(
		attr.SlogToolsetID(ts.ID.String()),
		attr.SlogToolsetSlug(ts.Slug),
	)

	// Build the set of URNs the subscription would pull in. Only consider
	// sources the toolset is actually subscribed to (not every source in the
	// deployment).
	subscribed := make(map[string]struct{}, len(ts.AutoSyncSources))
	for _, entry := range ts.AutoSyncSources {
		subscribed[entry] = struct{}{}
	}

	desired := make(map[string]struct{})
	for slug, urns := range urnsBySource {
		key := fmt.Sprintf("%s:%s", urn.ToolKindFunction, slug)
		if _, ok := subscribed[key]; !ok {
			continue
		}
		for _, u := range urns {
			desired[u] = struct{}{}
		}
	}
	if len(desired) == 0 {
		return nil, nil
	}

	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txTsRepo := toolsetsRepo.New(tx)

	if _, err := txTsRepo.LockToolsetForAutoSync(ctx, ts.ID); err != nil {
		return nil, fmt.Errorf("lock toolset: %w", err)
	}

	latest, err := txTsRepo.GetLatestToolsetVersion(ctx, ts.ID)
	var (
		existing      []urn.Tool
		resourceUrns  = []urn.Resource{} // NOT NULL on toolset_versions; nil would fail the insert
		predecessorID uuid.NullUUID
		nextVersion   int64 = 1
	)
	switch {
	case err == nil:
		existing = latest.ToolUrns
		resourceUrns = latest.ResourceUrns
		predecessorID = uuid.NullUUID{UUID: latest.ID, Valid: true}
		nextVersion = latest.Version + 1
	case err == pgx.ErrNoRows:
		// First version for this toolset.
	default:
		return nil, fmt.Errorf("read latest toolset version: %w", err)
	}

	presence := make(map[string]struct{}, len(existing))
	for _, u := range existing {
		presence[u.String()] = struct{}{}
	}

	added := make([]string, 0, len(desired))
	for u := range desired {
		if _, already := presence[u]; already {
			continue
		}
		added = append(added, u)
	}
	if len(added) == 0 {
		return nil, nil
	}
	sort.Strings(added)

	merged := make([]urn.Tool, 0, len(existing)+len(added))
	merged = append(merged, existing...)
	for _, s := range added {
		parsed, parseErr := urn.ParseTool(s)
		if parseErr != nil {
			return nil, fmt.Errorf("parse tool urn %q: %w", s, parseErr)
		}
		merged = append(merged, parsed)
	}

	created, err := txTsRepo.CreateToolsetVersion(ctx, toolsetsRepo.CreateToolsetVersionParams{
		ToolsetID:     ts.ID,
		Version:       nextVersion,
		ToolUrns:      merged,
		ResourceUrns:  resourceUrns,
		PredecessorID: predecessorID,
	})
	if err != nil {
		return nil, fmt.Errorf("create toolset version: %w", err)
	}

	// Subscription keys responsible for these additions.
	contributingSources := make([]string, 0)
	for _, key := range sortedKeys(subscribed) {
		slug := key[len(string(urn.ToolKindFunction))+1:]
		if _, ok := urnsBySource[slug]; ok {
			contributingSources = append(contributingSources, key)
		}
	}

	tsURN := urn.NewToolset(ts.ID)
	if err := a.audit.LogToolsetToolsAutoAdded(ctx, tx, audit.LogToolsetToolsAutoAddedEvent{
		OrganizationID:      ts.OrganizationID,
		ProjectID:           ts.ProjectID,
		Actor:               systemActor,
		ToolsetURN:          tsURN,
		ToolsetName:         ts.Name,
		ToolsetSlug:         ts.Slug,
		ToolsetVersionAfter: created.Version,
		DeploymentID:        deploymentID,
		Sources:             contributingSources,
		AddedURNs:           added,
	}); err != nil {
		return nil, fmt.Errorf("emit audit event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	tsLogger.InfoContext(ctx, "auto-sync extended toolset",
		slog.Int("added_count", len(added)),
		slog.Int64("toolset_version", created.Version),
	)
	return added, nil
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
