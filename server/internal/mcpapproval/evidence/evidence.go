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
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/exposure"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
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

	// Exposure is what this project's own traffic says about the server,
	// present only when the target had a URL to look up.
	Exposure *ExposureSection `json:"exposure,omitempty"`

	// Gaps lists the sources that could not be consulted this gather. A
	// reader must treat a listed source as unknown, never as clean.
	Gaps []string `json:"gaps,omitempty"`
}

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

// Gap names for the sources that can fail independently.
const (
	GapPackageLookup  = "package_lookup_failed"
	GapExposureLookup = "exposure_lookup_failed"
)

// PackageLookup is the slice of the package-metadata client the assembler
// needs. *packagemeta.Client satisfies it.
type PackageLookup interface {
	Lookup(ctx context.Context, registry identity.Registry, name string) (*packagemeta.Metadata, error)
}

var _ PackageLookup = (*packagemeta.Client)(nil)

// defaultSourceTimeout bounds each source's gather independently, so one
// unreachable source costs its own budget rather than the whole gather's —
// an admission is delayed by a registry outage, never held for the sum of
// every source's worst case.
const defaultSourceTimeout = 3 * time.Second

// Assembler gathers evidence for one requested server.
type Assembler struct {
	packages      PackageLookup
	traffic       exposure.Reader
	sourceTimeout time.Duration
}

// Option configures an Assembler.
type Option func(*Assembler)

// WithSourceTimeout overrides the per-source gather budget.
func WithSourceTimeout(timeout time.Duration) Option {
	return func(a *Assembler) { a.sourceTimeout = timeout }
}

func NewAssembler(packages PackageLookup, traffic exposure.Reader, options ...Option) *Assembler {
	assembler := &Assembler{packages: packages, traffic: traffic, sourceTimeout: defaultSourceTimeout}
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
		Exposure:            nil,
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
		}
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
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode evidence document: %w", err)
	}

	return encoded, nil
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
