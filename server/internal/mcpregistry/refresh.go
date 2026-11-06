package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

type CatalogRefreshSource interface {
	Fetch(context.Context) (CatalogBundle, error)
}
type CatalogRefreshAssets interface {
	CatalogAssetReader
	CatalogAssetWriter
}
type CatalogRefreshChange struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}
type CatalogRefreshPlan struct {
	ID             uuid.UUID              `json:"id"`
	SourceIdentity string                 `json:"sourceIdentity"`
	SourceRevision string                 `json:"sourceRevision"`
	ExpiresAt      time.Time              `json:"expiresAt"`
	Changes        []CatalogRefreshChange `json:"changes"`
}
type CatalogRefreshReceipt struct {
	SourceRevision string                 `json:"sourceRevision"`
	PlanID         uuid.UUID              `json:"planId"`
	State          string                 `json:"state"`
	Changes        []CatalogRefreshChange `json:"changes"`
	CompletedAt    time.Time              `json:"completedAt"`
}
type CatalogRefreshStatus struct {
	SourceIdentity          string                 `json:"sourceIdentity"`
	LastFullSuccessAt       *time.Time             `json:"lastFullSuccessAt,omitempty"`
	LastFullSuccessRevision string                 `json:"lastFullSuccessRevision,omitempty"`
	LastAttemptRevision     string                 `json:"lastAttemptRevision,omitempty"`
	LastAttemptState        string                 `json:"lastAttemptState,omitempty"`
	Conflicts               []CatalogRefreshChange `json:"conflicts,omitempty"`
	Failure                 string                 `json:"failure,omitempty"`
}
type CatalogRefresher struct {
	registry                    *Service
	environment, sourceIdentity string
	source                      CatalogRefreshSource
	assets                      CatalogRefreshAssets
}
type refreshState struct {
	Baseline                map[string]json.RawMessage `json:"baseline"`
	LastFullSuccessAt       *time.Time                 `json:"lastFullSuccessAt,omitempty"`
	LastFullSuccessRevision string                     `json:"lastFullSuccessRevision,omitempty"`
	LastAttemptRevision     string                     `json:"lastAttemptRevision,omitempty"`
	LastAttemptState        string                     `json:"lastAttemptState,omitempty"`
	Conflicts               []CatalogRefreshChange     `json:"conflicts,omitempty"`
	Absent                  map[string]bool            `json:"absent,omitempty"`
	Failure                 string                     `json:"failure,omitempty"`
}
type refreshStoredPlan struct {
	Public           CatalogRefreshPlan         `json:"public"`
	DestinationToken string                     `json:"destinationToken"`
	BaselineToken    string                     `json:"baselineToken"`
	Prepared         map[string]json.RawMessage `json:"prepared"`
	Upstream         map[string]json.RawMessage `json:"upstream"`
}

func NewCatalogRefresher(registry *Service, environment, sourceIdentity string, source CatalogRefreshSource, images CatalogRefreshAssets) *CatalogRefresher {
	return &CatalogRefresher{registry: registry, environment: environment, sourceIdentity: sourceIdentity, source: source, assets: images}
}
func (r *CatalogRefresher) check() error {
	if r.environment != "local" && r.environment != "dev" {
		return errors.New("catalog refresh destination must be local or dev")
	}
	if r.sourceIdentity == "" || len(r.sourceIdentity) > 256 || r.source == nil || r.registry == nil {
		return errors.New("catalog refresh source is not configured")
	}
	return nil
}
func (r *CatalogRefresher) fetch(ctx context.Context) (CatalogBundle, map[string]json.RawMessage, string, error) {
	b, err := r.source.Fetch(ctx)
	if err != nil {
		return b, nil, "", errors.New("catalog source unavailable")
	}
	if err = r.registry.validateCatalogBundle(ctx, b, assets.ValidateCatalogSVG); err != nil {
		return b, nil, "", errors.New("catalog source snapshot is invalid")
	}
	semantic := map[string]json.RawMessage{}
	for _, data := range b.Entries {
		normalized, err := refreshSemantic(ctx, data, func(_ context.Context, url string) ([]byte, error) {
			image, ok := b.Assets[url]
			if !ok {
				return nil, errors.New("missing catalog asset")
			}
			return image, nil
		})
		if err != nil {
			return b, nil, "", refreshError(err)
		}
		semantic[recordName(data)] = normalized
	}
	return b, semantic, refreshDigest(struct {
		Entries map[string]json.RawMessage
		Assets  map[string]bool
	}{semantic, refreshAssetDigests(b.Assets)}), nil
}
func (r *CatalogRefresher) prepare(ctx context.Context, b CatalogBundle) (map[string]json.RawMessage, error) {
	result := map[string]json.RawMessage{}
	rewritten := map[string]string{}
	for _, data := range b.Entries {
		icons, err := ParseCatalogIcons(data)
		if err != nil {
			return nil, refreshError(err)
		}
		for _, icon := range []*CatalogIcon{icons.Default, icons.Dark} {
			if icon == nil {
				continue
			}
			url, ok := rewritten[icon.Src]
			if !ok {
				if r.assets == nil {
					return nil, errors.New("catalog asset storage unavailable")
				}
				url, err = r.assets.StoreCatalogAsset(ctx, b.Assets[icon.Src])
				if err != nil {
					return nil, errors.New("catalog asset upload failed")
				}
				// Public platform storage canonicalizes SVGs. A trusted export must
				// already contain those canonical bytes; never establish a false baseline.
				stored, readErr := r.assets.ReadCatalogAsset(ctx, url)
				if readErr != nil || refreshDigest(stored) != refreshDigest(b.Assets[icon.Src]) {
					return nil, errors.New("catalog asset content changed during storage")
				}
				rewritten[icon.Src] = url
			}
			icon.Src = url
		}
		data, err = WithCatalogIcons(data, icons)
		if err != nil {
			return nil, refreshError(err)
		}
		data, err = withoutPublicationMetadata(data)
		if err != nil {
			return nil, refreshError(err)
		}
		result[recordName(data)] = data
	}
	return result, nil
}
func refreshSnapshot(ctx context.Context, tx pgx.Tx) (map[string]Entry, string, error) {
	sizeInfo, err := repo.New(tx).RefreshCatalogSize(ctx)
	if err != nil {
		return nil, "", refreshError(err)
	}
	if sizeInfo.EntryCount > 10000 || sizeInfo.DataBytes > CatalogBundleByteLimit {
		return nil, "", errors.New("destination catalog exceeds refresh limits")
	}
	rows, err := repo.New(tx).RefreshCatalogSnapshot(ctx)
	if err != nil {
		return nil, "", refreshError(err)
	}
	if len(rows) > 10000 {
		return nil, "", errors.New("destination catalog exceeds refresh limits")
	}
	entries := map[string]Entry{}
	size := 0
	for _, row := range rows {
		size += len(row.Data)
		if size > CatalogBundleByteLimit {
			return nil, "", errors.New("destination catalog exceeds refresh limits")
		}
		e, err := entry(row, nil)
		if err != nil {
			return nil, "", refreshError(err)
		}
		entries[recordName(e.Data)] = e
	}
	return entries, refreshDigest(entries), nil
}
func refreshReadState(ctx context.Context, tx pgx.Tx, source string) (refreshState, string, error) {
	raw, err := refreshLoadState(ctx, tx, source)
	if err != nil {
		return refreshState{}, "", refreshError(err)
	}
	var state refreshState
	if err = json.Unmarshal(raw, &state); err != nil {
		return state, "", refreshError(err)
	}
	if state.Baseline == nil {
		state.Baseline = map[string]json.RawMessage{}
	}
	if state.Absent == nil {
		state.Absent = map[string]bool{}
	}
	return state, refreshDigest(struct {
		Baseline map[string]json.RawMessage
		Absent   map[string]bool
	}{state.Baseline, state.Absent}), nil
}
func (r *CatalogRefresher) readDestination(ctx context.Context) (map[string]Entry, string, refreshState, string, error) {
	tx, err := r.registry.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, "", refreshState{}, "", refreshError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	entries, token, err := refreshSnapshot(ctx, tx)
	if err != nil {
		return nil, "", refreshState{}, "", refreshError(err)
	}
	state, baselineToken, err := refreshReadState(ctx, tx, r.sourceIdentity)
	return entries, token, state, baselineToken, refreshError(err)
}
func (r *CatalogRefresher) Preview(ctx context.Context, owner string) (CatalogRefreshPlan, error) {
	return r.preview(ctx, owner, false)
}
func (r *CatalogRefresher) preview(ctx context.Context, owner string, additive bool) (CatalogRefreshPlan, error) {
	if err := r.check(); err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	if owner == "" || len(owner) > 1024 {
		return CatalogRefreshPlan{}, errors.New("catalog refresh staff owner required")
	}
	b, upstream, revision, err := r.fetch(ctx)
	if err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	entries, destinationToken, state, baselineToken, err := r.readDestination(ctx)
	if err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	local := map[string]json.RawMessage{}
	for name, e := range entries {
		data, err := refreshSemantic(ctx, e.Data, func(ctx context.Context, url string) ([]byte, error) {
			if r.assets == nil {
				return nil, errors.New("catalog asset reader unavailable")
			}
			return r.assets.ReadCatalogAsset(ctx, url)
		})
		if err != nil {
			return CatalogRefreshPlan{}, errors.New("destination catalog assets unavailable")
		}
		local[name] = data
	}
	prepared := map[string]json.RawMessage{}
	for _, raw := range b.Entries {
		data, err := withoutPublicationMetadata(raw)
		if err != nil {
			return CatalogRefreshPlan{}, refreshError(err)
		}
		prepared[recordName(data)] = data
	}
	// Preview checks record mutations without uploading blobs. Icon content was
	// validated in the full source snapshot; destination IDs are assigned only
	// during Apply preflight, outside all write locks.
	trialPrepared := map[string]json.RawMessage{}
	for name, raw := range prepared {
		data, err := WithCatalogIcons(raw, CatalogIcons{Default: nil, Dark: nil})
		if err != nil {
			return CatalogRefreshPlan{}, refreshError(err)
		}
		trialPrepared[name] = data
	}
	plan := refreshStoredPlan{Public: CatalogRefreshPlan{ID: uuid.New(), SourceIdentity: r.sourceIdentity, SourceRevision: revision, ExpiresAt: time.Now().UTC().Add(15 * time.Minute), Changes: []CatalogRefreshChange{}}, DestinationToken: destinationToken, BaselineToken: baselineToken, Prepared: prepared, Upstream: upstream}
	names := refreshNames(upstream, state.Baseline, local)
	if additive {
		names = refreshNames(upstream)
	}
	for _, name := range names {
		action, reason := refreshDecision(state.Baseline[name], local[name], upstream[name], entries[name].Published)
		if state.Absent[name] {
			action, reason = "preserved", "upstream_removed"
			if len(upstream[name]) > 0 {
				action, reason = "conflict", "locally_modified"
				if len(local[name]) > 0 && !entries[name].Published && equalJSON(local[name], state.Baseline[name]) {
					if checkStructure(local[name], upstream[name]) == nil {
						action, reason = "republish", "upstream_returned"
					} else {
						reason = "immutable_structure"
					}
				}
			}
		}
		if additive && len(local[name]) > 0 {
			action, reason = "conflict", "existing_entry"
		}
		plan.Public.Changes = append(plan.Public.Changes, CatalogRefreshChange{Name: name, Action: action, Reason: reason})
	}
	// Validate the exact safe subset with the same mutation path, rolling back all
	// trial writes. The final plan is then persisted in a clean transaction.
	tx, err := r.registry.beginCatalogRefresh(ctx)
	if err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = r.verify(ctx, tx, plan); err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	trial, err := tx.Begin(ctx)
	if err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	for idx, change := range plan.Public.Changes {
		if change.Action == "conflict" || change.Action == "adopt" || change.Action == "preserved" {
			continue
		}
		savepoint, err := trial.Begin(ctx)
		if err != nil {
			return CatalogRefreshPlan{}, refreshError(err)
		}
		err = r.writeChange(ctx, savepoint, change, trialPrepared, entries)
		if err != nil {
			_ = savepoint.Rollback(ctx)
			if !refreshExpectedConflict(err) {
				return CatalogRefreshPlan{}, refreshError(err)
			}
			plan.Public.Changes[idx].Action = "conflict"
			plan.Public.Changes[idx].Reason = "validation_conflict"
		} else if err = savepoint.Commit(ctx); err != nil {
			return CatalogRefreshPlan{}, refreshError(err)
		}
	}
	if err = trial.Rollback(ctx); err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	if len(raw) > CatalogBundleByteLimit {
		return CatalogRefreshPlan{}, errors.New("catalog refresh plan exceeds size limit")
	}
	if err = refreshSavePlan(ctx, tx, plan.Public.ID, r.sourceIdentity, owner, plan.Public.ExpiresAt, raw); err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CatalogRefreshPlan{}, refreshError(err)
	}
	return plan.Public, nil
}
func refreshExpectedConflict(err error) bool {
	var invalid *InvalidError
	var pgerr *pgconn.PgError
	return errors.Is(err, ErrConflict) || errors.Is(err, ErrEndpointStructureImmutable) || errors.As(err, &invalid) || (errors.As(err, &pgerr) && pgerr.Code == pgerrcode.UniqueViolation)
}
func (r *CatalogRefresher) verify(ctx context.Context, tx pgx.Tx, plan refreshStoredPlan) error {
	_, token, err := refreshSnapshot(ctx, tx)
	if err != nil {
		return refreshError(err)
	}
	_, baselineToken, err := refreshReadState(ctx, tx, r.sourceIdentity)
	if err != nil {
		return refreshError(err)
	}
	if token != plan.DestinationToken || baselineToken != plan.BaselineToken {
		return fmt.Errorf("%w: catalog changed; create a new preview", ErrConflict)
	}
	return nil
}
func (r *CatalogRefresher) writeChange(ctx context.Context, tx pgx.Tx, change CatalogRefreshChange, prepared map[string]json.RawMessage, entries map[string]Entry) error {
	var err error
	switch change.Action {
	case "create":
		_, err = r.registry.CreateTx(ctx, tx, prepared[change.Name])
	case "update":
		e := entries[change.Name]
		_, err = r.registry.mutateTx(ctx, tx, e.ID, Token(e), prepared[change.Name], nil)
	case "republish":
		e := entries[change.Name]
		updated, updateErr := r.registry.mutateTx(ctx, tx, e.ID, Token(e), prepared[change.Name], nil)
		if updateErr != nil {
			return updateErr
		}
		published := true
		_, err = r.registry.mutateTx(ctx, tx, e.ID, Token(updated), nil, &published)
	case "unpublish":
		e := entries[change.Name]
		published := false
		_, err = r.registry.mutateTx(ctx, tx, e.ID, Token(e), nil, &published)
	}
	return refreshError(err)
}
func (r *CatalogRefresher) loadPlan(ctx context.Context, tx pgx.Tx, owner string, id uuid.UUID) (refreshStoredPlan, *CatalogRefreshReceipt, error) {
	data, rawReceipt, expires, err := refreshLoadPlan(ctx, tx, id, r.sourceIdentity, owner)
	if err != nil {
		return refreshStoredPlan{}, nil, refreshError(err)
	}
	if len(rawReceipt) > 0 {
		var receipt CatalogRefreshReceipt
		if err = json.Unmarshal(rawReceipt, &receipt); err != nil {
			return refreshStoredPlan{}, nil, refreshError(err)
		}
		var empty refreshStoredPlan
		return empty, &receipt, nil
	}
	if !time.Now().Before(expires) {
		return refreshStoredPlan{}, nil, fmt.Errorf("%w: catalog refresh plan expired; create a new preview", ErrConflict)
	}
	var plan refreshStoredPlan
	err = json.Unmarshal(data, &plan)
	return plan, nil, refreshError(err)
}
func (r *CatalogRefresher) Apply(ctx context.Context, owner string, id uuid.UUID) (out CatalogRefreshReceipt, applyErr error) {
	if err := r.check(); err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	// A committed receipt is authoritative even if the source is now unavailable
	// or changed. Ownership and configured source are checked before replay.
	readTx, err := r.registry.db.Begin(ctx)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	plan, receipt, err := r.loadPlan(ctx, readTx, owner, id)
	_ = readTx.Rollback(ctx)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	if receipt != nil {
		return *receipt, nil
	}
	defer func() {
		if applyErr != nil {
			r.recordFailure(ctx, plan.Public.SourceRevision, applyErr)
		}
	}()
	bundle, _, revision, err := r.fetch(ctx)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	if revision != plan.Public.SourceRevision {
		return CatalogRefreshReceipt{}, fmt.Errorf("%w: catalog source changed; create a new preview", ErrConflict)
	}
	// Only safe writes need asset preparation. Fetch/validation covers the full
	// snapshot, but conflicts and preserved entries never upload assets.
	selected := map[string]bool{}
	for _, change := range plan.Public.Changes {
		if change.Action == "create" || change.Action == "update" || change.Action == "republish" {
			selected[change.Name] = true
		}
	}
	selectedBundle := bundle
	selectedBundle.Entries = nil
	selectedBundle.Assets = map[string][]byte{}
	content := map[string][]byte{}
	for _, image := range bundle.Assets {
		content[refreshDigest(image)] = image
	}
	for _, change := range plan.Public.Changes {
		if selected[change.Name] {
			raw := plan.Prepared[change.Name]
			icons, parseErr := ParseCatalogIcons(raw)
			if parseErr != nil {
				return CatalogRefreshReceipt{}, refreshError(parseErr)
			}
			semanticIcons, parseErr := ParseCatalogIcons(plan.Upstream[change.Name])
			if parseErr != nil {
				return CatalogRefreshReceipt{}, refreshError(parseErr)
			}
			for _, pair := range [][2]*CatalogIcon{{icons.Default, semanticIcons.Default}, {icons.Dark, semanticIcons.Dark}} {
				if pair[0] == nil {
					continue
				}
				if pair[1] == nil {
					return CatalogRefreshReceipt{}, errors.New("catalog refresh asset identity missing")
				}
				image, ok := content[pair[1].Src]
				if !ok {
					return CatalogRefreshReceipt{}, errors.New("catalog refresh asset content missing")
				}
				selectedBundle.Assets[pair[0].Src] = image
			}
			selectedBundle.Entries = append(selectedBundle.Entries, raw)
		}
	}
	prepared, err := r.prepare(ctx, selectedBundle)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	tx, err := r.registry.beginCatalogRefresh(ctx)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plan, receipt, err = r.loadPlan(ctx, tx, owner, id)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	if receipt != nil {
		return *receipt, nil
	}
	if err = r.verify(ctx, tx, plan); err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	entries, _, err := refreshSnapshot(ctx, tx)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	state, _, err := refreshReadState(ctx, tx, r.sourceIdentity)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	result := CatalogRefreshReceipt{SourceRevision: plan.Public.SourceRevision, PlanID: id, State: "completed", Changes: plan.Public.Changes, CompletedAt: time.Now().UTC()}
	for _, change := range plan.Public.Changes {
		if change.Action == "preserved" {
			continue
		}
		if change.Action == "conflict" {
			result.State = "completed_with_conflicts"
			continue
		}
		if err = r.writeChange(ctx, tx, change, prepared, entries); err != nil {
			return CatalogRefreshReceipt{}, refreshError(err)
		}
		if data, ok := plan.Upstream[change.Name]; ok {
			state.Baseline[change.Name] = data
			delete(state.Absent, change.Name)
		} else {
			state.Absent[change.Name] = true
		}
	}
	if result.State == "completed" {
		state.LastFullSuccessAt = &result.CompletedAt
		state.LastFullSuccessRevision = plan.Public.SourceRevision
	}
	state.LastAttemptRevision = plan.Public.SourceRevision
	state.LastAttemptState = result.State
	state.Failure = ""
	state.Conflicts = nil
	for _, change := range result.Changes {
		if change.Action == "conflict" && len(state.Conflicts) < 100 {
			state.Conflicts = append(state.Conflicts, change)
		}
	}
	if len(state.Baseline) > 10000 {
		return CatalogRefreshReceipt{}, errors.New("catalog refresh baseline exceeds entry limit")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	if err = refreshSaveState(ctx, tx, r.sourceIdentity, raw); err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	if err = refreshSaveReceipt(ctx, tx, id, raw); err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CatalogRefreshReceipt{}, refreshError(err)
	}
	return result, nil
}
func (r *CatalogRefresher) Status(ctx context.Context) (CatalogRefreshStatus, error) {
	if err := r.check(); err != nil {
		return CatalogRefreshStatus{}, refreshError(err)
	}
	tx, err := r.registry.db.Begin(ctx)
	if err != nil {
		return CatalogRefreshStatus{}, refreshError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, _, err := refreshReadState(ctx, tx, r.sourceIdentity)
	return CatalogRefreshStatus{SourceIdentity: r.sourceIdentity, LastFullSuccessAt: state.LastFullSuccessAt, LastFullSuccessRevision: state.LastFullSuccessRevision, LastAttemptRevision: state.LastAttemptRevision, LastAttemptState: state.LastAttemptState, Conflicts: state.Conflicts, Failure: state.Failure}, refreshError(err)
}

// Import is the trusted additive bootstrap. Legacy caller-provided bundles keep
// their original importer semantics and cannot establish refresh ownership.
func (r *CatalogRefresher) Import(ctx context.Context, owner string) (CatalogImportResult, error) {
	plan, err := r.preview(ctx, owner, true)
	if err != nil {
		return CatalogImportResult{}, refreshError(err)
	}
	receipt, err := r.Apply(ctx, owner, plan.ID)
	if err != nil {
		return CatalogImportResult{}, refreshError(err)
	}
	result := CatalogImportResult{Created: []string{}, Conflicts: []string{}}
	for _, c := range receipt.Changes {
		switch c.Action {
		case "create":
			result.Created = append(result.Created, c.Name)
		case "conflict":
			result.Conflicts = append(result.Conflicts, c.Name)
		}
	}
	return result, nil
}

// A writer can already hold a row or table lock when it requests the advisory
// mapping lock. Never wait for that writer while retaining our advisory lock.
func (s *Service) beginCatalogRefresh(ctx context.Context) (pgx.Tx, error) {
	for {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, refreshError(err)
		}
		if err = repo.New(tx).LockOktaMappings(ctx); err != nil {
			_ = tx.Rollback(ctx)
			return nil, refreshError(err)
		}
		err = repo.New(tx).LockCatalogRefresh(ctx)
		if err == nil {
			return tx, nil
		}
		_ = tx.Rollback(ctx)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != pgerrcode.LockNotAvailable {
			return nil, refreshError(err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, refreshError(ctx.Err())
		case <-timer.C:
		}
	}
}

// Failure diagnostics are bounded categories, never source responses or payloads.
// Recording them does not advance either the baseline or full-success marker.
func (r *CatalogRefresher) recordFailure(ctx context.Context, revision string, cause error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	tx, err := r.registry.beginCatalogRefresh(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, _, err := refreshReadState(ctx, tx, r.sourceIdentity)
	if err != nil {
		return
	}
	state.LastAttemptRevision = revision
	state.LastAttemptState = "failed"
	state.Failure = "refresh_failed"
	if errors.Is(cause, ErrConflict) {
		state.Failure = "stale_plan"
	}
	state.Conflicts = nil
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	if refreshSaveState(ctx, tx, r.sourceIdentity, raw) != nil {
		return
	}
	_ = tx.Commit(ctx)
}

func refreshError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("catalog refresh: %w", err)
}

// URL churn does not change a verified snapshot: icons identify their bytes.
func refreshAssetDigests(images map[string][]byte) map[string]bool {
	digests := make(map[string]bool, len(images))
	for _, image := range images {
		digests[refreshDigest(image)] = true
	}
	return digests
}
