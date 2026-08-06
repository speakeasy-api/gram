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
