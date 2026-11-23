package platformmcp

import (
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// Catalog search results render as a carousel of small cards in the same
// MCP Apps view as every other result card. The carousel is rendered and
// escaped here and carried in the result's _meta under cardMetaKey, so the
// text and structured content stay exactly as they were.

// catalogCarouselTemplate is one card per catalog server, in a row that
// scrolls sideways. html/template escapes every value for its context. Logo is
// the one exception: a data: image URI that cardLogoDataURI builds only from
// stored bytes it has checked.
//
// A server not yet in the project gets an Add button. The button carries the
// request as plain text in data-message; the view sends that text to the chat
// as the user's message, so the agent adds the server through
// register_catalog_mcp with its usual confirmation. A server already in the
// project gets an Open link to its dashboard page instead.
var catalogCarouselTemplate = template.Must(template.New("catalog").Parse(`<section class="carousel" aria-label="MCP catalog results">` +
	`{{if .Slides}}<div class="viewport">` +
	`<button type="button" class="nav nav-prev" data-scroll="-1" aria-label="Previous servers" hidden>` + catalogChevron + `</button>` +
	`<ul class="track" tabindex="0" aria-label="MCP catalog servers{{with .Project}} for project {{.}}{{end}}">` +
	`{{range .Slides}}<li class="slide"><article class="mini">` +
	`<div class="mini-head"><div class="tile{{if .Logo}} tile-logo{{end}}">` +
	`{{if .Logo}}<img class="logo" src="{{.Logo}}" alt="">{{else if .Initials}}<span aria-hidden="true">{{.Initials}}</span>{{else}}` + cardServerIcon + `{{end}}` +
	`</div><div class="mini-title"><p class="eyebrow eyebrow-{{.Eyebrow.Tone}}">{{.Eyebrow.Label}}</p><h2>{{.Title}}</h2></div></div>` +
	`{{with .Description}}<p class="line clamp">{{.}}</p>{{end}}` +
	`<div class="foot">` +
	`{{if .Open}}<a class="button" href="{{.Open.URL}}" target="_blank" rel="noopener noreferrer">{{.Open.Label}} <span aria-hidden="true">&rarr;</span></a>` +
	`{{else if .AddMessage}}<button type="button" class="button" aria-label="Add {{.Title}} to project" data-message="{{.AddMessage}}">Add to project</button>{{end}}</div>` +
	`</article></li>{{end}}</ul>` +
	`<button type="button" class="nav nav-next" data-scroll="1" aria-label="Next servers" hidden>` + catalogChevron + `</button>` +
	`</div>` +
	`{{else}}<p class="empty">No catalog servers match this search.</p>{{end}}` +
	`{{if .More}}<p class="more">More servers match. Ask for the next page to see them.</p>{{end}}` +
	`<footer class="brand">` + cardSpeakeasyMark + `<span>speakeasy</span></footer>` +
	`</section>`))

// catalogChevron is the arrow on the carousel's scroll buttons. The previous
// button mirrors it in CSS.
const catalogChevron = `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6"/></svg>`

// catalogCarousel is what the carousel shows for one page of results.
type catalogCarousel struct {
	// Project is the slug the Add buttons add to and the added state is read
	// from, or "" when no project resolved.
	Project string
	Slides  []catalogSlide
	// More is set when the search has a next page.
	More bool
}

// catalogSlide is one catalog server. Every value except Logo is plain text.
type catalogSlide struct {
	Eyebrow     cardStatus
	Title       string
	Logo        template.URL
	Initials    string
	Description string
	// Open links to a server already in the project. When it is nil,
	// AddMessage is the chat message that asks the agent to add the server.
	Open       *cardAction
	AddMessage string
}

// catalogServerKey is a catalog server's identity in a project.
type catalogServerKey struct {
	providerKey string
	catalogRef  string
}

// catalogCardProject is the project a catalog carousel is shown for, and
// the catalog servers it already has.
type catalogCardProject struct {
	Slug  string
	Added map[catalogServerKey]catalogAddedServer
}

// catalogAddedServer is a catalog server already in the project.
type catalogAddedServer struct {
	Logo template.URL
	// OpenURL is the server's dashboard page, or "" when there is none.
	OpenURL string
}

// buildCatalogCarousel turns one page of search results into the carousel.
// project may be nil, and then no server shows as added.
func buildCatalogCarousel(candidates []CatalogCandidate, project *catalogCardProject, more bool) catalogCarousel {
	carousel := catalogCarousel{Project: "", Slides: make([]catalogSlide, 0, len(candidates)), More: more}
	if project != nil {
		carousel.Project = project.Slug
	}
	for _, candidate := range candidates {
		slide := catalogSlide{
			Eyebrow:     cardStatus{Label: "Catalog", Tone: cardToneNeutral},
			Title:       cardTitle(candidate.Name),
			Logo:        "",
			Initials:    cardInitials(candidate.Name),
			Description: candidate.Description,
			Open:        nil,
			AddMessage:  catalogAddMessage(candidate, carousel.Project),
		}
		if project != nil {
			if added, ok := project.Added[catalogServerKey{providerKey: candidate.ProviderKey, catalogRef: candidate.CatalogRef}]; ok {
				slide.Eyebrow = cardStatus{Label: "In project", Tone: cardToneDone}
				slide.Logo = added.Logo
				slide.AddMessage = ""
				if added.OpenURL != "" {
					slide.Open = &cardAction{Label: "Open", URL: added.OpenURL}
				}
			}
		}
		carousel.Slides = append(carousel.Slides, slide)
	}
	return carousel
}

// catalogAddMessage is the chat message an Add button sends. It names the
// exact catalog entry so the agent does not have to search again.
func catalogAddMessage(candidate CatalogCandidate, projectSlug string) string {
	target := "a project"
	if projectSlug != "" {
		target = "the " + projectSlug + " project"
	}
	return fmt.Sprintf("Add the %s MCP server from the catalog to %s (provider_key %s, catalog_ref %s).", cardTitle(candidate.Name), target, candidate.ProviderKey, candidate.CatalogRef)
}

// catalogCarouselResult is a tool result that carries the carousel in _meta.
// The SDK still fills in the text and structured content from the handler's
// output.
func catalogCarouselResult(carousel catalogCarousel) *mcp.CallToolResult {
	var rendered strings.Builder
	if err := catalogCarouselTemplate.Execute(&rendered, carousel); err != nil {
		// The carousel is decoration on a result that has already succeeded.
		return nil
	}
	return &mcp.CallToolResult{Meta: mcp.Meta{cardMetaKey: rendered.String()}, Content: nil, StructuredContent: nil, IsError: false, InputRequests: nil, RequestState: ""}
}

// catalogCardProject reads the project a catalog carousel is shown for: the
// named one, or else the organization's default project, the same one
// register_catalog_mcp adds to. The carousel is decoration, so any failure
// returns nil and the carousel shows no server as added. Logos are read only
// for added servers on this page.
func (s *RegistrationService) catalogCardProject(ctx context.Context, principal Principal, projectSlug string, page []CatalogCandidate) *catalogCardProject {
	if s == nil || s.store == nil || s.logos == nil {
		return nil
	}
	slug, err := projectSlugOrDefault(ctx, s.store, principal.OrganizationID, projectSlug)
	if err != nil {
		return nil
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, slug)
	if err != nil {
		return nil
	}
	if eligible, err := s.store.EligibleCatalogRegistrationTarget(ctx, principal.OrganizationID, project); err != nil || !eligible {
		return nil
	}
	rows, err := platformrepo.New(s.logos.db).ListPlatformMCPProjectCatalogServers(ctx, platformrepo.ListPlatformMCPProjectCatalogServersParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	if err != nil {
		s.logos.logger.WarnContext(ctx, "list platform mcp project catalog servers for card", attr.SlogError(err), attr.SlogProjectID(project.ID.String()))
		return nil
	}

	onPage := make(map[catalogServerKey]bool, len(page))
	for _, candidate := range page {
		onPage[catalogServerKey{providerKey: candidate.ProviderKey, catalogRef: candidate.CatalogRef}] = true
	}
	addedOnPage := false
	for _, row := range rows {
		if onPage[catalogServerKey{providerKey: row.CatalogProvider, catalogRef: row.CatalogReference}] {
			addedOnPage = true
			break
		}
	}
	// The organization slug is read once, and only when an Open link needs it.
	organizationSlug := ""
	if addedOnPage && s.dashboardURL != nil {
		organization, err := organizationsrepo.New(s.logos.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
		if err != nil {
			s.logos.logger.WarnContext(ctx, "read platform mcp card organization", attr.SlogError(err))
		} else {
			organizationSlug = organization.Slug
		}
	}
	cardProject := &catalogCardProject{Slug: project.Slug, Added: make(map[catalogServerKey]catalogAddedServer)}
	for _, row := range rows {
		key := catalogServerKey{providerKey: row.CatalogProvider, catalogRef: row.CatalogReference}
		if !onPage[key] {
			continue
		}
		if _, seen := cardProject.Added[key]; seen {
			continue
		}
		added := catalogAddedServer{
			Logo:    s.logos.registrationLogo(ctx, principal.OrganizationID, project.ID, row.RegistrationID.String()),
			OpenURL: "",
		}
		if route := mcpservers.DashboardRouteParam(row.McpServerSlug.String, row.McpServerID); organizationSlug != "" && route != "" {
			added.OpenURL = s.dashboardURL.JoinPath(organizationSlug, "projects", project.Slug, "mcp", route).String()
		}
		cardProject.Added[key] = added
	}
	return cardProject
}
