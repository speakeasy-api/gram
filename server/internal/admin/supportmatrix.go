package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
)

// SeedSupportMatrix inserts the matrix's platforms, plans, methods and
// capabilities as rows, so tables that reference them by id have something to
// point at, and retires plans the matrix no longer names. The matrix itself is
// code (supportmatrix.Current) and nothing edits these rows.
func SeedSupportMatrix(ctx context.Context, db *pgxpool.Pool) error {
	matrix, err := supportmatrix.Current()
	if err != nil {
		return fmt.Errorf("load support matrix: %w", err)
	}
	catalog, err := json.Marshal(seedCatalog(matrix))
	if err != nil {
		return fmt.Errorf("encode support matrix seed: %w", err)
	}
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.LockSupportMatrix(ctx); err != nil {
			return fmt.Errorf("lock support catalog: %w", err)
		}
		for _, seed := range []func(context.Context, []byte) error{q.SeedSupportPlatforms, q.SeedSupportPlans, q.RetireSupportPlans, q.SeedSupportMethods, q.SeedSupportCapabilities} {
			if err := seed(ctx, catalog); err != nil {
				return fmt.Errorf("seed support catalog: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed support matrix transaction: %w", err)
	}
	return nil
}

type seedEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Vendor  string `json:"vendor,omitempty"`
	Family  string `json:"family,omitempty"`
	Surface string `json:"surface,omitempty"`
	Plans   string `json:"plans,omitempty"`
	Group   string `json:"group,omitempty"`
}

// seedCatalog is the shape the seed queries read: the axes, in matrix order.
func seedCatalog(matrix *supportmatrix.Matrix) map[string][]seedEntry {
	catalog := map[string][]seedEntry{"products": {}, "plans": {}, "methods": {}, "capabilities": {}}
	for _, p := range matrix.Platforms {
		catalog["products"] = append(catalog["products"], seedEntry{ID: p.ID, Name: p.Name, Vendor: p.Vendor, Family: p.Family, Surface: p.Surface, Plans: "", Group: ""})
	}
	for _, p := range matrix.Plans {
		catalog["plans"] = append(catalog["plans"], seedEntry{ID: p.ID, Name: p.Name, Vendor: p.Vendor, Family: "", Surface: "", Plans: "", Group: ""})
	}
	for _, m := range matrix.Methods {
		catalog["methods"] = append(catalog["methods"], seedEntry{ID: m.ID, Name: m.Name, Vendor: m.Vendor, Family: "", Surface: "", Plans: m.Plans, Group: ""})
	}
	for _, c := range matrix.Capabilities {
		catalog["capabilities"] = append(catalog["capabilities"], seedEntry{ID: c.ID, Name: c.Name, Vendor: "", Family: "", Surface: "", Plans: "", Group: c.Group})
	}
	return catalog
}

// GetSupportMatrix serves the matrix the server was built with.
func (s *Service) GetSupportMatrix(ctx context.Context, _ *gen.GetSupportMatrixPayload) (*gen.SupportMatrix, error) {
	matrix, err := supportmatrix.Current()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load support matrix").LogError(ctx, s.logger)
	}
	return supportMatrixView(matrix), nil
}

func supportMatrixView(matrix *supportmatrix.Matrix) *gen.SupportMatrix {
	view := &gen.SupportMatrix{
		Capabilities: make([]*gen.SupportCapability, 0, len(matrix.Capabilities)),
		Platforms:    make([]*gen.SupportPlatform, 0, len(matrix.Platforms)),
		Methods:      make([]*gen.SupportMethod, 0, len(matrix.Methods)),
		Revision:     matrix.Revision,
	}
	for _, c := range matrix.Capabilities {
		view.Capabilities = append(view.Capabilities, &gen.SupportCapability{ID: c.ID, Name: c.Name, Group: c.Group})
	}
	for _, p := range matrix.Platforms {
		view.Platforms = append(view.Platforms, &gen.SupportPlatform{ID: p.ID, Name: p.Name, Vendor: p.Vendor, Family: p.Family, Surface: p.Surface})
	}
	for i := range matrix.Methods {
		method := &matrix.Methods[i]
		platforms := make([]*gen.SupportPlatformSupport, 0, len(method.Platforms))
		for j := range method.Platforms {
			support := &method.Platforms[j]
			entry := &gen.SupportPlatformSupport{
				Platform:      support.Platform,
				Applicability: string(support.Applicability),
				Accounts: &gen.SupportAccounts{
					Personal:   string(support.Accounts.Personal),
					Team:       string(support.Accounts.Team),
					Enterprise: string(support.Accounts.Enterprise),
				},
				Os:    nil,
				Note:  support.Note,
				Cells: factsView(support.Cells),
			}
			if support.OS != nil {
				entry.Os = &gen.SupportOS{Mac: osView(support.OS.Mac), Windows: osView(support.OS.Windows), Linux: osView(support.OS.Linux)}
			}
			platforms = append(platforms, entry)
		}
		view.Methods = append(view.Methods, &gen.SupportMethod{
			ID: method.ID, Name: method.Name, Vendor: method.Vendor, Plans: method.Plans,
			Claims: factsView(method.Claims), Platforms: platforms,
		})
	}
	return view
}

func factsView(facts map[string]supportmatrix.Fact) map[string]*gen.SupportFact {
	view := make(map[string]*gen.SupportFact, len(facts))
	for id, fact := range facts {
		view[id] = &gen.SupportFact{Status: string(fact.Status), Note: fact.Note, Verify: fact.Verify}
	}
	return view
}

func osView(value supportmatrix.OSSupport) *string {
	if value == "" {
		return nil
	}
	s := string(value)
	return &s
}
