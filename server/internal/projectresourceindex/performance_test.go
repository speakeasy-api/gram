package projectresourceindex

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/project_resource_index"
	"github.com/speakeasy-api/gram/server/internal/projectresourceindex/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// This file is intentionally a synthetic, reproducible construction benchmark.
// It exercises the production adapters (including all seven queryStore calls),
// sorting, compact wire encoding, and gzip. It does not claim to measure a DB.

func syntheticStore(total int) *fakeStore {
	f := &fakeStore{calls: map[string]int{}}
	for i := range total {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1))
		name := fmt.Sprintf("Resource %06d", i)
		slug := fmt.Sprintf("resource-%06d", i)
		switch i % 7 {
		case 0:
			f.assistants = append(f.assistants, repo.ListProjectResourceIndexAssistantsRow{ID: id, Name: name})
		case 1:
			f.environments = append(f.environments, repo.ListProjectResourceIndexEnvironmentsRow{ID: id, Name: name, Slug: slug})
		case 2:
			f.gateways = append(f.gateways, repo.ListProjectResourceIndexGatewaysRow{ID: id, Name: name})
		case 3:
			f.mcp = append(f.mcp, repo.ListProjectResourceIndexMCPServersRow{ID: id, Name: pgtype.Text{String: name, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true}, RouteID: id})
		case 4:
			f.plugins = append(f.plugins, repo.ListProjectResourceIndexPluginsRow{ID: id, Name: name, Slug: slug})
		case 5:
			f.rules = append(f.rules, repo.ListProjectResourceIndexCustomRulesRow{ID: id, Title: name, RuleID: slug, Severity: "medium"})
		case 6:
			f.skills = append(f.skills, repo.ListProjectResourceIndexSkillsRow{ID: id, DisplayName: name, Name: slug})
		}
	}
	return f
}

type wireItem struct {
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	RouteSelector string   `json:"route_selector"`
	Keywords      []string `json:"keywords,omitempty"`
}
type wireKind struct {
	Kind      string `json:"kind"`
	Complete  bool   `json:"complete"`
	Overflow  bool   `json:"overflow"`
	ItemCount int    `json:"item_count"`
}
type wireIndex struct {
	SchemaVersion string      `json:"schema_version"`
	GeneratedAt   string      `json:"generated_at"`
	Items         []*wireItem `json:"items"`
	Kinds         []*wireKind `json:"kinds"`
}

func constructSyntheticIndex(t *testing.T, total int) (*wireIndex, int, time.Duration, error) {
	t.Helper()
	store := syntheticStore(total)
	svc := &Service{
		logger:   testenv.NewLogger(t),
		repo:     store,
		authz:    fakeAuthz{},
		features: fakeFeatures(true),
	}
	serviceStarted := time.Now()
	index, err := svc.Get(testContext(uuid.Nil), &gen.GetPayload{})
	serviceDuration := time.Since(serviceStarted)
	if err != nil {
		return nil, 0, 0, err
	}
	wireItems := make([]*wireItem, 0, len(index.Items))
	for _, item := range index.Items {
		wireItems = append(wireItems, &wireItem{Kind: item.Kind, ID: item.ID, Name: item.Name, RouteSelector: item.RouteSelector, Keywords: item.Keywords})
	}
	kinds := make([]*wireKind, 0, len(index.Kinds))
	for _, kind := range index.Kinds {
		kinds = append(kinds, &wireKind{Kind: kind.Kind, Complete: kind.Complete, Overflow: kind.Overflow, ItemCount: kind.ItemCount})
	}
	calls := 0
	for _, n := range store.calls {
		calls += n
	}
	return &wireIndex{SchemaVersion: index.SchemaVersion, GeneratedAt: "2026-01-01T00:00:00Z", Items: wireItems, Kinds: kinds}, calls, serviceDuration, nil
}

func medianDuration(values []time.Duration) time.Duration {
	slices.Sort(values)
	return values[len(values)/2]
}

//nolint:paralleltest // stable process-wide allocation measurements require isolation.
func TestSyntheticIndexPerformanceEvidence(t *testing.T) {
	if testing.Short() {
		return
	}
	fmt.Println("items\tqueries\tservice_index_ms\tconstruct+json_ms\tjson_bytes\tgzip_bytes\talloc_delta_bytes")
	for _, size := range []int{1_000, 10_000, 50_000} {
		var queryTimes, totalTimes []time.Duration
		var jsonBytes, gzipBytes, allocDelta uint64
		for range 5 {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			started := time.Now()
			index, calls, queryTime, err := constructSyntheticIndex(t, size)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 7 {
				t.Fatalf("got %d store queries, want 7", calls)
			}
			payload, err := json.Marshal(index)
			if err != nil {
				t.Fatal(err)
			}
			var compressed bytes.Buffer
			zw := gzip.NewWriter(&compressed)
			if _, err := zw.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			totalTimes = append(totalTimes, time.Since(started))
			queryTimes = append(queryTimes, queryTime)
			runtime.ReadMemStats(&after)
			jsonBytes, gzipBytes = uint64(len(payload)), uint64(compressed.Len())
			if delta := after.TotalAlloc - before.TotalAlloc; delta > allocDelta {
				allocDelta = delta
			}
		}
		fmt.Printf("%d\t7\t%.3f\t%.3f\t%d\t%d\t%d\n", size, float64(medianDuration(queryTimes))/float64(time.Millisecond), float64(medianDuration(totalTimes))/float64(time.Millisecond), jsonBytes, gzipBytes, allocDelta)
	}
}
