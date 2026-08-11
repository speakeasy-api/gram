// Package packagemeta looks up what a package registry publishes about an MCP
// server distributed as a package.
//
// It exists because the MCP registry is the weaker source for the servers this
// workflow actually receives. A request usually arrives as a shadow-MCP block
// or a proactive ask, naming a server no curated catalogue lists, and the MCP
// registry carries no license field for any server at all. npm and PyPI carry
// license, publish history, version count and maintainer count for exactly
// that population, over public endpoints needing no credential.
//
// As with the rest of the evidence pipeline, everything returned is what a
// publisher declared. A license field is a claim about licensing, not a
// verified one, and a maintainer count says how many accounts can publish —
// not who they are or whether they are still trusted.
package packagemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// maxResponseBytes bounds a registry response. npm returns the full document
// for every published version, which for a long-lived package runs to tens of
// megabytes, so the cap has to sit above what real packages produce — an
// oversized response fails loudly rather than truncating into a baffling
// decode error, and a cap that ordinary packages hit would fail the lookups
// for exactly the most established publishers.
const maxResponseBytes = 32 << 20

// Doer issues HTTP requests. `*guardian.HTTPClient` satisfies it, which is what
// the composition root supplies so lookups inherit egress protection.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Metadata is what a package registry publishes about one package.
//
// Every field is a publisher declaration. Zero values mean the registry
// published nothing, which must surface as unknown rather than as a finding.
type Metadata struct {
	// Registry is the index the metadata came from.
	Registry identity.Registry

	// Name is the package as the registry names it.
	Name string

	// License is the declared license, empty when none is published. The MCP
	// registry carries no license for any server, so for a package-distributed
	// server this is the only place it comes from.
	//
	// Not necessarily a tidy SPDX identifier: the reference filesystem server
	// publishes "SEE LICENSE IN LICENSE", which is a valid expression telling
	// you to go read a file. Render it as the string the publisher chose
	// rather than parsing it into a known-licenses set.
	License string

	// LatestVersion is the version the registry currently resolves as latest.
	LatestVersion string

	// FirstPublished is when the package first appeared. A package first
	// published days ago is a materially different proposition from one with
	// years of history.
	FirstPublished time.Time

	// LastPublished is the most recent release, which is the maintenance
	// signal an approver reads.
	LastPublished time.Time

	// VersionCount is how many versions have been published.
	VersionCount int

	// MaintainerCount is how many accounts can publish the package. A single
	// maintainer is a bus factor and an account-takeover surface; it is not by
	// itself a problem.
	MaintainerCount int

	// Deprecated reports that the publisher marked the package deprecated
	// (npm) or the release yanked (PyPI).
	Deprecated bool

	// DeprecationReason is the publisher's stated reason, if any.
	DeprecationReason string
}

// Client looks packages up over the public registry APIs.
type Client struct {
	http    Doer
	npmURL  string
	pypiURL string
}

// Option overrides a client default.
type Option func(*Client)

// WithNPMBaseURL points npm lookups at a different host, for a self-hosted
// mirror such as Verdaccio or Artifactory, or for a test server.
func WithNPMBaseURL(base string) Option {
	return func(c *Client) { c.npmURL = strings.TrimSuffix(base, "/") }
}

// WithPyPIBaseURL points PyPI lookups at a different host, for a self-hosted
// index or a test server.
func WithPyPIBaseURL(base string) Option {
	return func(c *Client) { c.pypiURL = strings.TrimSuffix(base, "/") }
}

// NewClient builds a client against the public registries. The Doer should be
// guardian-backed in production so lookups are subject to egress policy.
func NewClient(doer Doer, options ...Option) *Client {
	client := &Client{
		http:    doer,
		npmURL:  "https://registry.npmjs.org",
		pypiURL: "https://pypi.org",
	}
	for _, option := range options {
		option(client)
	}

	return client
}

// Lookup fetches what the registry publishes about a package.
//
// A package the registry does not know returns (nil, nil): an unknown package
// is an ordinary outcome for a server nobody has catalogued, not an error, and
// it must reach the approver as "not published there" rather than as a failure.
func (c *Client) Lookup(ctx context.Context, registry identity.Registry, name string) (*Metadata, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}

	switch registry {
	case identity.RegistryNPM:
		return c.lookupNPM(ctx, trimmed)
	case identity.RegistryPyPI:
		return c.lookupPyPI(ctx, trimmed)
	default:
		return nil, nil
	}
}

// get fetches and decodes a registry document, reporting whether the package
// exists.
func (c *Client) get(ctx context.Context, endpoint string, into any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("build package metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("fetch package metadata: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetch package metadata: %s", resp.Status)
	}

	// Read one byte past the cap so an oversized body is detected as such and
	// fails with a size error, not as a decode failure on truncated JSON.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return false, fmt.Errorf("read package metadata: %w", err)
	}
	if len(body) > maxResponseBytes {
		return false, fmt.Errorf("package metadata response exceeded the %d-byte limit", maxResponseBytes)
	}

	if err := json.Unmarshal(body, into); err != nil {
		return false, fmt.Errorf("decode package metadata: %w", err)
	}

	return true, nil
}

type npmDocument struct {
	Name     string `json:"name"`
	DistTags struct {
		Latest string `json:"latest"`
	} `json:"dist-tags"`
	Time        npmTimes       `json:"time"`
	Versions    map[string]any `json:"versions"`
	Maintainers []struct {
		Name string `json:"name"`
	} `json:"maintainers"`
	License any `json:"license"`
}

// npmTimes is npm's time map with non-string values dropped. A package that
// ever had a version unpublished carries `time.unpublished` as an object, and
// a strict string-map decode would fail the whole lookup over a field this
// package never reads.
type npmTimes map[string]string

func (t *npmTimes) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode npm time map: %w", err)
	}

	out := make(map[string]string, len(raw))
	for key, value := range raw {
		var timestamp string
		if err := json.Unmarshal(value, &timestamp); err != nil {
			continue
		}
		out[key] = timestamp
	}
	*t = out

	return nil
}

func (c *Client) lookupNPM(ctx context.Context, name string) (*Metadata, error) {
	// A scoped name contains exactly one slash, separating scope from
	// package. Each segment is validated and escaped individually so a
	// crafted name cannot smuggle extra path segments — or `.`/`..`
	// traversal — into the registry request once the intended separator is
	// in place.
	segments := strings.Split(name, "/")
	if len(segments) > 2 {
		return nil, fmt.Errorf("invalid npm package name %q", name)
	}
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return nil, fmt.Errorf("invalid npm package name %q", name)
		}
		escaped[i] = url.PathEscape(segment)
	}
	endpoint := c.npmURL + "/" + strings.Join(escaped, "/")

	var doc npmDocument
	found, err := c.get(ctx, endpoint, &doc)
	if err != nil || !found {
		return nil, err
	}

	// A 200 whose document carries no name is not a package the registry
	// published — it is a response this client does not recognize, and
	// presenting it as metadata would put an empty package in front of an
	// approver as if it were a finding.
	if doc.Name == "" {
		return nil, fmt.Errorf("unrecognized npm metadata response for %q", name)
	}

	latest := doc.DistTags.Latest
	meta := &Metadata{
		Registry:        identity.RegistryNPM,
		Name:            doc.Name,
		License:         npmLicense(doc.License),
		LatestVersion:   latest,
		FirstPublished:  npmTime(doc.Time["created"]),
		LastPublished:   npmLastRelease(doc.Time),
		VersionCount:    len(doc.Versions),
		MaintainerCount: len(doc.Maintainers),
		// npm marks deprecation per version. The latest version being
		// deprecated is what an approver cares about — an old deprecated
		// release says nothing about what installs today.
		Deprecated:        false,
		DeprecationReason: "",
	}

	if version, ok := doc.Versions[latest].(map[string]any); ok {
		if reason, ok := version["deprecated"].(string); ok {
			meta.Deprecated = true
			meta.DeprecationReason = reason
		}
	}

	return meta, nil
}

// npmLicense reads npm's license field, which is a string on modern packages
// and an object on older ones.
func npmLicense(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]any:
		if name, ok := value["type"].(string); ok {
			return name
		}
	}

	return ""
}

// npmLastRelease is the newest per-version publish time. The top-level
// `modified` entry moves on any metadata edit — a deprecation or a dist-tag
// change would make an abandoned package read as actively maintained — so the
// release times are what carry the maintenance signal, matching what the PyPI
// path derives from its per-release upload times.
func npmLastRelease(times npmTimes) time.Time {
	var last time.Time
	for key, value := range times {
		if key == "created" || key == "modified" {
			continue
		}
		if parsed := npmTime(value); parsed.After(last) {
			last = parsed
		}
	}

	return last
}

func npmTime(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

type pypiDocument struct {
	Info struct {
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		License      string   `json:"license"`
		Yanked       bool     `json:"yanked"`
		YankedReason string   `json:"yanked_reason"`
		Classifiers  []string `json:"classifiers"`
	} `json:"info"`
	Releases map[string][]struct {
		UploadTime string `json:"upload_time_iso_8601"`
	} `json:"releases"`
}

func (c *Client) lookupPyPI(ctx context.Context, name string) (*Metadata, error) {
	// PEP 508 extras select optional dependencies of the same package, so
	// `mcp-server[sse]` is looked up as `mcp-server`. Only a well-formed,
	// terminal extras expression is stripped: a malformed spec like `foo[bar`
	// names nothing pip would install, and quietly resolving it to `foo`
	// would attribute another package's evidence to it. Left unchanged, it
	// finds no project and surfaces as unknown, which is the honest outcome.
	if open := strings.IndexByte(name, '['); open >= 0 {
		inner, closed := strings.CutSuffix(name[open+1:], "]")
		if closed && inner != "" && !strings.ContainsAny(inner, "[]") {
			name = strings.TrimSpace(name[:open])
		}
	}

	endpoint := c.pypiURL + "/pypi/" + url.PathEscape(name) + "/json"

	var doc pypiDocument
	found, err := c.get(ctx, endpoint, &doc)
	if err != nil || !found {
		return nil, err
	}

	if doc.Info.Name == "" {
		return nil, fmt.Errorf("unrecognized pypi metadata response for %q", name)
	}

	first, last := pypiPublishWindow(doc)

	return &Metadata{
		Registry:       identity.RegistryPyPI,
		Name:           doc.Info.Name,
		License:        pypiLicense(doc),
		LatestVersion:  doc.Info.Version,
		FirstPublished: first,
		LastPublished:  last,
		VersionCount:   len(doc.Releases),
		// PyPI publishes no maintainer list on this endpoint, so the count is
		// unknown rather than zero. Zero must not be rendered as "no
		// maintainers".
		MaintainerCount:   0,
		Deprecated:        doc.Info.Yanked,
		DeprecationReason: doc.Info.YankedReason,
	}, nil
}

// pypiLicense prefers the explicit license field and falls back to the
// Trove classifier, which is where well-formed packages put the SPDX-ish name
// and which is often populated when the free-text field is not.
func pypiLicense(doc pypiDocument) string {
	if license := strings.TrimSpace(doc.Info.License); license != "" {
		return license
	}

	const prefix = "License :: "
	for _, classifier := range doc.Info.Classifiers {
		if !strings.HasPrefix(classifier, prefix) {
			continue
		}
		parts := strings.Split(classifier, " :: ")
		return parts[len(parts)-1]
	}

	return ""
}

// pypiPublishWindow finds the earliest and latest upload across every release,
// since PyPI publishes no package-level created or modified timestamp.
func pypiPublishWindow(doc pypiDocument) (time.Time, time.Time) {
	var first, last time.Time
	for _, files := range doc.Releases {
		for _, file := range files {
			uploaded, err := time.Parse(time.RFC3339, file.UploadTime)
			if err != nil {
				continue
			}
			if first.IsZero() || uploaded.Before(first) {
				first = uploaded
			}
			if uploaded.After(last) {
				last = uploaded
			}
		}
	}

	return first, last
}
