package identity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
)

func TestResolve_EmptyReferenceIsUnresolved(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"", "   ", "\n"} {
		got := identity.Resolve(ref)
		require.Equal(t, identity.KindUnresolved, got.Kind)
		require.Empty(t, got.ArtifactRef)
		require.False(t, got.VersionPinned)
	}
}

// A bare `mcp__<server>__` tool-name prefix is what a sender records when its
// inventory never resolved the server. It identifies nothing, and guessing
// from it would attach evidence to the wrong artifact.
func TestResolve_ToolNamePrefixIsUnresolved(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("mcp__somesever__")
	require.Equal(t, identity.KindUnresolved, got.Kind)
	require.Empty(t, got.ArtifactRef)
}

func TestResolve_RemoteURL(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("https://mcp.example.com/sse")
	require.Equal(t, identity.KindRemote, got.Kind)
	require.Equal(t, "url:https://mcp.example.com/sse", got.ArtifactRef)
	require.Equal(t, "mcp.example.com", got.Host)
	require.False(t, got.VersionPinned, "a remote endpoint can never be pinned")
}

// Ownership lives at the registrable domain: comparing a full host against a
// vendor's known domain would call every subdomain a mismatch.
func TestResolve_RegistrableDomainDropsSubdomains(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("https://mcp.somevendor.io/sse")
	require.Equal(t, "mcp.somevendor.io", got.Host)
	require.Equal(t, "somevendor.io", got.RegistrableDomain)

	// A multi-part public suffix must not be mistaken for a registrable domain.
	uk := identity.Resolve("https://mcp.somevendor.co.uk/sse")
	require.Equal(t, "somevendor.co.uk", uk.RegistrableDomain)
}

// A fully-qualified name with the DNS trailing dot is the same server as one
// without, so it must neither key differently nor lose its registrable domain.
func TestResolve_TrailingDotHostIsCanonicalised(t *testing.T) {
	t.Parallel()

	fqdn := identity.Resolve("https://mcp.example.com./sse")
	plain := identity.Resolve("https://mcp.example.com/sse")

	require.Equal(t, plain.ArtifactRef, fqdn.ArtifactRef)
	require.Equal(t, "mcp.example.com", fqdn.Host)
	require.Equal(t, "example.com", fqdn.RegistrableDomain)

	withPort := identity.Resolve("https://mcp.example.com.:8443/sse")
	require.Equal(t, "mcp.example.com", withPort.Host)
	require.Equal(t, "url:https://mcp.example.com:8443/sse", withPort.ArtifactRef)
}

// An unlisted TLD matches the public-suffix list's implicit wildcard rule,
// which would otherwise report the name as its own registrable domain and let
// an internal hostname masquerade as an owner.
func TestResolve_UnlistedSuffixHasNoRegistrableDomain(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"http://mcp.internal/sse",
		"http://mcp.unknownsuffix/sse",
	} {
		got := identity.Resolve(ref)
		require.Equal(t, identity.KindRemote, got.Kind, ref)
		require.Empty(t, got.RegistrableDomain, "%s is under no managed suffix", ref)
	}
}

// Private-section suffixes report icann=false like the wildcard fallback, but
// they really do delimit ownership and must survive.
func TestResolve_PrivateSuffixStillYieldsRegistrableDomain(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("https://someone.blogspot.com/sse")
	require.Equal(t, "someone.blogspot.com", got.RegistrableDomain)
}

// No registrable domain exists for these, and empty must read as
// "undeterminable" rather than as a mismatch.
func TestResolve_RegistrableDomainEmptyWhenUndeterminable(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"http://localhost:3000/sse",
		"http://127.0.0.1:3000/sse",
		"http://[::1]:3000/sse",
	} {
		got := identity.Resolve(ref)
		require.Equal(t, identity.KindRemote, got.Kind, ref)
		require.Empty(t, got.RegistrableDomain, "%s has no registrable domain", ref)
	}
}

// ArtifactRef is the key everything else hangs off, so two endpoints that
// differ only in percent-encoding must not collapse into one identity.
func TestResolve_PercentEncodedPathsStayDistinct(t *testing.T) {
	t.Parallel()

	encoded := identity.Resolve("https://mcp.example.com/a%2Fb")
	plain := identity.Resolve("https://mcp.example.com/a/b")
	require.NotEqual(t, plain.ArtifactRef, encoded.ArtifactRef)
}

// Query strings carry per-install tokens and credentials embedded in userinfo
// are secrets; neither belongs in an identity two installs should share.
func TestResolve_RemoteURLCanonicalizesAwayQueryAndCredentials(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("https://user:pass@MCP.Example.com/sse?token=abc123#frag")
	require.Equal(t, "url:https://mcp.example.com/sse", got.ArtifactRef)
	require.Equal(t, "mcp.example.com", got.Host)
}

func TestResolve_NonHTTPSchemeIsNotRemote(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("ftp://example.com/thing")
	require.Equal(t, identity.KindUnresolved, got.Kind)
}

func TestResolve_UnpinnedNpxPackage(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y @scope/mcp-server")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, identity.RegistryNPM, got.Registry)
	require.Equal(t, "@scope/mcp-server", got.PackageName)
	require.Empty(t, got.PackageVersion)
	require.Equal(t, "npm:@scope/mcp-server", got.ArtifactRef)
	require.False(t, got.VersionPinned, "no version named means it floats to latest")
}

// The scope's leading @ is part of the name, so the split has to happen at the
// second @ rather than the first.
func TestResolve_PinnedScopedNpxPackage(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y @scope/mcp-server@1.2.3")
	require.Equal(t, "@scope/mcp-server", got.PackageName)
	require.Equal(t, "1.2.3", got.PackageVersion)
	require.Equal(t, "npm:@scope/mcp-server@1.2.3", got.ArtifactRef)
	require.True(t, got.VersionPinned)
}

func TestResolve_PinnedUnscopedNpxPackage(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx mcp-server@0.4.1")
	require.Equal(t, "mcp-server", got.PackageName)
	require.Equal(t, "0.4.1", got.PackageVersion)
	require.True(t, got.VersionPinned)
}

// A dist-tag or a range moves, so scanning what it points at today says
// nothing about what runs tomorrow.
func TestResolve_DistTagAndRangeAreNotPinned(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{
		"npx pkg@latest",
		"npx pkg@next",
		"npx pkg@^1.2.0",
		"npx pkg@~1.2.0",
		"npx pkg@>=1.0.0",
		"npx pkg@1.2.x",
	} {
		got := identity.Resolve(spec)
		require.Equal(t, identity.KindPackage, got.Kind, spec)
		require.False(t, got.VersionPinned, "%s must not count as pinned", spec)
	}
}

func TestResolve_LauncherPathAndExtensionAreStripped(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("/usr/local/bin/npx -y mcp-server@1.0.0")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "mcp-server", got.PackageName)

	// Windows commands arrive with backslash separators and an extension.
	// A launcher path containing spaces is out of scope: the command is split
	// on whitespace with no quote handling.
	got = identity.Resolve(`C:\nodejs\npx.exe mcp-server@1.0.0`)
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "mcp-server", got.PackageName)
}

func TestResolve_SubcommandLaunchers(t *testing.T) {
	t.Parallel()

	npm := identity.Resolve("pnpm dlx mcp-server@2.0.0")
	require.Equal(t, identity.RegistryNPM, npm.Registry)
	require.Equal(t, "mcp-server", npm.PackageName)
	require.True(t, npm.VersionPinned)

	pypi := identity.Resolve("pipx run mcp-thing==3.1.0")
	require.Equal(t, identity.RegistryPyPI, pypi.Registry)
	require.Equal(t, "mcp-thing", pypi.PackageName)
	require.Equal(t, "pypi:mcp-thing@3.1.0", pypi.ArtifactRef)
	require.True(t, pypi.VersionPinned)
}

// `pnpm install` does not run a server, so the launcher alone must not be
// enough to resolve a package.
func TestResolve_SubcommandLauncherWithoutItsSubcommandIsUnresolved(t *testing.T) {
	t.Parallel()

	require.Equal(t, identity.KindUnresolved, identity.Resolve("pnpm install some-pkg").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("pipx install some-pkg").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("pnpm").Kind)
}

func TestResolve_UvxPackage(t *testing.T) {
	t.Parallel()

	pinned := identity.Resolve("uvx mcp-thing==1.4.0")
	require.Equal(t, identity.RegistryPyPI, pinned.Registry)
	require.Equal(t, "mcp-thing", pinned.PackageName)
	require.True(t, pinned.VersionPinned)

	// A pip range specifier names no single release.
	ranged := identity.Resolve("uvx mcp-thing>=1.4.0")
	require.Equal(t, "mcp-thing", ranged.PackageName)
	require.Empty(t, ranged.PackageVersion)
	require.False(t, ranged.VersionPinned)
}

func TestResolve_UnknownLauncherIsUnresolved(t *testing.T) {
	t.Parallel()

	require.Equal(t, identity.KindUnresolved, identity.Resolve("node ./dist/index.js").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("./my-server").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("docker run foo/bar").Kind)
}

// Every server installed through mcp-remote would otherwise collapse into the
// single identity `npm:mcp-remote`.
func TestResolve_MCPRemoteResolvesToItsTarget(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx mcp-remote@0.1.25 https://mcp.example.com/mcp/team")
	require.Equal(t, identity.KindRemote, got.Kind)
	require.Equal(t, "url:https://mcp.example.com/mcp/team", got.ArtifactRef)
	require.Equal(t, "mcp.example.com", got.Host)
}

// A package that merely ends in "mcp-remote" can connect anywhere. Treating it
// as the proxy would let it launder its target URL into an identity that looks
// like the server that URL names.
func TestResolve_ScopedLookalikeIsNotTreatedAsMCPRemote(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx @evil/mcp-remote https://mcp.example.com/mcp/team")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "@evil/mcp-remote", got.PackageName)
}

func TestResolve_MCPRemoteWithoutTargetIsUnresolved(t *testing.T) {
	t.Parallel()

	require.Equal(t, identity.KindUnresolved, identity.Resolve("npx mcp-remote").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("npx mcp-remote --debug").Kind)
}

// First-wins: a trailing argument must not displace the real package.
func TestResolve_TrailingArgumentDoesNotDisplacePackage(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y @scope/real-server --docs other-package")
	require.Equal(t, "@scope/real-server", got.PackageName)
}

// Real configurations pass the server's own arguments after the package, e.g.
// the filesystem server's allowed directories.
func TestResolve_ServerArgumentsAfterPackageAreIgnored(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y @modelcontextprotocol/server-filesystem /Users/someone/Desktop /Users/someone/Downloads")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "@modelcontextprotocol/server-filesystem", got.PackageName)
	require.False(t, got.VersionPinned)

	pypi := identity.Resolve("uvx mcp-server-git --repository /Users/someone/projects/repo")
	require.Equal(t, "mcp-server-git", pypi.PackageName)
	require.Equal(t, identity.RegistryPyPI, pypi.Registry)
}

// `npx -p <pkg> <bin>` installs <pkg> and runs a binary from it, so the bare
// word after the flag is a binary name rather than a package.
func TestResolve_PackageFlagNamesThePackage(t *testing.T) {
	t.Parallel()

	separated := identity.Resolve("npx -p some-pkg@1.2.3 some-binary")
	require.Equal(t, "some-pkg", separated.PackageName)
	require.Equal(t, "1.2.3", separated.PackageVersion)
	require.True(t, separated.VersionPinned)

	joined := identity.Resolve("npx --package=some-pkg@1.2.3 some-binary")
	require.Equal(t, "some-pkg", joined.PackageName)
	require.Equal(t, "1.2.3", joined.PackageVersion)

	long := identity.Resolve("npx --package some-pkg some-binary")
	require.Equal(t, "some-pkg", long.PackageName)
}

// mcp-remote's own documented invocation uses the -p form, so the proxy
// diversion has to survive it.
func TestResolve_MCPRemoteViaPackageFlag(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -p mcp-remote@latest mcp-remote-client https://remote.mcp.example/sse")
	require.Equal(t, identity.KindRemote, got.Kind)
	require.Equal(t, "url:https://remote.mcp.example/sse", got.ArtifactRef)
}

func TestResolve_PackageFlagWithNoValueIsUnresolved(t *testing.T) {
	t.Parallel()

	require.Equal(t, identity.KindUnresolved, identity.Resolve("npx -p").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("npx --package").Kind)
}

// A selector after the first bare word belongs to the server, not the
// launcher. `-p` is a common port flag, and reading it as npx's package
// selector resolved a real config to a nonexistent artifact.
func TestResolve_ServerFlagsAreNotLauncherSelectors(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y some-mcp-server -p 8080")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "some-mcp-server", got.PackageName)

	uv := identity.Resolve("uvx some-mcp-server --from trusted-pkg")
	require.Equal(t, "some-mcp-server", uv.PackageName)
}

// npx hands arguments after the package to the binary, so a trailing selector
// must not rename what actually ran — otherwise the review is keyed to a
// trusted package while an untrusted one executes.
func TestResolve_TrailingSelectorCannotRenameThePackage(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y @evil/mcp-server --package=@modelcontextprotocol/server-filesystem")
	require.Equal(t, "@evil/mcp-server", got.PackageName)

	uv := identity.Resolve("uvx evil-server --from=trusted-pkg")
	require.Equal(t, "evil-server", uv.PackageName)
}

// A wildcard component is a range; the same letter inside a prerelease or
// build tag is part of an exact version.
func TestResolve_PlatformSuffixedVersionsStayPinned(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{"npx pkg@1.0.0-linux", "npx pkg@2.0.0-osx", "npx pkg@1.0.0+build.x", "npx pkg@v1.2.3"} {
		require.True(t, identity.Resolve(spec).VersionPinned, "%s names one immutable release", spec)
	}

	for _, spec := range []string{"npx pkg@1.2.x", "npx pkg@1.x", "npx pkg@1.2.*"} {
		require.False(t, identity.Resolve(spec).VersionPinned, "%s floats", spec)
	}
}

// A port equal to the scheme default is redundant and must not key a second
// artifact for the same endpoint.
func TestResolve_DefaultPortsAreDropped(t *testing.T) {
	t.Parallel()

	require.Equal(t, identity.Resolve("https://mcp.example.com/sse").ArtifactRef, identity.Resolve("https://mcp.example.com:443/sse").ArtifactRef)
	require.Equal(t, identity.Resolve("http://mcp.example.com/sse").ArtifactRef, identity.Resolve("http://mcp.example.com:80/sse").ArtifactRef)

	// A non-default port still distinguishes the endpoint.
	require.NotEqual(t, identity.Resolve("https://mcp.example.com/sse").ArtifactRef, identity.Resolve("https://mcp.example.com:8443/sse").ArtifactRef)
}

// Hostname() strips the brackets an IPv6 literal needs, and a URL is malformed
// without them.
func TestResolve_PortlessIPv6KeepsItsBrackets(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("http://[::1]/sse")
	require.Equal(t, "url:http://[::1]/sse", got.ArtifactRef)
}

// npx ships as npx.cmd on Windows, and the canonical Windows stdio config
// launches it through the command processor.
func TestResolve_WindowsLauncherForms(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		`npx.cmd -y @scope/server`,
		`cmd /c npx -y @scope/server`,
		`cmd.exe /C npx.cmd -y @scope/server`,
		`NPX -y @scope/server`,
	} {
		got := identity.Resolve(command)
		require.Equal(t, identity.KindPackage, got.Kind, command)
		require.Equal(t, "@scope/server", got.PackageName, command)
	}
}

// Several selectors install several packages and the binary may come from any
// of them. Picking the first would attach evidence to the wrong artifact.
func TestResolve_MultiplePackageSelectorsAreUnresolved(t *testing.T) {
	t.Parallel()

	require.Equal(t, identity.KindUnresolved, identity.Resolve("npx -p pkg-a -p pkg-b some-binary").Kind)
	require.Equal(t, identity.KindUnresolved, identity.Resolve("npx --package=pkg-a --package=pkg-b some-binary").Kind)
}

// uv spells -p as --python and uses it to choose an interpreter, so npx's
// package-selector reading must not leak into the PyPI launchers.
func TestResolve_UvxPythonFlagIsNotAPackageSelector(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("uvx -p 3.12 mcp-server")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "mcp-server", got.PackageName, "-p selects the interpreter, not the package")

	joined := identity.Resolve("uvx -p=3.12 mcp-server")
	require.Equal(t, "mcp-server", joined.PackageName)
}

// npm and pip accept a URL in place of a registry name, and such a URL can
// carry a token. The artifact ref is stored and shown, so it must not persist
// one.
func TestResolve_CredentialsInAURLPackageSpecAreStripped(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("npx -y https://tok3n@github.example/org/pkg.git?auth=s3cret")
	require.NotContains(t, got.ArtifactRef, "tok3n")
	require.NotContains(t, got.ArtifactRef, "s3cret")
	require.NotContains(t, got.PackageName, "tok3n")
	require.NotContains(t, got.PackageName, "s3cret")
}

// pipx names the package with --spec, not uv's --from. Sharing one selector
// across the PyPI registry keyed the invocation to the command name.
func TestResolve_PipxSpecSelector(t *testing.T) {
	t.Parallel()

	got := identity.Resolve("pipx run --spec mcp-thing==1.0.0 some-command")
	require.Equal(t, identity.KindPackage, got.Kind)
	require.Equal(t, "mcp-thing", got.PackageName)
	require.Equal(t, "1.0.0", got.PackageVersion)
	require.True(t, got.VersionPinned)

	joined := identity.Resolve("pipx run --spec=mcp-thing==1.0.0 some-command")
	require.Equal(t, "mcp-thing", joined.PackageName)
}

// A selector belongs to the launcher that defines it. uvx has no --spec and
// pipx has no --from, so neither may be read for the other.
func TestResolve_SelectorsAreLauncherSpecific(t *testing.T) {
	t.Parallel()

	require.Equal(t, "real-pkg", identity.Resolve("uvx --from real-pkg some-command").PackageName)
	require.Equal(t, "real-pkg", identity.Resolve("pipx run --spec real-pkg some-command").PackageName)
}

// npm resolves a partial version as a range: `pkg@1.2` installs the newest
// 1.2.x, so it names no single release.
func TestResolve_NpmPartialVersionsAreNotPinned(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{"npx pkg@1", "npx pkg@1.2"} {
		require.False(t, identity.Resolve(spec).VersionPinned, "%s floats", spec)
	}

	require.True(t, identity.Resolve("npx pkg@1.2.3").VersionPinned)
}

// PyPI has no partial-version rule: a version only arrives through ==, which
// is exact at whatever precision it names.
func TestResolve_PyPIPartialExactVersionStaysPinned(t *testing.T) {
	t.Parallel()

	require.True(t, identity.Resolve("uvx mcp-thing==1.2").VersionPinned)
}

// A port is a number, so a zero-padded default is still the default.
func TestResolve_ZeroPaddedDefaultPortIsDropped(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		identity.Resolve("https://mcp.example.com/sse").ArtifactRef,
		identity.Resolve("https://mcp.example.com:0443/sse").ArtifactRef,
	)
}

// npm reads anything that is not valid semver as a dist-tag, and only forbids
// tags that are themselves valid semver — so `1.2.abc` is a legal tag name a
// publisher can repoint at will. A version-shaped string is not a pin.
func TestResolve_VersionShapedNpmTagsAreNotPinned(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{
		"npx pkg@1.2.abc",
		"npx pkg@1..3",
		"npx pkg@01.2.3",
		"npx pkg@1.2.3.4",
		"npx pkg@1.2.3-",
		"npx pkg@1.2.3+",
	} {
		got := identity.Resolve(spec)
		require.Equal(t, identity.KindPackage, got.Kind, spec)
		require.False(t, got.VersionPinned, "%s is not a complete semver and npm resolves it as a tag", spec)
	}
}

// Genuine semver, including prerelease and build metadata, still pins.
func TestResolve_CompleteSemverStillPins(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{
		"npx pkg@1.2.3",
		"npx pkg@0.0.1",
		"npx pkg@1.2.3-beta.1",
		"npx pkg@1.2.3+build.5",
		"npx pkg@1.2.3-rc.1+build.5",
		"npx pkg@v1.2.3",
	} {
		require.True(t, identity.Resolve(spec).VersionPinned, "%s names one immutable release", spec)
	}
}

// A launch command routinely embeds credentials. The redacted form keeps the
// structure that identifies the server — launcher, package, flags — while
// every secret-shaped value is removed, and it collapses whitespace so it
// doubles as a dedupe key.
func TestRedactCommand_StripsSecretShapedValues(t *testing.T) {
	t.Parallel()

	got := identity.RedactCommand(`FAKE_TOKEN=fabricated123 npx  -y mcp-remote https://mcp.example.com/sse?key=fabricated456 --header "Authorization: Bearer fabricated789" --api-key=fabricated000`)
	require.Equal(t,
		"FAKE_TOKEN=<redacted> npx -y mcp-remote https://mcp.example.com/sse --header=<redacted> --api-key=<redacted>",
		got)
}

// A secret flag with a separate value folds the redacted value into the flag
// (`--token=<redacted>`), never emitting it as a free-standing token, and
// non-secret flags keep theirs.
func TestRedactCommand_SeparateFlagValueIsRedacted(t *testing.T) {
	t.Parallel()

	got := identity.RedactCommand("npx -y some-server --token fabricated123 --port 8080")
	require.Equal(t, "npx -y some-server --token=<redacted> --port 8080", got)

	shortHeader := identity.RedactCommand(`npx -y some-server -H "X-Api-Key: fabricated456"`)
	require.Equal(t, "npx -y some-server -H=<redacted>", shortHeader)
}

// A credential flag placed before the package spec must not displace the
// package: the redacted value stays glued to its flag, so resolution of the
// stored form still reads the real package rather than `<redacted>`.
func TestRedactCommand_CredentialFlagBeforePackageKeepsResolution(t *testing.T) {
	t.Parallel()

	redacted := identity.RedactCommand("npx -y --token fabricated123 @scope/server@1.2.3")
	require.Equal(t, "npx -y --token=<redacted> @scope/server@1.2.3", redacted)
	require.Equal(t, "npm:@scope/server@1.2.3", identity.Resolve(redacted).ArtifactRef)
}

// A quoted environment value with spaces splits into several tokens; all of
// them are the secret, and every one must go.
func TestRedactCommand_QuotedEnvValueIsFullyConsumed(t *testing.T) {
	t.Parallel()

	got := identity.RedactCommand(`FAKE_TOKEN="fabricated one two" npx -y @scope/server`)
	require.Equal(t, "FAKE_TOKEN=<redacted> npx -y @scope/server", got)

	joined := identity.RedactCommand(`npx -y some-server --header="Authorization: Bearer fabricated123"`)
	require.Equal(t, "npx -y some-server --header=<redacted>", joined)
}

// A curl-style short option carries its value attached (`-Hvalue`,
// `-H"a b"`); the attached value is a credential and must not survive.
func TestRedactCommand_AttachedShortHeaderValueIsRedacted(t *testing.T) {
	t.Parallel()

	quoted := identity.RedactCommand(`npx -y some-server -H"Authorization: Bearer fabricated123"`)
	require.Equal(t, "npx -y some-server -H=<redacted>", quoted)

	bare := identity.RedactCommand("npx -y some-server -HX-Api-Key:fabricated456")
	require.Equal(t, "npx -y some-server -H=<redacted>", bare)
}

// A shell-quoted endpoint is still a URL: the quotes must not smuggle its
// userinfo and query tokens past redaction.
func TestRedactCommand_QuotedURLIsStillRedacted(t *testing.T) {
	t.Parallel()

	got := identity.RedactCommand(`npx -y mcp-remote 'https://user:fabricated@mcp.example.com/sse?key=fabricated123'`)
	require.Equal(t, "npx -y mcp-remote https://mcp.example.com/sse", got)

	joined := identity.RedactCommand(`npx -y some-server --url="https://mcp.example.com/sse?key=fabricated456"`)
	require.Equal(t, "npx -y some-server --url=https://mcp.example.com/sse", joined)
}

// An exact PyPI spec whose package name contains a secret marker is a package,
// not an environment assignment: the version pin must survive redaction.
func TestRedactCommand_PyPIExactSpecIsNotAnEnvAssignment(t *testing.T) {
	t.Parallel()

	got := identity.RedactCommand("uvx authlib==1.3.0")
	require.Equal(t, "uvx authlib==1.3.0", got)
	require.Equal(t, "pypi:authlib@1.3.0", identity.Resolve(got).ArtifactRef)
	require.True(t, identity.Resolve(got).VersionPinned)
}

// Rotated secrets must not split one server into two reviews: the same
// command with different tokens redacts to the same string.
func TestRedactCommand_RotatedTokensRedactIdentically(t *testing.T) {
	t.Parallel()

	first := identity.RedactCommand("MY_API_KEY=fabricated-aaa npx -y @scope/server --auth-token fabricated-bbb")
	second := identity.RedactCommand("MY_API_KEY=fabricated-ccc  npx -y @scope/server --auth-token fabricated-ddd")
	require.Equal(t, first, second)
}

// Redaction must not disturb what identity resolution reads: selector flags,
// package specs, and non-secret environment prefixes survive, and a URL-shaped
// token gets the same treatment RedactServerURL gives an endpoint.
func TestRedactCommand_KeepsNonSecretStructure(t *testing.T) {
	t.Parallel()

	got := identity.RedactCommand("NODE_ENV=production npx -y -p @scope/pkg@1.2.3 server-bin --verbose")
	require.Equal(t, "NODE_ENV=production npx -y -p @scope/pkg@1.2.3 server-bin --verbose", got)

	url := identity.RedactCommand("npx -y mcp-remote https://user:fabricated@mcp.example.com/sse#frag")
	require.Equal(t, "npx -y mcp-remote https://mcp.example.com/sse", url)

	require.Equal(t,
		identity.Resolve("npx -y @scope/pkg@1.2.3").ArtifactRef,
		identity.Resolve(identity.RedactCommand("npx -y @scope/pkg@1.2.3 --api-key fabricated")).ArtifactRef,
		"resolution reads the same package off the redacted form")
}
