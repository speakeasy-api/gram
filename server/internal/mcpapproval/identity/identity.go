// Package identity resolves an observed MCP server reference into a stable
// artifact identity.
//
// A reference reaches us as one of three things: an HTTP(S) endpoint, a stdio
// launch command, or a bare `mcp__<server>__` tool-name prefix that the
// sender's inventory never resolved. Only the first two identify a server, and
// the third is reported unresolved rather than guessed at.
//
// Resolution is pure and offline. Nothing here contacts a registry, a package
// index, or the server itself: probing a server under review would mean
// handing credentials to the destination being reviewed.
package identity

import (
	"net"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Kind classifies what a reference resolved to.
type Kind string

const (
	// KindUnresolved means the reference names no server we can identify.
	// This is a legitimate outcome, not an error, and callers must surface it
	// as unknown rather than as an absence of findings.
	KindUnresolved Kind = "unresolved"

	// KindRemote means the reference is an HTTP(S) MCP endpoint.
	KindRemote Kind = "remote"

	// KindPackage means the reference launches a published package over stdio.
	KindPackage Kind = "package"
)

// Registry names the package index a stdio launcher installs from.
type Registry string

const (
	// RegistryNPM is the npm registry, reached through npx, bunx, pnpm dlx, or yarn dlx.
	RegistryNPM Registry = "npm"

	// RegistryPyPI is the Python package index, reached through uvx or pipx run.
	RegistryPyPI Registry = "pypi"
)

// Identity is the resolved shape of a requested MCP server.
type Identity struct {
	// Kind classifies the reference. Every other field is meaningful only for
	// a particular kind, and callers should switch on this first.
	Kind Kind

	// ArtifactRef is the canonical identifier evidence and analysis key on:
	// `npm:@scope/name@1.2.3`, `pypi:name@1.2.3`, or `url:https://host/path`.
	// Empty when unresolved. A package ref carries its version only when one
	// was named, so the ref alone does not imply immutability.
	ArtifactRef string

	// VersionPinned reports whether the reference names an exact version.
	// False for a floating invocation — no version, a dist-tag such as
	// `latest`, or a range such as `^1.2.0` — where the code that runs may
	// differ from the code anything scanned. Source analysis is only
	// meaningful against a pinned artifact.
	VersionPinned bool

	// Host is the full lower-cased hostname for a remote endpoint, subdomain
	// included, empty otherwise.
	Host string

	// RegistrableDomain is Host reduced to its effective TLD plus one label,
	// so `mcp.somevendor.io` becomes `somevendor.io`. This is the part that
	// says who owns the endpoint, and comparing a full host against a vendor's
	// known domain would call every subdomain a mismatch.
	//
	// Empty when no registrable domain exists — an IP literal, `localhost`, or
	// a hostname under no public suffix. Whether the owner is the vendor it
	// appears to be remains a question for evidence gathering, not this package.
	RegistrableDomain string

	// Registry is the package index for a package reference, empty otherwise.
	Registry Registry

	// PackageName is the package identifier, including any npm scope.
	PackageName string

	// PackageVersion is the version or tag exactly as it was named, which may
	// be a dist-tag or a range. Empty when the reference named none.
	PackageVersion string
}

// unresolved is the identity for a reference that names no server we can
// place. Shared so every failure path returns the same value.
var unresolved = Identity{
	Kind:              KindUnresolved,
	ArtifactRef:       "",
	VersionPinned:     false,
	Host:              "",
	RegistrableDomain: "",
	Registry:          "",
	PackageName:       "",
	PackageVersion:    "",
}

// npmLaunchers and pypiLaunchers are the single-token commands that fetch and
// run a package in one step. Multi-token forms (`pnpm dlx`, `pipx run`) are
// handled by subcommandLaunchers.
var npmLaunchers = map[string]bool{
	"npx":  true,
	"bunx": true,
}

var pypiLaunchers = map[string]bool{
	"uvx": true,
}

// subcommandLaunchers maps a launcher and its required subcommand to the
// registry it installs from. Requiring the subcommand keeps `pnpm install`
// and `pipx install` — which do not run a server — out of resolution.
var subcommandLaunchers = map[string]map[string]Registry{
	"pnpm": {"dlx": RegistryNPM},
	"yarn": {"dlx": RegistryNPM},
	"bun":  {"x": RegistryNPM},
	"pipx": {"run": RegistryPyPI},
}

// Resolve turns an observed server reference into an identity.
//
// The reference is whatever the sender recorded: a URL, a stdio launch
// command, or something that identifies nothing. Resolve never errors —
// an unidentifiable reference resolves to KindUnresolved, which callers must
// handle as a real outcome.
func Resolve(reference string) Identity {
	ref := strings.TrimSpace(reference)
	if ref == "" {
		return unresolved
	}

	if u, ok := absoluteHTTPURL(ref); ok {
		return remoteIdentity(u)
	}

	return resolveCommand(ref)
}

// absoluteHTTPURL reports whether a reference is an absolute http(s) URL with
// a host, which is what distinguishes an endpoint from a launch command.
func absoluteHTTPURL(reference string) (*url.URL, bool) {
	u, err := url.Parse(reference)
	if err != nil {
		return nil, false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	if u.Hostname() == "" {
		return nil, false
	}
	return u, true
}

// remoteIdentity builds the identity for an HTTP(S) endpoint, dropping the
// query string and any embedded credentials so two references to the same
// server share an artifact ref. A remote endpoint is never version-pinned:
// nothing links a URL to a fixed revision of what serves it.
func remoteIdentity(u *url.URL) Identity {
	host := strings.ToLower(u.Hostname())

	return Identity{
		Kind:              KindRemote,
		ArtifactRef:       "url:" + redactedURL(u),
		VersionPinned:     false,
		Host:              host,
		RegistrableDomain: registrableDomain(host),
		Registry:          "",
		PackageName:       "",
		PackageVersion:    "",
	}
}

// redactedURL renders a URL with everything that is per-install or secret
// removed: credentials in the userinfo, the query string, and the fragment.
// What remains is the part two references to the same server share, and it is
// safe to store and show.
func redactedURL(u *url.URL) string {
	canonical := &url.URL{
		Scheme:      u.Scheme,
		Opaque:      "",
		User:        nil,
		Host:        strings.ToLower(u.Host),
		Path:        u.Path,
		RawPath:     u.RawPath,
		OmitHost:    false,
		ForceQuery:  false,
		RawQuery:    "",
		Fragment:    "",
		RawFragment: "",
	}

	return canonical.String()
}

// registrableDomain reduces a hostname to its effective TLD plus one label,
// which is the part that identifies who owns it.
//
// Returns empty for anything with no registrable domain — an IP literal, a
// single-label host such as `localhost`, or a name under no known public
// suffix. An empty result means "ownership undeterminable", which a caller
// must treat as unknown rather than as a mismatch.
func registrableDomain(host string) string {
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}

	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}

	return domain
}

// resolveCommand extracts the package a stdio launch command runs.
//
// Only the package the launcher installs counts. Scanning the whole command
// for anything package-shaped would let an unrelated argument
// (`npx @evil/mcp --docs some-trusted-pkg`) stand in for the real one, which
// is a single flag's worth of evasion.
func resolveCommand(command string) Identity {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return unresolved
	}

	// The launcher may be an absolute path (`/usr/local/bin/npx`). Windows
	// commands arrive with backslash separators and an extension, and
	// path.Base only understands forward slashes, so normalise first.
	launcher := strings.TrimSuffix(path.Base(strings.ReplaceAll(fields[0], `\`, "/")), ".exe")
	rest := fields[1:]

	switch {
	case npmLaunchers[launcher]:
		return firstPackageSpec(rest, RegistryNPM)
	case pypiLaunchers[launcher]:
		return firstPackageSpec(rest, RegistryPyPI)
	}

	subcommands, isSubcommandLauncher := subcommandLaunchers[launcher]
	if !isSubcommandLauncher || len(rest) == 0 {
		return unresolved
	}

	reg, known := subcommands[rest[0]]
	if !known {
		return unresolved
	}

	return firstPackageSpec(rest[1:], reg)
}

// firstPackageSpec takes the first non-flag argument as the package spec.
// Flags before it belong to the launcher (`-y`, `--yes`), and first-wins means
// a trailing argument cannot displace the real package. Real configurations
// pass the server's own arguments after the package
// (`npx -y @scope/server /some/path`), so stopping at the first candidate is
// what keeps those out.
//
// The exception is a flag that names the package itself. `npx -p <pkg> <bin>`
// and `npx --package=<pkg> <bin>` both install <pkg> and then run a binary
// from it, so the bare word after them is a binary name, not a package.
// mcp-remote's own documented invocation takes that form.
func firstPackageSpec(args []string, registry Registry) Identity {
	// The selector is npx-specific. uv spells -p as --python and uses it to
	// choose an interpreter, so reading it as a package there would resolve
	// `uvx -p 3.12 mcp-server` to the Python version.
	if registry == RegistryNPM {
		spec, rest, count := npmPackageSelector(args)
		switch {
		case count > 1, count == 1 && spec == "":
			// Repeated selectors install several packages and the binary may
			// come from any of them; a selector with no value names nothing.
			// Both are unidentifiable offline, and guessing would attach
			// evidence to the wrong artifact.
			return unresolved
		case count == 1:
			return packageSpec(spec, rest, registry)
		}
	}

	// uv's own selector. `uvx --from <pkg> <bin>` is the counterpart of npx's
	// -p: the package is named by the flag and the bare word after it is a
	// binary.
	if registry == RegistryPyPI {
		for i, arg := range args {
			if value, ok := strings.CutPrefix(arg, "--from="); ok && value != "" {
				return packageSpec(value, args[i+1:], registry)
			}
			if arg == "--from" && i+1 < len(args) {
				return packageSpec(args[i+1], args[i+2:], registry)
			}
		}
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if takesSeparateValue(arg, registry) {
			// Skip the flag's value so it is not mistaken for the package.
			// `uvx -p 3.12 mcp-server` names an interpreter, not a package.
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}

		return packageSpec(arg, args[i+1:], registry)
	}

	return unresolved
}

// takesSeparateValue reports whether a launcher flag consumes the argument
// after it. Only the flags that could otherwise be misread as a package are
// listed; an unknown value-taking flag degrades to naming its own value as the
// package, which resolution treats as any other unrecognised spec.
func takesSeparateValue(arg string, registry Registry) bool {
	if registry != RegistryPyPI {
		return false
	}

	return arg == "-p" || arg == "--python" || arg == "--with"
}

// npmPackageSelector finds npx's package selector in both its spellings,
// `-p <pkg>` / `--package <pkg>` and `-p=<pkg>` / `--package=<pkg>`.
//
// It returns the first selector's value, the arguments following it, and how
// many selectors appeared in total. The count is what lets the caller reject
// an invocation naming several packages rather than picking one arbitrarily.
func npmPackageSelector(args []string) (string, []string, int) {
	spec := ""
	var rest []string
	count := 0

	record := func(value string, remaining []string) {
		count++
		if count == 1 {
			spec = value
			rest = remaining
		}
	}

	for i, arg := range args {
		switch {
		case arg == "-p", arg == "--package":
			if i+1 >= len(args) {
				record("", nil)
				continue
			}
			record(args[i+1], args[i+2:])
		case strings.HasPrefix(arg, "-p="):
			record(strings.TrimPrefix(arg, "-p="), args[i+1:])
		case strings.HasPrefix(arg, "--package="):
			record(strings.TrimPrefix(arg, "--package="), args[i+1:])
		}
	}

	return spec, rest, count
}

// packageSpec resolves a package spec, diverting to the proxy's target when
// the package is mcp-remote. rest is what followed the spec, which is where
// that target appears.
func packageSpec(spec string, rest []string, registry Registry) Identity {
	if registry == RegistryNPM && isMCPRemoteSpec(spec) {
		return mcpRemoteIdentity(rest)
	}

	return packageIdentity(spec, registry)
}

// isMCPRemoteSpec reports whether an argument is the npm spec for mcp-remote,
// tolerating a version suffix.
//
// The unscoped name is the only one accepted. A package whose last path
// segment happens to be "mcp-remote" (`@evil/mcp-remote`) can connect
// anywhere, so treating it as the proxy would let it launder its target URL
// into an identity that looks like the server the target names.
func isMCPRemoteSpec(spec string) bool {
	name, _ := splitPackageSpec(spec, RegistryNPM)
	return name == "mcp-remote"
}

// mcpRemoteIdentity resolves a command that proxies stdio to a remote server
// through mcp-remote. The identity is the URL being proxied to, not the proxy
// package: every server installed this way would otherwise collapse into one
// identity. This is the shape of Gram's own install snippet for OAuth-backed
// servers, so it is common rather than exotic.
//
// The target is the first absolute http(s) argument after the package spec.
// First-wins means a trailing argument cannot displace it.
func mcpRemoteIdentity(args []string) Identity {
	for _, arg := range args {
		if u, ok := absoluteHTTPURL(arg); ok {
			return remoteIdentity(u)
		}
	}

	return unresolved
}

// packageIdentity builds the identity for a package spec such as
// `@scope/name@1.2.3` or `name==1.2.3`.
func packageIdentity(spec string, registry Registry) Identity {
	// npm and pip both accept a URL in place of a registry name, and such a
	// URL can carry a token in its userinfo or query
	// (`https://TOKEN@github.com/org/pkg.git`). The artifact ref is stored and
	// shown to reviewers, so strip those the same way an endpoint reference is
	// stripped rather than persisting a credential.
	if u, ok := absoluteHTTPURL(spec); ok {
		spec = redactedURL(u)
	}

	name, version := splitPackageSpec(spec, registry)
	if name == "" {
		return unresolved
	}

	ref := string(registry) + ":" + name
	if version != "" {
		ref += "@" + version
	}

	return Identity{
		Kind:              KindPackage,
		ArtifactRef:       ref,
		VersionPinned:     isExactVersion(version),
		Host:              "",
		RegistrableDomain: "",
		Registry:          registry,
		PackageName:       name,
		PackageVersion:    version,
	}
}

// splitPackageSpec separates a package spec into its name and version.
//
// The npm form needs care: a leading `@` starts a scope rather than a version,
// so `@scope/name@1.2.3` splits at the second `@`, not the first.
func splitPackageSpec(spec string, registry Registry) (string, string) {
	if registry == RegistryPyPI {
		// pip specifiers use ==, >=, ~= and friends. Only the exact form
		// carries a version we could pin to; the rest resolve at install time.
		if name, version, found := strings.Cut(spec, "=="); found {
			return name, version
		}
		if idx := strings.IndexAny(spec, "<>~!="); idx >= 0 {
			return spec[:idx], ""
		}
		return spec, ""
	}

	if strings.HasPrefix(spec, "@") {
		idx := strings.Index(spec[1:], "@")
		if idx < 0 {
			return spec, ""
		}
		return spec[:idx+1], spec[idx+2:]
	}

	name, version, found := strings.Cut(spec, "@")
	if !found {
		return spec, ""
	}
	return name, version
}

// isExactVersion reports whether a version string names one immutable release.
//
// Dist-tags (`latest`, `next`) and ranges (`^1.2.0`, `>=1.0`, `1.2.x`) all
// resolve to different content over time, so a scan of whatever they point at
// today says nothing about what runs tomorrow.
func isExactVersion(version string) bool {
	if version == "" {
		return false
	}
	if version[0] < '0' || version[0] > '9' {
		return false
	}
	return !strings.ContainsAny(version, "^~><=|* ") && !strings.Contains(version, "x")
}
