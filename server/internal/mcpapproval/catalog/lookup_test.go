package catalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

type lookupProjects struct {
	id  uuid.UUID
	err error
}

func (p *lookupProjects) GetProjectByID(_ context.Context, id uuid.UUID) (projectrepo.Project, error) {
	p.id = id
	return projectrepo.Project{OrganizationID: "trusted-org"}, p.err
}

type lookupOrganizations struct{ id string }

func (o *lookupOrganizations) GetOrganizationMetadata(_ context.Context, id string) (orgrepo.OrganizationMetadatum, error) {
	o.id = id
	return orgrepo.OrganizationMetadatum{Slug: "trusted-slug"}, nil
}

type lookupCatalog struct {
	org, slug string
	selected  externalmcp.CatalogSource
	reader    externalmcp.RegistryReader
	err       error
}

func (c *lookupCatalog) SelectedSource(_ context.Context, org, slug string) (externalmcp.CatalogSource, error) {
	c.org = org
	c.slug = slug
	return c.selected, c.err
}
func (c *lookupCatalog) ReaderFor(s externalmcp.CatalogSource) (externalmcp.RegistryReader, error) {
	if s.Registry.ID != c.selected.Registry.ID {
		panic("unselected source")
	}
	return c.reader, nil
}

type lookupReader struct {
	calls     []uuid.UUID
	err       error
	result    externalmcp.ListServersResult
	details   *externalmcp.ServerDetails
	detailErr error
}

func (r *lookupReader) ListServers(_ context.Context, s externalmcp.Registry, _ externalmcp.ListServersParams) (externalmcp.ListServersResult, error) {
	r.calls = append(r.calls, s.ID)
	return r.result, r.err
}
func (r *lookupReader) GetServerDetails(_ context.Context, _ externalmcp.Registry, _ string, _ []string) (*externalmcp.ServerDetails, error) {
	return r.details, r.detailErr
}
func TestLookupSelectedSourceAndTrustedOrganization(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[native], func(t *testing.T) {
			selected := uuid.New()
			if native {
				selected = externalmcp.NativeCatalogRegistryID
			}
			reader := &lookupReader{}
			catalogs := &lookupCatalog{selected: externalmcp.CatalogSource{Registry: externalmcp.Registry{ID: selected}}, reader: reader}
			projects := &lookupProjects{}
			orgs := &lookupOrganizations{}
			source := &Source{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), projects: projects, organizations: orgs, catalog: catalogs}
			projectID := uuid.New()
			match, err := source.Lookup(t.Context(), projectID, "https://example.com/mcp", true)
			require.NoError(t, err)
			require.Nil(t, match)
			require.Equal(t, projectID, projects.id)
			require.Equal(t, "trusted-org", orgs.id)
			require.Equal(t, "trusted-org", catalogs.org)
			require.Equal(t, "trusted-slug", catalogs.slug)
			require.Equal(t, []uuid.UUID{selected}, reader.calls)
			reader.err = errors.New("selected read failed")
			_, err = source.Lookup(t.Context(), projectID, "https://example.com/mcp", true)
			require.ErrorContains(t, err, "selected read failed")
			require.Len(t, reader.calls, 2)
			projects.err = errors.New("project unavailable")
			_, err = source.Lookup(t.Context(), projectID, "https://example.com/mcp", true)
			require.ErrorContains(t, err, "project unavailable")
			require.Len(t, reader.calls, 2)
		})
	}
}

func TestLookupMatchKeepsProvenanceAndToolPresence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		raw          string
		detailErr    error
		includeTools bool
		wantNil      bool
	}{
		{"unknown", `{}`, nil, true, true},
		{"empty", `{"Tools":[]}`, nil, true, false},
		{"detail failure", `{}`, errors.New("unavailable"), true, true},
		{"provenance only", `{}`, errors.New("must not fetch"), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &lookupReader{details: details(t, tc.raw), detailErr: tc.detailErr, result: externalmcp.ListServersResult{Servers: []*types.ExternalMCPServerEntry{
				{RegistrySpecifier: "wrong", Remotes: []*types.ExternalMCPRemote{{URL: "https://example.com/other"}}},
				{RegistrySpecifier: "matched", Meta: map[string]any{"com.pulsemcp/server": map[string]any{"isOfficial": true}}, Remotes: []*types.ExternalMCPRemote{{URL: "https://example.com/mcp"}}},
			}}}
			catalogs := &lookupCatalog{selected: externalmcp.CatalogSource{Registry: externalmcp.Registry{ID: uuid.New()}, SourceKey: "selected-key", Name: "Selected display"}, reader: reader}
			source := &Source{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), projects: &lookupProjects{}, organizations: &lookupOrganizations{}, catalog: catalogs}
			match, err := source.Lookup(t.Context(), uuid.New(), "https://example.com/mcp", tc.includeTools)
			require.NoError(t, err)
			require.NotNil(t, match)
			require.Equal(t, "matched", match.Specifier)
			require.Equal(t, "Selected display", match.Registry)
			require.True(t, match.Provenance.Official)
			if tc.wantNil {
				require.Nil(t, match.Tools)
			} else {
				require.NotNil(t, match.Tools)
				require.Empty(t, match.Tools)
			}
			catalogs.err = errors.New("selection unavailable")
			_, err = source.Lookup(t.Context(), uuid.New(), "https://example.com/mcp", true)
			require.ErrorContains(t, err, "selection unavailable")
			require.Len(t, reader.calls, 1)
			_, err = source.Lookup(t.Context(), uuid.Nil, "https://example.com/mcp", true)
			require.ErrorContains(t, err, "requires a research project")
			require.Len(t, reader.calls, 1)
		})
	}
}

// The selected native reader can supply retained evidence without listing it publicly.
type retainedLookupReader struct {
	lookupReader
	retainedCalls int
}

func (r *retainedLookupReader) ListEvidenceServers(_ context.Context, source externalmcp.Registry) (externalmcp.ListServersResult, error) {
	r.retainedCalls++
	return r.result, nil
}
func TestLookupUsesSelectedRetainedEvidenceReader(t *testing.T) {
	reader := &retainedLookupReader{lookupReader: lookupReader{result: externalmcp.ListServersResult{Servers: []*types.ExternalMCPServerEntry{{RegistrySpecifier: "retained", Remotes: []*types.ExternalMCPRemote{{URL: "https://example.com/mcp"}}}}}}}
	catalogs := &lookupCatalog{selected: externalmcp.CatalogSource{Registry: externalmcp.Registry{ID: externalmcp.NativeCatalogRegistryID}, Name: "Speakeasy", SourceKey: "speakeasy"}, reader: reader}
	source := &Source{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), projects: &lookupProjects{}, organizations: &lookupOrganizations{}, catalog: catalogs}
	match, err := source.Lookup(t.Context(), uuid.New(), "https://example.com/mcp", false)
	require.NoError(t, err)
	require.Equal(t, "retained", match.Specifier)
	require.Equal(t, "Speakeasy", match.Registry)
	require.Equal(t, 1, reader.retainedCalls)
	require.Empty(t, reader.calls)
	catalogs.err = errors.New("selected source unavailable")
	_, err = source.Lookup(t.Context(), uuid.New(), "https://example.com/mcp", false)
	require.Error(t, err)
	require.Equal(t, 1, reader.retainedCalls)
}
