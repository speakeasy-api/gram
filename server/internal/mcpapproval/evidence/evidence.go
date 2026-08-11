// Package evidence composes every signal the evidence packages derive into
// the document stored on an approval request.
//
// The document is what the admin's evidence panel renders and what a decision
// freezes. Its shape is versioned (Version), because frozen copies outlive
// changes to it.
//
// Three properties the shape holds deliberately:
//
//   - Found, not-found, and could-not-look are distinct. A registry that has
//     no such package is a finding; a lookup that failed is a gap, listed in
//     Gaps so its absence never reads as checked-and-clean.
//   - Everything gathered here is a declaration or an observation of this
//     org's own traffic, never a claim about the server's behaviour.
//   - Gathering is best-effort per source: one source failing leaves the
//     others standing, and the failure itself is recorded.
//
// The research agent's output deliberately never enters this document. Agent
// findings are web-sourced claims with their own trust tier, lifecycle, and
// caveats, stored in mcp_research_reports and presented alongside — an admin
// must always be able to tell "the registry says" and "our own traffic shows"
// from "an agent read on the web". This document is instead the agent's
// briefing: identity is where its research starts, and Gaps lists exactly
// what deterministic gathering could not get. The section names are a stable
// vocabulary, so a research claim can reference the section it corroborates
// or contradicts.
package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/authority"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/catalog"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/exposure"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
)

// Version is the shape version of the document this package assembles. It is
// stored alongside every gather and copied onto every decision's frozen
// snapshot, so bump it when the document's shape changes.
const Version = 1

// Document is one gather of everything known about a requested server.
type Document struct {
	// Identity is how the reference resolved. Always present: even an
	// unresolved reference is a real outcome the panel must show as unknown.
	Identity IdentitySection `json:"identity"`

	// Package is the registry's published metadata, present only when the
	// reference names a package and its registry knows it.
	Package *PackageSection `json:"package,omitempty"`

	// PackageNotPublished reports that the lookup ran cleanly and the
	// registry has no such package — a strong signal, distinct from a lookup
	// that failed.
	PackageNotPublished bool `json:"package_not_published,omitempty"`

	// Repository is what the code host publishes about the package's declared
	// source repository, present only when the publisher declared one on a
	// supported host and the host knows it. The repository is the publisher's
	// claim: nothing verifies it builds this package.
	Repository *RepositorySection `json:"repository,omitempty"`

	// RepositoryNotFound reports that the publisher declared a repository and
	// the code host has no such repository — checked-and-absent, and for a
	// published package a telling one. Distinct from a lookup failure, which
	// is a gap, and from a repository on an unsupported host, which was never
	// consulted and leaves both fields unset.
	RepositoryNotFound bool `json:"repository_not_found,omitempty"`

	// Advisories is OSV's answer for the package, present whenever the query
	// ran cleanly — including a clean answer with zero advisories, which is
	// checked-and-clean, a real finding.
	Advisories *AdvisoriesSection `json:"advisories,omitempty"`

	// Domain is the registry's registration record for a remote server's
	// registrable domain, present when the RDAP lookup ran cleanly.
	Domain *DomainSection `json:"domain,omitempty"`

	// Exposure is what this project's own traffic says about the server,
	// present only when the target had a URL to look up.
	Exposure *ExposureSection `json:"exposure,omitempty"`

	// Authority is what the server and its authorization server publish about
	// authentication, gathered for remote targets through the well-known
	// OAuth discovery endpoints. Also set when an unauthenticated tool
	// listing succeeded — a server that served the protocol without any
	// credential — even if it published no OAuth metadata.
	Authority *AuthoritySection `json:"authority,omitempty"`

	// Capabilities is what each tool declares about itself, gathered from the
	// server's own unauthenticated tools/list or, failing that, the registry
	// catalog's copy (see CapabilitiesSource).
	Capabilities []CapabilitySection `json:"capabilities,omitempty"`

	// CapabilitiesSource records where Capabilities came from: the server's
	// own unauthenticated tools/list (CapabilitiesFromServer) or its registry
	// catalog entry (CapabilitiesFromRegistry). The registry's copy is one
	// step further from the source, and the panel must say so. Empty when no
	// source supplied declarations; set with an empty Capabilities when a
	// source answered with zero tools, which is a real (if odd) declaration.
	CapabilitiesSource string `json:"capabilities_source,omitempty"`

	// Provenance is the registry catalog's maturity and popularity signals
	// for the matched entry, present for remote targets whose catalog lookup
	// ran. Catalogued false is checked-and-absent — every registry answered
	// and none carries the URL — distinct from a lookup failure, which is a
	// gap.
	Provenance *ProvenanceSection `json:"provenance,omitempty"`

	// Gaps lists the sources that could not be consulted this gather. A
	// reader must treat a listed source as unknown, never as clean.
	Gaps []string `json:"gaps,omitempty"`
}

// Capabilities sources, recorded in CapabilitiesSource.
const (
	CapabilitiesFromServer   = "server"
	CapabilitiesFromRegistry = "registry"
)

// IdentitySection mirrors identity.Identity for storage.
type IdentitySection struct {
	Kind              string `json:"kind"`
	ArtifactRef       string `json:"artifact_ref,omitempty"`
	VersionPinned     bool   `json:"version_pinned"`
	Host              string `json:"host,omitempty"`
	RegistrableDomain string `json:"registrable_domain,omitempty"`
	Registry          string `json:"registry,omitempty"`
	PackageName       string `json:"package_name,omitempty"`
	PackageVersion    string `json:"package_version,omitempty"`
}

// PackageSection mirrors packagemeta.Metadata for storage.
type PackageSection struct {
	Registry          string `json:"registry"`
	Name              string `json:"name"`
	License           string `json:"license,omitempty"`
	LatestVersion     string `json:"latest_version,omitempty"`
	FirstPublished    string `json:"first_published,omitempty"`
	LastPublished     string `json:"last_published,omitempty"`
	VersionCount      int    `json:"version_count,omitempty"`
	MaintainerCount   int    `json:"maintainer_count,omitempty"`
	Deprecated        bool   `json:"deprecated,omitempty"`
	DeprecationReason string `json:"deprecation_reason,omitempty"`
}

// RepositorySection mirrors repometa.Stats for storage, plus the URL the
// publisher declared.
type RepositorySection struct {
	URL   string `json:"url"`
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Stars int    `json:"stars"`
	Forks int    `json:"forks"`

	// OpenIssues counts open issues and pull requests together, which is how
	// GitHub publishes the number.
	OpenIssues int `json:"open_issues"`

	Archived  bool   `json:"archived,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	PushedAt  string `json:"pushed_at,omitempty"`

	// ContributorCount is zero when the host did not answer the extra
	// contributors request — unknown, never "no contributors".
	ContributorCount int `json:"contributor_count,omitempty"`
}

// AdvisoriesSection mirrors advisories.Report for storage.
type AdvisoriesSection struct {
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`

	// KnownCount is every advisory OSV returned; Advisories is a most-recent
	// sample of them.
	KnownCount int            `json:"known_count"`
	Advisories []AdvisoryItem `json:"advisories,omitempty"`
}

// AdvisoryItem mirrors advisories.Advisory for storage.
type AdvisoryItem struct {
	ID        string `json:"id"`
	Summary   string `json:"summary,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Published string `json:"published,omitempty"`
}

// DomainSection mirrors domainmeta.Registration for storage.
type DomainSection struct {
	Domain string `json:"domain"`

	// RegisteredAt is empty when the registry published no registration
	// event — unknown, not "recently".
	RegisteredAt string `json:"registered_at,omitempty"`

	Registrar string `json:"registrar,omitempty"`

	// Unregistered reports the registry answered that no such domain exists —
	// odd for a domain currently answering traffic, and its own signal.
	Unregistered bool `json:"unregistered,omitempty"`
}

// ExposureSection mirrors exposure.Signals for storage.
type ExposureSection struct {
	Status       string `json:"status"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	URLHost      string `json:"url_host,omitempty"`
	ServerName   string `json:"server_name,omitempty"`
	FirstSeen    string `json:"first_seen,omitempty"`
	LastSeen     string `json:"last_seen,omitempty"`
	FirstCalled  string `json:"first_called,omitempty"`
	LastCalled   string `json:"last_called,omitempty"`
	CallCount    uint64 `json:"call_count,omitempty"`
	UserCount    uint64 `json:"user_count,omitempty"`
	InUse        bool   `json:"in_use"`
}

// AuthoritySection mirrors authority.Authority for storage.
type AuthoritySection struct {
	Mode                 string              `json:"mode"`
	Transport            string              `json:"transport,omitempty"`
	Scopes               []string            `json:"scopes,omitempty"`
	DynamicRegistration  bool                `json:"dynamic_registration,omitempty"`
	DemandedSecrets      []CredentialSection `json:"demanded_secrets,omitempty"`
	OptionalSecrets      []CredentialSection `json:"optional_secrets,omitempty"`
	UnauthenticatedTools []string            `json:"unauthenticated_tools,omitempty"`
	Undeclared           bool                `json:"undeclared,omitempty"`
}

// CredentialSection mirrors authority.Credential for storage.
type CredentialSection struct {
	Name        string `json:"name"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

// CapabilitySection mirrors capability.Assessment for storage: one tool's
// declarations, never observations. The four raw hints are stored alongside
// the derived summary so the document preserves exactly what was declared —
// including explicit-false and undeclared states — rather than only the
// positive capabilities the assessment surfaces.
type CapabilitySection struct {
	Tool          string   `json:"tool"`
	Declared      []string `json:"declared,omitempty"`
	SchemaImplied []string `json:"schema_implied,omitempty"`
	ActsOnBehalf  bool     `json:"acts_on_behalf,omitempty"`
	Unannotated   bool     `json:"unannotated,omitempty"`
	ReadOnlyHint  *bool    `json:"read_only_hint,omitempty"`
	Destructive   *bool    `json:"destructive_hint,omitempty"`
	Idempotent    *bool    `json:"idempotent_hint,omitempty"`
	OpenWorld     *bool    `json:"open_world_hint,omitempty"`
}

// ProvenanceSection mirrors provenance.Provenance for storage, plus which
// registry made the claims.
type ProvenanceSection struct {
	Registry              string `json:"registry,omitempty"`
	Specifier             string `json:"specifier,omitempty"`
	Catalogued            bool   `json:"catalogued"`
	Official              bool   `json:"official,omitempty"`
	Status                string `json:"status,omitempty"`
	IsLatest              bool   `json:"is_latest,omitempty"`
	PublishedAt           string `json:"published_at,omitempty"`
	UpdatedAt             string `json:"updated_at,omitempty"`
	VisitorsLastWeek      int    `json:"visitors_last_week,omitempty"`
	VisitorsLastFourWeeks int    `json:"visitors_last_four_weeks,omitempty"`
	VisitorsTotal         int    `json:"visitors_total,omitempty"`
}

// Gap names for the sources that can fail independently.
const (
	GapPackageLookup    = "package_lookup_failed"
	GapExposureLookup   = "exposure_lookup_failed"
	GapAuthorityProbe   = "authority_probe_failed"
	GapToolDeclarations = "tool_declarations_probe_failed"
	GapCatalogLookup    = "catalog_lookup_failed"
	GapRepositoryLookup = "repository_lookup_failed"
	GapAdvisoryLookup   = "advisory_lookup_failed"
	GapDomainLookup     = "domain_lookup_failed"
)

// GappedOnAllRemoteSources reports that this gather failed on every source
// that consults the network about a remote server — the authority probe, the
// tool-declarations probe, and the registry catalog lookup. A document in this
// state carries nothing a fresh gather could not, so a refresh that produced
// one has learned nothing and must not replace a document that did better.
// Always false for non-remote targets, which have no remote sources to gap on.
func (d Document) GappedOnAllRemoteSources() bool {
	if d.Identity.Kind != string(identity.KindRemote) {
		return false
	}

	return slices.Contains(d.Gaps, GapAuthorityProbe) &&
		slices.Contains(d.Gaps, GapToolDeclarations) &&
		slices.Contains(d.Gaps, GapCatalogLookup)
}

// PackageLookup is the slice of the package-metadata client the assembler
// needs. *packagemeta.Client satisfies it.
type PackageLookup interface {
	Lookup(ctx context.Context, registry identity.Registry, name string) (*packagemeta.Metadata, error)
}

var _ PackageLookup = (*packagemeta.Client)(nil)

// RepositoryLookup fetches what a code host publishes about a declared source
// repository. A nil result with a nil error means the host has no such
// repository (or the URL is on a host the client does not consult).
// *repometa.Client satisfies it.
type RepositoryLookup interface {
	Lookup(ctx context.Context, repositoryURL string) (*repometa.Stats, error)
}

var _ RepositoryLookup = (*repometa.Client)(nil)

// AdvisoryLookup asks a vulnerability database which advisories name a
// package. A nil report with a nil error means the database does not cover
// the registry. *advisories.Client satisfies it.
type AdvisoryLookup interface {
	Query(ctx context.Context, registry identity.Registry, name string, version string) (*advisories.Report, error)
}

var _ AdvisoryLookup = (*advisories.Client)(nil)

// DomainLookup fetches a domain's registration record. A nil registration
// with a nil error means the registry knows no such domain.
// *domainmeta.Client satisfies it.
type DomainLookup interface {
	Lookup(ctx context.Context, domain string) (*domainmeta.Registration, error)
}

var _ DomainLookup = (*domainmeta.Client)(nil)

// AuthorityProber discovers a remote server's published OAuth metadata. A nil
// declaration with a nil error means the probe ran and the server publishes
// none — kept distinct from a failed probe, which is a gap.
type AuthorityProber interface {
	DiscoverAuthority(ctx context.Context, serverURL string) (*authority.Declaration, error)
}

// ToolProber lists a remote server's tool declarations without credentials.
type ToolProber interface {
	ListToolDeclarations(ctx context.Context, serverURL string) ([]capability.Declaration, error)
}

// CatalogLookup matches a server URL against the configured MCP registries.
// A nil match with a nil error is checked-and-absent. includeTools asks for
// the entry's tool declarations, which cost an extra registry round trip;
// when false, a match carries provenance only. *catalog.Source satisfies it.
type CatalogLookup interface {
	Lookup(ctx context.Context, serverURL string, includeTools bool) (*catalog.Match, error)
}

var _ CatalogLookup = (*catalog.Source)(nil)

// defaultSourceTimeout bounds each source's gather independently, so one
// unreachable source costs its own budget rather than the whole gather's —
// an admission is delayed by a registry outage, never held for the sum of
// every source's worst case.
const defaultSourceTimeout = 3 * time.Second

// Assembler gathers evidence for one requested server.
type Assembler struct {
	packages       PackageLookup
	repositories   RepositoryLookup
	advisoryDB     AdvisoryLookup
	domains        DomainLookup
	traffic        exposure.Reader
	authorityProbe AuthorityProber
	toolProbe      ToolProber
	catalog        CatalogLookup
	sourceTimeout  time.Duration
}

// Option configures an Assembler.
type Option func(*Assembler)

// WithSourceTimeout overrides the per-source gather budget.
func WithSourceTimeout(timeout time.Duration) Option {
	return func(a *Assembler) { a.sourceTimeout = timeout }
}

func NewAssembler(packages PackageLookup, repositories RepositoryLookup, advisoryDB AdvisoryLookup, domains DomainLookup, traffic exposure.Reader, authorityProbe AuthorityProber, toolProbe ToolProber, catalogLookup CatalogLookup, options ...Option) *Assembler {
	assembler := &Assembler{
		packages:       packages,
		repositories:   repositories,
		advisoryDB:     advisoryDB,
		domains:        domains,
		traffic:        traffic,
		authorityProbe: authorityProbe,
		toolProbe:      toolProbe,
		catalog:        catalogLookup,
		sourceTimeout:  defaultSourceTimeout,
	}
	for _, option := range options {
		option(assembler)
	}

	return assembler
}

// Assemble gathers one evidence document for a requested server.
//
// resolved is the reference's identity; projectID bounds the traffic lookup.
// Sources are best-effort: a failing source lands in Gaps and the rest of the
// document stands, so intake never loses an admission to a flaky registry.
func (a *Assembler) Assemble(ctx context.Context, projectID uuid.UUID, resolved identity.Identity) ([]byte, error) {
	document := Document{
		Identity: IdentitySection{
			Kind:              string(resolved.Kind),
			ArtifactRef:       resolved.ArtifactRef,
			VersionPinned:     resolved.VersionPinned,
			Host:              resolved.Host,
			RegistrableDomain: resolved.RegistrableDomain,
			Registry:          string(resolved.Registry),
			PackageName:       resolved.PackageName,
			PackageVersion:    resolved.PackageVersion,
		},
		Package:             nil,
		PackageNotPublished: false,
		Repository:          nil,
		RepositoryNotFound:  false,
		Advisories:          nil,
		Domain:              nil,
		Exposure:            nil,
		Authority:           nil,
		Capabilities:        nil,
		CapabilitiesSource:  "",
		Provenance:          nil,
		Gaps:                nil,
	}

	if resolved.Kind == identity.KindPackage && resolved.Registry != "" {
		lookupCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
		metadata, err := a.packages.Lookup(lookupCtx, resolved.Registry, resolved.PackageName)
		cancel()
		switch {
		case err != nil:
			document.Gaps = append(document.Gaps, GapPackageLookup)
		case metadata == nil:
			document.PackageNotPublished = true
		default:
			document.Package = &PackageSection{
				Registry:          string(metadata.Registry),
				Name:              metadata.Name,
				License:           metadata.License,
				LatestVersion:     metadata.LatestVersion,
				FirstPublished:    formatTime(metadata.FirstPublished),
				LastPublished:     formatTime(metadata.LastPublished),
				VersionCount:      metadata.VersionCount,
				MaintainerCount:   metadata.MaintainerCount,
				Deprecated:        metadata.Deprecated,
				DeprecationReason: metadata.DeprecationReason,
			}
			a.lookupRepository(ctx, metadata.RepositoryURL, &document)
		}

		a.queryAdvisories(ctx, resolved, &document)
	}

	// The traffic lookup takes the resolved URL, not the raw reference: a
	// stdio command that proxies through mcp-remote resolves to the URL it
	// targets, and the inventory is keyed by URL.
	if resolved.Kind == identity.KindRemote {
		target := strings.TrimPrefix(resolved.ArtifactRef, "url:")
		assessCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
		signals, err := exposure.Assess(assessCtx, a.traffic, projectID, target)
		cancel()
		if err != nil {
			document.Gaps = append(document.Gaps, GapExposureLookup)
		} else if signals.Status != exposure.StatusUnaddressable {
			document.Exposure = &ExposureSection{
				Status:       string(signals.Status),
				CanonicalURL: signals.CanonicalURL,
				URLHost:      signals.URLHost,
				ServerName:   signals.ServerName,
				FirstSeen:    formatTime(signals.FirstSeen),
				LastSeen:     formatTime(signals.LastSeen),
				FirstCalled:  formatTime(signals.FirstCalled),
				LastCalled:   formatTime(signals.LastCalled),
				CallCount:    signals.CallCount,
				UserCount:    signals.UserCount,
				InUse:        signals.InUse(),
			}
		}

		authorityConsulted := a.probeAuthority(ctx, target, &document)
		serverDeclared := a.probeToolDeclarations(ctx, target, &document, authorityConsulted)
		a.lookupCatalog(ctx, target, &document, serverDeclared)
		a.lookupDomain(ctx, resolved.RegistrableDomain, &document)
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode evidence document: %w", err)
	}

	return encoded, nil
}

// lookupRepository asks the code host about the repository the publisher
// declared. A URL on a host the client does not consult leaves the document
// untouched: not consulted must not read as checked.
func (a *Assembler) lookupRepository(ctx context.Context, repositoryURL string, document *Document) {
	if repositoryURL == "" {
		return
	}
	if _, _, supported := repometa.ParseGitHubRepo(repositoryURL); !supported {
		return
	}

	lookupCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
	defer cancel()

	stats, err := a.repositories.Lookup(lookupCtx, repositoryURL)
	switch {
	case err != nil:
		document.Gaps = append(document.Gaps, GapRepositoryLookup)
	case stats == nil:
		document.RepositoryNotFound = true
	default:
		document.Repository = &RepositorySection{
			URL:              repositoryURL,
			Host:             stats.Host,
			Owner:            stats.Owner,
			Name:             stats.Name,
			Stars:            stats.Stars,
			Forks:            stats.Forks,
			OpenIssues:       stats.OpenIssues,
			Archived:         stats.Archived,
			CreatedAt:        formatTime(stats.CreatedAt),
			PushedAt:         formatTime(stats.PushedAt),
			ContributorCount: stats.ContributorCount,
		}
	}
}

// queryAdvisories asks the vulnerability database about the package. The
// query runs whole-package rather than version-scoped: an approver deciding
// on a server wants its advisory history, and a floating invocation may
// install any version anyway. It also runs whether or not the registry lookup
// succeeded — the two sources fail independently.
func (a *Assembler) queryAdvisories(ctx context.Context, resolved identity.Identity, document *Document) {
	queryCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
	defer cancel()

	report, err := a.advisoryDB.Query(queryCtx, resolved.Registry, resolved.PackageName, "")
	switch {
	case err != nil:
		document.Gaps = append(document.Gaps, GapAdvisoryLookup)
	case report == nil:
		// The database does not cover this registry: not consulted.
	default:
		section := &AdvisoriesSection{
			Ecosystem:  report.Ecosystem,
			Package:    report.Package,
			KnownCount: report.KnownCount,
			Advisories: nil,
		}
		for _, advisory := range report.Advisories {
			section.Advisories = append(section.Advisories, AdvisoryItem{
				ID:        advisory.ID,
				Summary:   advisory.Summary,
				Severity:  advisory.Severity,
				Published: formatTime(advisory.Published),
			})
		}
		document.Advisories = section
	}
}

// lookupDomain asks the registry when a remote server's registrable domain
// was registered. An IP literal or public-suffix-less host has no registrable
// domain and is skipped: there is nothing to look up.
func (a *Assembler) lookupDomain(ctx context.Context, domain string, document *Document) {
	if domain == "" {
		return
	}

	lookupCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
	defer cancel()

	registration, err := a.domains.Lookup(lookupCtx, domain)
	switch {
	case err != nil:
		document.Gaps = append(document.Gaps, GapDomainLookup)
	case registration == nil:
		document.Domain = &DomainSection{
			Domain:       domain,
			RegisteredAt: "",
			Registrar:    "",
			Unregistered: true,
		}
	default:
		document.Domain = &DomainSection{
			Domain:       registration.Domain,
			RegisteredAt: formatTime(registration.RegisteredAt),
			Registrar:    registration.Registrar,
			Unregistered: false,
		}
	}
}

// probeAuthority asks the server's well-known endpoints what authentication
// it publishes, reporting whether discovery ran to completion. A probe that
// finds nothing leaves the section absent — the
// server publishing no OAuth metadata is not the server declaring it needs
// nothing.
func (a *Assembler) probeAuthority(ctx context.Context, serverURL string, document *Document) bool {
	probeCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
	defer cancel()

	declaration, err := a.authorityProbe.DiscoverAuthority(probeCtx, serverURL)
	if err != nil {
		document.Gaps = append(document.Gaps, GapAuthorityProbe)
		return false
	}
	if declaration == nil {
		return true
	}

	summary := authority.Summarise(*declaration)
	document.Authority = &AuthoritySection{
		Mode:                 string(summary.Mode),
		Transport:            summary.Transport,
		Scopes:               summary.Scopes,
		DynamicRegistration:  summary.DynamicRegistration,
		DemandedSecrets:      credentialSections(summary.DemandedSecrets),
		OptionalSecrets:      credentialSections(summary.OptionalSecrets),
		UnauthenticatedTools: summary.UnauthenticatedTools,
		Undeclared:           summary.Undeclared,
	}

	return true
}

func credentialSections(credentials []authority.Credential) []CredentialSection {
	sections := make([]CredentialSection, 0, len(credentials))
	for _, credential := range credentials {
		sections = append(sections, CredentialSection{
			Name:        credential.Name,
			Required:    credential.Required,
			Description: credential.Description,
		})
	}

	return sections
}

// probeToolDeclarations connects without credentials and records what each
// tool declares about itself. It reports whether the server answered — a
// refusal is not yet a gap, because the catalog lookup may still supply the
// registry's copy of the declarations; recording the gap when both fail is
// lookupCatalog's job.
//
// authorityConsulted gates the synthetic authority section: when the
// authority probe itself failed, asserting "answers without any credential"
// alongside an authority_probe_failed gap would be the exact
// failed-probe-reads-as-clean conflation the gaps exist to prevent.
func (a *Assembler) probeToolDeclarations(ctx context.Context, serverURL string, document *Document, authorityConsulted bool) bool {
	probeCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
	defer cancel()

	declarations, err := a.toolProbe.ListToolDeclarations(probeCtx, serverURL)
	if err != nil {
		return false
	}

	document.CapabilitiesSource = CapabilitiesFromServer
	a.fillCapabilities(document, declarations)
	if authorityConsulted {
		recordUnauthenticatedListing(document, declarations)
	}

	return true
}

// recordUnauthenticatedListing carries a successful credential-less tools/list
// into the authority section: the server served the MCP protocol and named
// these tools to an unauthenticated caller. When no OAuth metadata was
// published either, that success is itself the authority evidence — the
// section is created with mode none rather than left absent, because "we
// connected without any credential" is a real finding, unlike a pair of 404s
// on well-known URLs.
func recordUnauthenticatedListing(document *Document, declarations []capability.Declaration) {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.Name)
	}

	if document.Authority == nil {
		document.Authority = &AuthoritySection{
			Mode:                 string(authority.ModeNone),
			Transport:            "http",
			Scopes:               nil,
			DynamicRegistration:  false,
			DemandedSecrets:      nil,
			OptionalSecrets:      nil,
			UnauthenticatedTools: names,
			Undeclared:           false,
		}
		return
	}

	document.Authority.UnauthenticatedTools = names
}

// lookupCatalog matches the server against the configured registries. A match
// always fills the provenance section; its tool declarations fill the
// capability section only when the server itself refused to answer, labeled
// as the registry's copy. When the server refused and no registry supplies
// declarations either, the tool-declarations gap lands here — declarations
// could not be consulted anywhere, which must never read as a clean empty
// list.
func (a *Assembler) lookupCatalog(ctx context.Context, serverURL string, document *Document, serverDeclared bool) {
	lookupCtx, cancel := context.WithTimeout(ctx, a.sourceTimeout)
	defer cancel()

	// Tool declarations are only requested when the server itself refused to
	// answer: the details fetch is an extra registry round trip whose result
	// would otherwise be discarded in favor of the server's own words.
	match, err := a.catalog.Lookup(lookupCtx, serverURL, !serverDeclared)
	if err != nil {
		document.Gaps = append(document.Gaps, GapCatalogLookup)
		if !serverDeclared {
			document.Gaps = append(document.Gaps, GapToolDeclarations)
		}
		return
	}

	if match == nil {
		document.Provenance = &ProvenanceSection{
			Registry:              "",
			Specifier:             "",
			Catalogued:            false,
			Official:              false,
			Status:                "",
			IsLatest:              false,
			PublishedAt:           "",
			UpdatedAt:             "",
			VisitorsLastWeek:      0,
			VisitorsLastFourWeeks: 0,
			VisitorsTotal:         0,
		}
		if !serverDeclared {
			document.Gaps = append(document.Gaps, GapToolDeclarations)
		}
		return
	}

	document.Provenance = &ProvenanceSection{
		Registry:              match.Registry,
		Specifier:             match.Specifier,
		Catalogued:            match.Provenance.Catalogued,
		Official:              match.Provenance.Official,
		Status:                match.Provenance.Status,
		IsLatest:              match.Provenance.IsLatest,
		PublishedAt:           formatTime(match.Provenance.PublishedAt),
		UpdatedAt:             formatTime(match.Provenance.UpdatedAt),
		VisitorsLastWeek:      match.Provenance.VisitorsLastWeek,
		VisitorsLastFourWeeks: match.Provenance.VisitorsLastFourWeeks,
		VisitorsTotal:         match.Provenance.VisitorsTotal,
	}

	if serverDeclared {
		return
	}
	if match.Tools == nil {
		document.Gaps = append(document.Gaps, GapToolDeclarations)
		return
	}

	document.CapabilitiesSource = CapabilitiesFromRegistry
	a.fillCapabilities(document, match.Tools)
}

// fillCapabilities assesses each declaration and appends it to the document's
// capability section.
func (a *Assembler) fillCapabilities(document *Document, declarations []capability.Declaration) {
	for _, declaration := range declarations {
		assessment := capability.Assess(declaration)
		document.Capabilities = append(document.Capabilities, CapabilitySection{
			Tool:          assessment.Tool,
			Declared:      capabilityStrings(assessment.Declared),
			SchemaImplied: capabilityStrings(assessment.SchemaImplied),
			ActsOnBehalf:  assessment.ActsOnBehalf,
			Unannotated:   assessment.Unannotated,
			ReadOnlyHint:  declaration.ReadOnly,
			Destructive:   declaration.Destructive,
			Idempotent:    declaration.Idempotent,
			OpenWorld:     declaration.OpenWorld,
		})
	}
}

func capabilityStrings(values []capability.Capability) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}

	return out
}

// DecodeDocument reads a stored evidence document at the given shape version.
//
// This is how downstream consumers — the research-agent runner briefing
// itself, or anything rendering a decision's frozen snapshot — get the
// document typed instead of groping through a map. Version dispatch lives
// here so a frozen version-1 snapshot stays decodable after the shape moves
// on.
func DecodeDocument(raw []byte, version int) (Document, error) {
	if version != Version {
		return Document{}, fmt.Errorf("unsupported evidence version %d", version)
	}

	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return Document{}, fmt.Errorf("decode evidence document: %w", err)
	}

	return document, nil
}

// formatTime renders a timestamp for storage, empty when unknown.
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.UTC().Format(time.RFC3339)
}
