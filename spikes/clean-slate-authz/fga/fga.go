// Package fga runs the same model on an embedded OpenFGA server, so the spike
// can compare decisions (as a differential oracle) and cost.
package fga

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/memory"
	"github.com/openfga/openfga/pkg/storage/migrate"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/authz"
	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fixtures"
)

//go:embed model.fga
var modelDSL string

type Backend struct {
	srv     *server.Server
	ds      storage.OpenFGADatastore
	storeID string
	modelID string

	// Translation cost, reported by the tests.
	GrantTuples   int // tuples that mirror our relationship rows
	DerivedTuples int // extra tuples for facts we keep in domain rows
}

func NewMemory(ctx context.Context) (*Backend, error) { return newBackend(ctx, memory.New()) }

// CachedOptions turns on OpenFGA's check and iterator caches. They serve
// results up to their TTL old: faster, but revocation waits for the TTL.
func CachedOptions(ttl time.Duration) []server.OpenFGAServiceV1Option {
	return []server.OpenFGAServiceV1Option{
		server.WithCheckQueryCacheEnabled(true), server.WithCheckQueryCacheTTL(ttl),
		server.WithCheckIteratorCacheEnabled(true), server.WithCheckIteratorCacheTTL(ttl),
	}
}

// NewPostgres runs OpenFGA's own migrations into uri, then opens its datastore.
func NewPostgres(ctx context.Context, uri string, opts ...server.OpenFGAServiceV1Option) (*Backend, error) {
	if err := migrate.RunMigrations(migrate.MigrationConfig{Engine: "postgres", URI: uri, Timeout: time.Minute, PingTimeout: 10 * time.Second}); err != nil {
		return nil, fmt.Errorf("openfga migrate: %w", err)
	}
	ds, err := postgres.New(uri, sqlcommon.NewConfig())
	if err != nil {
		return nil, err
	}
	return newBackend(ctx, ds, opts...)
}

func newBackend(ctx context.Context, ds storage.OpenFGADatastore, opts ...server.OpenFGAServiceV1Option) (*Backend, error) {
	srv, err := server.NewServerWithOpts(append([]server.OpenFGAServiceV1Option{server.WithDatastore(ds)}, opts...)...)
	if err != nil {
		return nil, err
	}
	st, err := srv.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: "spike"})
	if err != nil {
		return nil, err
	}
	m, err := transformer.TransformDSLToProto(modelDSL)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	wm, err := srv.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId: st.GetId(), TypeDefinitions: m.GetTypeDefinitions(), SchemaVersion: m.GetSchemaVersion(), Conditions: m.GetConditions(),
	})
	if err != nil {
		return nil, fmt.Errorf("write model: %w", err)
	}
	return &Backend{srv: srv, ds: ds, storeID: st.GetId(), modelID: wm.GetAuthorizationModelId()}, nil
}

func (b *Backend) Close() {
	b.srv.Close()
	b.ds.Close()
}

// TupleFor translates one of our relationship rows. It fails for a row with
// both a condition and an expiry: OpenFGA allows one condition per tuple, so
// that combination would need a composite condition per pairing.
func TupleFor(r authz.Relationship) (*openfgav1.TupleKey, error) {
	user := r.Subject.String()
	if r.Relation == "protected" {
		user = "user:*" // a marker becomes "protected from every user"
	}
	tk := &openfgav1.TupleKey{Object: r.Object.String(), Relation: strings.ReplaceAll(r.Relation, ".", "_"), User: user}
	switch {
	case r.ConditionName != "" && r.ExpiresAt != nil:
		return nil, fmt.Errorf("%s: condition plus expiry needs a composite OpenFGA condition", r)
	case r.ConditionName != "":
		var params map[string]any
		if err := json.Unmarshal(r.ConditionParams, &params); err != nil {
			return nil, err
		}
		for _, k := range []string{"tools", "dispositions"} { // CEL needs every parameter present
			if _, ok := params[k]; !ok {
				params[k] = []any{}
			}
		}
		c, err := structpb.NewStruct(params)
		if err != nil {
			return nil, err
		}
		tk.Condition = &openfgav1.RelationshipCondition{Name: r.ConditionName, Context: c}
	case r.ExpiresAt != nil:
		c, _ := structpb.NewStruct(map[string]any{"expires_at": r.ExpiresAt.UTC().Format(time.RFC3339)})
		tk.Condition = &openfgav1.RelationshipCondition{Name: "not_expired", Context: c}
	}
	return tk, nil
}

// Load writes the grant tuples plus the derived tuples OpenFGA needs for facts
// our engine reads from domain rows: parents, ownership, authorship, and the
// reverse group pointer for tupleToUserset.
func (b *Backend) Load(ctx context.Context, rows []authz.Relationship) error {
	var tuples []*openfgav1.TupleKey
	for _, r := range rows {
		tk, err := TupleFor(r)
		if err != nil {
			return err
		}
		tuples = append(tuples, tk)
	}
	b.GrantTuples = len(tuples)

	derived := []*openfgav1.TupleKey{
		{Object: "mcp_server:" + fixtures.Server, Relation: "project", User: "project:" + fixtures.Project},
		{Object: "project:" + fixtures.Project, Relation: "organization", User: "organization:" + fixtures.Org},
	}
	for _, a := range fixtures.Agents {
		derived = append(derived,
			&openfgav1.TupleKey{Object: "agent:" + a.ID, Relation: "owner", User: "user:" + a.OwnerUserID},
			&openfgav1.TupleKey{Object: "agent:" + a.ID, Relation: "project", User: "project:" + a.ProjectID})
	}
	for _, r := range rows {
		switch {
		case r.Object.Type == "organization" && r.Relation == "member" && r.Subject.Type == "user":
			derived = append(derived, &openfgav1.TupleKey{Object: "user:" + r.Subject.ID, Relation: "organization", User: r.Object.String()})
		case r.Object.Type == "group" && r.Relation == "member" && r.Subject.Type == "user":
			derived = append(derived, &openfgav1.TupleKey{Object: "user:" + r.Subject.ID, Relation: "member_group", User: r.Object.String()})
		}
	}
	for _, c := range fixtures.Chats {
		if c.AuthorUserID != "" {
			derived = append(derived, &openfgav1.TupleKey{Object: "chat:" + c.ID, Relation: "author", User: "user:" + c.AuthorUserID})
		}
		if c.AgentID != "" {
			derived = append(derived, &openfgav1.TupleKey{Object: "chat:" + c.ID, Relation: "agent", User: "agent:" + c.AgentID})
		}
	}
	b.DerivedTuples = len(derived)
	return b.write(ctx, append(tuples, derived...), nil)
}

func (b *Backend) write(ctx context.Context, writes []*openfgav1.TupleKey, deletes []*openfgav1.TupleKeyWithoutCondition) error {
	for len(writes) > 0 || len(deletes) > 0 {
		req := &openfgav1.WriteRequest{StoreId: b.storeID, AuthorizationModelId: b.modelID}
		if n := min(len(writes), 100); n > 0 {
			req.Writes = &openfgav1.WriteRequestWrites{TupleKeys: writes[:n]}
			writes = writes[n:]
		}
		if n := min(len(deletes), 100); n > 0 {
			req.Deletes = &openfgav1.WriteRequestDeletes{TupleKeys: deletes[:n]}
			deletes = deletes[n:]
		}
		if _, err := b.srv.Write(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes tuples. A domain change (e.g. leaving a group) can need more
// than one: the grant-side tuple and its derived reverse pointer.
func (b *Backend) Delete(ctx context.Context, keys ...*openfgav1.TupleKeyWithoutCondition) error {
	return b.write(ctx, nil, keys)
}

// Facts is the request context every check must carry: CEL conditions fail on
// missing parameters, so unknown facts are passed as empty values.
type Facts struct {
	Tool, Disposition string
	Now               time.Time
}

func (f Facts) context() *structpb.Struct {
	now := f.Now
	if now.IsZero() {
		now = time.Now()
	}
	c, _ := structpb.NewStruct(map[string]any{"tool": f.Tool, "disposition": f.Disposition, "current_time": now.UTC().Format(time.RFC3339)})
	return c
}

func (b *Backend) Check(ctx context.Context, user, relation, object string, f Facts) (bool, error) {
	res, err := b.srv.Check(ctx, &openfgav1.CheckRequest{
		StoreId: b.storeID, AuthorizationModelId: b.modelID,
		TupleKey: &openfgav1.CheckRequestTupleKey{User: user, Relation: relation, Object: object},
		Context:  f.context(),
	})
	if err != nil {
		return false, err
	}
	return res.GetAllowed(), nil
}

// CheckLayers mirrors our layers: an agent request is one Check per layer.
func (b *Backend) CheckLayers(ctx context.Context, users []string, relation, object string, f Facts) (bool, error) {
	for _, u := range users {
		ok, err := b.Check(ctx, u, relation, object, f)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// BatchCheck checks many objects/facts for one user in one call (tools/list).
func (b *Backend) BatchCheck(ctx context.Context, user, relation, object string, facts []Facts) ([]bool, error) {
	items := make([]*openfgav1.BatchCheckItem, len(facts))
	for i, f := range facts {
		items[i] = &openfgav1.BatchCheckItem{
			TupleKey:      &openfgav1.CheckRequestTupleKey{User: user, Relation: relation, Object: object},
			Context:       f.context(),
			CorrelationId: fmt.Sprintf("c%d", i),
		}
	}
	res, err := b.srv.BatchCheck(ctx, &openfgav1.BatchCheckRequest{StoreId: b.storeID, AuthorizationModelId: b.modelID, Checks: items})
	if err != nil {
		return nil, err
	}
	out := make([]bool, len(facts))
	for i := range facts {
		out[i] = res.GetResult()[fmt.Sprintf("c%d", i)].GetAllowed()
	}
	return out, nil
}

// ListObjects returns object ids the user has relation on — unsorted and capped.
func (b *Backend) ListObjects(ctx context.Context, user, relation, typ string, f Facts) ([]string, error) {
	res, err := b.srv.ListObjects(ctx, &openfgav1.ListObjectsRequest{
		StoreId: b.storeID, AuthorizationModelId: b.modelID, User: user, Relation: relation, Type: typ, Context: f.context(),
	})
	if err != nil {
		return nil, err
	}
	return res.GetObjects(), nil
}
