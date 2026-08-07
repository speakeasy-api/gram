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
	"slices"
	"strconv"
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
	host := canonicalHostname(u)

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
		Host:        canonicalHostPort(u),
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

// canonicalHostname lowercases a URL's host and drops the trailing dot of a
// fully-qualified name.
//
// `mcp.example.com.` and `mcp.example.com` name the same server, so keeping
// the dot would give them separate artifact refs, and the public-suffix lookup
// rejects it outright as an empty label.
func canonicalHostname(u *url.URL) string {
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

// canonicalHostPort is canonicalHostname with the port reattached, for
// rebuilding a URL.
func canonicalHostPort(u *url.URL) string {
	host := canonicalHostname(u)

	// A port equal to the scheme's default is redundant: `https://h:443/x` and
	// `https://h/x` are the same endpoint and must not key as two artifacts.
	// Compared numerically: a port is a number, and `:0443` is the same
	// endpoint as the implicit 443 even though the strings differ.
	if port := u.Port(); port != "" && !isDefaultPort(u.Scheme, port) {
		return net.JoinHostPort(host, port)
	}

	// Hostname() strips the brackets from an IPv6 literal, and a URL is
	// malformed without them. JoinHostPort is not usable here because an empty
	// port still emits a trailing colon.
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}

	return host
}

// defaultPorts is the port each scheme implies, which canonicalisation drops.
var defaultPorts = map[string]int{
	"http":  80,
	"https": 443,
}

// isDefaultPort reports whether an explicit port is the one its scheme already
// implies. The comparison is numeric so a zero-padded spelling still matches.
func isDefaultPort(scheme string, port string) bool {
	implied, known := defaultPorts[scheme]
	if !known {
		return false
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return false
	}

	return n == implied
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

	// An unlisted TLD falls back to the public-suffix list's implicit `*`
	// rule, which hands back the final label and would make `mcp.internal`
	// look like a registrable domain. Such a name is under no managed
	// registry, so it identifies no owner.
	//
	// Suffixes from the list's private section (`blogspot.com`,
	// `herokuapp.com`) also report icann=false but really do delimit
	// ownership, and they are always multi-label — which is what separates
	// them from the fallback rule.
	suffix, icann := publicsuffix.PublicSuffix(host)
	if !icann && !strings.Contains(suffix, ".") {
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

	fields = unwrapShell(fields)
	if len(fields) == 0 {
		return unresolved
	}

	launcher := normalizeLauncher(fields[0])
	rest := fields[1:]

	switch {
	case npmLaunchers[launcher]:
		return firstPackageSpec(rest, launcher, RegistryNPM)
	case pypiLaunchers[launcher]:
		return firstPackageSpec(rest, launcher, RegistryPyPI)
	}

	subcommands, isSubcommandLauncher := subcommandLaunchers[launcher]
	if !isSubcommandLauncher || len(rest) == 0 {
		return unresolved
	}

	reg, known := subcommands[rest[0]]
	if !known {
		return unresolved
	}

	return firstPackageSpec(rest[1:], launcher, reg)
}

// normalizeLauncher reduces a launcher token to a bare comparable name.
//
// The launcher may be an absolute path (`/usr/local/bin/npx`), and on Windows
// it arrives with backslash separators, a shim extension, and any casing —
// path.Base only understands forward slashes, so separators are normalised
// first. npx ships as `npx.cmd` on Windows, not `npx.exe`.
func normalizeLauncher(field string) string {
	name := strings.ToLower(path.Base(strings.ReplaceAll(field, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		if trimmed, ok := strings.CutSuffix(name, ext); ok {
			return trimmed
		}
	}

	return name
}

// unwrapShell strips a Windows command-processor prefix so the real launcher
// is examined. The canonical Windows MCP stdio config is
// `"command": "cmd", "args": ["/c", "npx", "-y", "<pkg>"]`, which would
// otherwise resolve to the shell rather than the package.
//
// Only cmd is unwrapped. A POSIX `sh -c` passes its script as one quoted
// argument, which whitespace splitting has already destroyed, so there is
// nothing reliable left to read.
func unwrapShell(fields []string) []string {
	if len(fields) < 2 || normalizeLauncher(fields[0]) != "cmd" {
		return fields
	}

	switch strings.ToLower(fields[1]) {
	case "/c", "/k":
		return fields[2:]
	default:
		return fields
	}
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
func firstPackageSpec(args []string, launcher string, registry Registry) Identity {
	var (
		selected  string
		rest      []string
		selectors int
	)

	for i := 0; i < len(args); i++ {
		name, value, joined := splitFlag(args[i])

		if !strings.HasPrefix(args[i], "-") {
			// The first bare word ends the launcher's own arguments.
			// Everything after it belongs to the package or the binary, so a
			// selector appearing later is the server's own flag rather than
			// the launcher's — which is what keeps `npx -y some-server -p 8080`
			// resolving to the server instead of to its port, and stops a
			// trailing `--package=@trusted/thing` from renaming what ran.
			if selectors == 0 {
				return packageSpec(args[i], args[i+1:], registry)
			}

			break
		}

		switch {
		case isSelectorFlag(name, launcher):
			selectors++
			if !joined {
				if i+1 >= len(args) {
					// A trailing selector names nothing. Leaving selected
					// empty makes the final switch resolve to unknown.
					break
				}
				value = args[i+1]
				i++
			}
			if selectors == 1 {
				selected, rest = value, args[i+1:]
			}

		case takesSeparateValue(name, registry) && !joined:
			// Skip the value so it is not mistaken for the package.
			// `uvx -p 3.12 mcp-server` names an interpreter.
			i++
		}
	}

	switch {
	case selectors > 1, selectors == 1 && selected == "":
		// Repeated selectors install several packages and the binary may come
		// from any of them; a selector with no value names nothing. Both are
		// unidentifiable offline, and guessing would attach evidence to the
		// wrong artifact.
		return unresolved
	case selectors == 1:
		return packageSpec(selected, rest, registry)
	}

	return unresolved
}

// splitFlag separates a flag from an inline value, so `--package=foo` yields
// ("--package", "foo", true) and `-y` yields ("-y", "", false).
func splitFlag(arg string) (string, string, bool) {
	name, value, found := strings.Cut(arg, "=")
	return name, value, found
}

// isSelectorFlag reports whether a flag names the package to install rather
// than describing how to install it. `npx -p <pkg> <bin>` and
// `uvx --from <pkg> <bin>` both run a binary out of the named package, so the
// bare word after them is a command name.
//
// The spelling is per launcher, not per registry: uvx and pipx both install
// from PyPI but name the package with --from and --spec respectively, and npx
// spells it -p where uv uses -p for the interpreter. Sharing one set across a
// registry would read pipx's package as its command name.
func isSelectorFlag(name string, launcher string) bool {
	return slices.Contains(selectorFlags[launcher], name)
}

// selectorFlags maps a launcher to the flags that name the package to install.
var selectorFlags = map[string][]string{
	"npx":  {"-p", "--package"},
	"bunx": {"-p", "--package"},
	"bun":  {"-p", "--package"},
	"pnpm": {"-p", "--package"},
	"yarn": {"-p", "--package"},
	"uvx":  {"--from"},
	"pipx": {"--spec"},
}

// takesSeparateValue reports whether a launcher flag consumes the argument
// after it. Only the flags that could otherwise be misread as a package are
// listed; an unknown value-taking flag degrades to naming its own value as the
// package, which resolution treats as any other unrecognised spec.
func takesSeparateValue(name string, registry Registry) bool {
	if registry != RegistryPyPI {
		return false
	}

	return name == "-p" || name == "--python" || name == "--with"
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
		VersionPinned:     isExactVersion(version, registry),
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
func isExactVersion(version string, registry Registry) bool {
	// npm accepts a leading v on an otherwise exact version.
	v := strings.TrimPrefix(version, "v")
	if v == "" {
		return false
	}

	// A dist-tag or a range operator never starts with a digit.
	if v[0] < '0' || v[0] > '9' {
		return false
	}
	if strings.ContainsAny(v, "^~><=|* ") {
		return false
	}

	// A wildcard is only a range when it stands as a whole component: `1.2.x`
	// floats, while the x in a prerelease or build tag such as `1.0.0-linux`
	// is part of an exact version. Testing for the letter anywhere would drop
	// every platform-suffixed release.
	core, _, _ := strings.Cut(v, "+")
	core, _, _ = strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	for _, part := range parts {
		if part == "x" || part == "X" || part == "*" {
			return false
		}
	}

	// npm resolves a partial version as a range: `pkg@1.2` installs the newest
	// 1.2.x and `pkg@1` the newest 1.x, so only a complete major.minor.patch
	// names one release. PyPI has no such rule — a version only reaches here
	// through the `==` operator, which is exact at whatever precision it names.
	if registry == RegistryNPM && len(parts) != 3 {
		return false
	}

	return true
}
