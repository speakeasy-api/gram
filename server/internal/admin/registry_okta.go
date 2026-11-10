package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/oktamatch"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// observedOktaNames loads the cross-tenant aggregate, leaving out Okta's own
// applications, which every tenant has and no entry should claim.
func (s *Service) observedOktaNames(ctx context.Context) ([]oktamatch.Observed, error) {
	names := make([]string, 0, len(oktaapplications.InternalApplications))
	modes := make([]string, 0, len(oktaapplications.InternalApplications))
	for _, rule := range oktaapplications.InternalApplications {
		names = append(names, rule.Name)
		modes = append(modes, rule.SignOnMode)
	}
	rows, err := repo.New(s.db).AdminListObservedOktaApplicationNames(ctx, repo.AdminListObservedOktaApplicationNamesParams{InternalNames: names, InternalModes: modes})
	if err != nil {
		return nil, fmt.Errorf("list observed okta application names: %w", err)
	}
	observed := make([]oktamatch.Observed, 0, len(rows))
	for _, row := range rows {
		observed = append(observed, oktamatch.Observed{Name: row.Name, Labels: row.Labels, SignOnModes: row.SignOnModes, Organizations: int(row.Organizations)})
	}
	return observed, nil
}

// oktaMappings returns which entry claims each OIN name.
func (s *Service) oktaMappings(ctx context.Context) (map[string]string, error) {
	rows, err := repo.New(s.db).AdminListRegistryOktaMappings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list registry okta mappings: %w", err)
	}
	claimed := make(map[string]string, len(rows))
	for _, row := range rows {
		claimed[row.OinName] = row.EntryName
	}
	return claimed, nil
}

func (s *Service) GetRegistryOktaCandidates(ctx context.Context, p *gen.GetRegistryOktaCandidatesPayload) (*gen.AdminRegistryOktaCandidates, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid registry entry id")
	}
	entry, err := s.registry.Get(ctx, id)
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	observed, err := s.observedOktaNames(ctx)
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	claimed, err := s.oktaMappings(ctx)
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	facts := oktamatch.EntryFromRecord(entry.Data)
	candidates := make([]*gen.AdminRegistryOktaCandidate, 0)
	for _, c := range oktamatch.Match(facts, observed) {
		var mappedBy *string
		if holder, ok := claimed[c.Name]; ok && holder != facts.Name {
			mappedBy = conv.PtrEmpty(holder)
		} else if ok {
			// Already on this entry; nothing to propose.
			continue
		}
		candidates = append(candidates, &gen.AdminRegistryOktaCandidate{
			OinName:       c.Name,
			Organizations: c.Organizations,
			SignOnModes:   c.SignOnModes,
			Reason:        c.Reason,
			Integrator:    oktamatch.Integrator(c.Name),
			MappedBy:      mappedBy,
		})
	}
	return &gen.AdminRegistryOktaCandidates{Candidates: candidates}, nil
}

func (s *Service) ListRegistryOktaUnmapped(ctx context.Context, _ *gen.ListRegistryOktaUnmappedPayload) (*gen.AdminRegistryOktaUnmapped, error) {
	observed, err := s.observedOktaNames(ctx)
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	claimed, err := s.oktaMappings(ctx)
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	entries, err := repo.New(s.db).AdminListRegistryEntryFacts(ctx)
	if err != nil {
		return nil, s.registryError(ctx, fmt.Errorf("list registry entry facts: %w", err))
	}
	type fact struct {
		id    uuid.UUID
		entry oktamatch.Entry
	}
	facts := make([]fact, 0, len(entries))
	for _, e := range entries {
		facts = append(facts, fact{id: e.RegistryEntryID, entry: oktamatch.EntryFromRecord(e.Data)})
	}
	names := make([]*gen.AdminRegistryOktaUnmappedName, 0)
	for _, o := range observed {
		if _, ok := claimed[o.Name]; ok {
			continue
		}
		item := &gen.AdminRegistryOktaUnmappedName{
			OinName:            o.Name,
			Organizations:      o.Organizations,
			SignOnModes:        o.SignOnModes,
			Integrator:         oktamatch.Integrator(o.Name),
			SuggestedEntryID:   nil,
			SuggestedEntryName: nil,
			Reason:             nil,
		}
		// The best entry for this one name: strongest reason wins, ties by
		// catalog order.
		rank := map[string]int{oktamatch.ReasonDomain: 0, oktamatch.ReasonTitle: 1, oktamatch.ReasonLabel: 2}
		best := 3
		for _, f := range facts {
			for _, c := range oktamatch.Match(f.entry, []oktamatch.Observed{o}) {
				if rank[c.Reason] < best {
					best = rank[c.Reason]
					item.SuggestedEntryID = conv.PtrEmpty(f.id.String())
					item.SuggestedEntryName = conv.PtrEmpty(f.entry.Name)
					item.Reason = conv.PtrEmpty(c.Reason)
				}
			}
		}
		names = append(names, item)
	}
	return &gen.AdminRegistryOktaUnmapped{Names: names}, nil
}
