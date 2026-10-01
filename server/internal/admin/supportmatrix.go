package admin

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
)

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
