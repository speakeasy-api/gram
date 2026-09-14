package aitargets_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

func validTarget() aitargets.Target {
	return aitargets.Target{
		ID:          "chatgpt-classic",
		DisplayName: "ChatGPT Classic",
		Category:    aitargets.CategoryHarness,
		Signatures: aitargets.Signatures{
			BundleIDs:    []string{"com.openai.chat"},
			Binaries:     []string{},
			ConfigDirs:   []string{},
			ProcessNames: []string{},
		},
		VersionHint: nil,
		Enabled:     true,
	}
}

func TestValidateTargetAcceptsAValidTarget(t *testing.T) {
	t.Parallel()
	require.NoError(t, aitargets.ValidateTarget(validTarget()))
}

func TestValidateTargetAcceptsEveryInstallSignatureKind(t *testing.T) {
	t.Parallel()

	byBinary := validTarget()
	byBinary.Signatures = aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{"claude"}, ConfigDirs: []string{}, ProcessNames: []string{"claude"}}
	require.NoError(t, aitargets.ValidateTarget(byBinary))

	byConfigDir := validTarget()
	byConfigDir.Signatures = aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{"~/.config/opencode"}, ProcessNames: []string{}}
	require.NoError(t, aitargets.ValidateTarget(byConfigDir))

	byAbsoluteDir := validTarget()
	byAbsoluteDir.Signatures = aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{"/opt/homebrew/etc/opencode"}, ProcessNames: []string{}}
	require.NoError(t, aitargets.ValidateTarget(byAbsoluteDir))

	withHint := validTarget()
	withHint.VersionHint = &aitargets.VersionHint{PlistKey: "CFBundleVersion"}
	require.NoError(t, aitargets.ValidateTarget(withHint))

	spacedProcess := validTarget()
	spacedProcess.Signatures.ProcessNames = []string{"LM Studio"}
	require.NoError(t, aitargets.ValidateTarget(spacedProcess))
}

func TestValidateTargetRejectsEachRule(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*aitargets.Target){
		"uppercase id":               func(x *aitargets.Target) { x.ID = "ChatGPT" },
		"id with space":              func(x *aitargets.Target) { x.ID = "chat gpt" },
		"id starting with hyphen":    func(x *aitargets.Target) { x.ID = "-chatgpt" },
		"id too long":                func(x *aitargets.Target) { x.ID = strings.Repeat("a", 65) },
		"empty display name":         func(x *aitargets.Target) { x.DisplayName = "" },
		"padded display name":        func(x *aitargets.Target) { x.DisplayName = " ChatGPT " },
		"display name too long":      func(x *aitargets.Target) { x.DisplayName = strings.Repeat("a", 129) },
		"unknown category":           func(x *aitargets.Target) { x.Category = "future_category" },
		"too many bundle ids":        func(x *aitargets.Target) { x.Signatures.BundleIDs = repeat("com.example.app", 17) },
		"bundle id with slash":       func(x *aitargets.Target) { x.Signatures.BundleIDs = []string{"com/openai/chat"} },
		"empty bundle id":            func(x *aitargets.Target) { x.Signatures.BundleIDs = []string{""} },
		"binary with path separator": func(x *aitargets.Target) { x.Signatures.Binaries = []string{"../../etc/passwd"} },
		"binary absolute path":       func(x *aitargets.Target) { x.Signatures.Binaries = []string{"/usr/bin/claude"} },
		"binary dot":                 func(x *aitargets.Target) { x.Signatures.Binaries = []string{"."} },
		"binary dot dot":             func(x *aitargets.Target) { x.Signatures.Binaries = []string{".."} },
		"process name regex meta":    func(x *aitargets.Target) { x.Signatures.ProcessNames = []string{".*"} },
		"process name with slash":    func(x *aitargets.Target) { x.Signatures.ProcessNames = []string{"bin/claude"} },
		"plist key with dot":         func(x *aitargets.Target) { x.VersionHint = &aitargets.VersionHint{PlistKey: "CF.Bundle"} },
		"empty plist key":            func(x *aitargets.Target) { x.VersionHint = &aitargets.VersionHint{PlistKey: ""} },
		"no install signature": func(x *aitargets.Target) {
			x.Signatures = aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{"ChatGPT"}}
		},
	}
	for name, mutate := range cases {
		target := validTarget()
		mutate(&target)
		err := aitargets.ValidateTarget(target)
		require.Error(t, err, "case %q must be rejected", name)
		require.ErrorIs(t, err, aitargets.ErrInvalidTarget, "case %q must wrap ErrInvalidTarget", name)
	}
}

// A config dir is whatever the device agent can resolve: a home-relative
// prefix or an absolute path, however it was written. The agent only checks
// whether the directory exists, so the shape is not worth policing here.
func TestValidateTargetAcceptsAnyConfigDirShape(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{
		".claude",
		"~/.claude",
		"~/.claude/",
		"/opt/homebrew/etc/claude",
		"~/Library/Application Support/com.openai.chat/",
		"Library/Application Support/Cursor",
	} {
		target := validTarget()
		target.Signatures.ConfigDirs = []string{dir}
		require.NoError(t, aitargets.ValidateTarget(target), "config dir %q", dir)
	}
}

func TestValidateRejectsDuplicateIDsAndOversizedLists(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, aitargets.Validate([]aitargets.Target{validTarget(), validTarget()}), aitargets.ErrInvalidTarget)

	oversized := make([]aitargets.Target, 0, aitargets.MaxTargets+1)
	for i := range aitargets.MaxTargets + 1 {
		target := validTarget()
		target.ID = "target-" + strings.Repeat("a", i%10) + "-" + strings.Repeat("b", i/10)
		oversized = append(oversized, target)
	}
	require.ErrorIs(t, aitargets.Validate(oversized), aitargets.ErrInvalidTarget)
}

func repeat(value string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = value
	}
	return out
}

// TestValidateTargetRejectsMatchersTheGatewayCannotResolve pins parity with
// cimd.ValidateClientIDURL, the authorization-time gate.
//
// A matcher this accepts and that gate rejects is the worst shape on offer:
// the target is reported enforceable, an administrator stores a block, and no
// client id matching it can ever reach the gateway. The block reads as active
// and silently never fires. The cross-check below fails if the two validators
// ever drift apart on one of these.
func TestValidateTargetRejectsMatchersTheGatewayCannotResolve(t *testing.T) {
	t.Parallel()

	for _, clientID := range []string{
		"https://client.example/%gh",      // malformed percent-escape
		"https://client.example/%zz/x",    // malformed percent-escape mid-path
		"https://client.example/%2e%2e/x", // dot segment hidden behind escapes
		"https://client.example/../x",     // literal dot segment
		"https://client.example",          // bare origin, no path
		"https://user@client.example/x",   // userinfo component
		"https://client.example/x#frag",   // fragment
		"http://client.example/x",         // not https
	} {
		t.Run(clientID, func(t *testing.T) {
			t.Parallel()

			target := validTarget()
			target.GatewayClient = aitargets.GatewayClient{
				CIMDVendorKeys:  nil,
				OAuthClientIDs:  []string{clientID},
				ClientInfoNames: nil,
			}
			require.ErrorIsf(t, aitargets.ValidateTarget(target), aitargets.ErrInvalidTarget,
				"%q must not be storable as a gateway matcher", clientID)

			_, err := cimd.ValidateClientIDURL(clientID)
			require.Errorf(t, err,
				"%q is rejected here but accepted at authorization; the two validators have drifted", clientID)
		})
	}
}

// TestValidateTargetAcceptsALegallyEscapedMatcher is the other half: the parse
// added above must reject malformed escapes without rejecting valid ones.
func TestValidateTargetAcceptsALegallyEscapedMatcher(t *testing.T) {
	t.Parallel()

	const clientID = "https://client.example/a%20b/client.json"

	target := validTarget()
	target.GatewayClient = aitargets.GatewayClient{
		CIMDVendorKeys:  nil,
		OAuthClientIDs:  []string{clientID},
		ClientInfoNames: nil,
	}
	require.NoError(t, aitargets.ValidateTarget(target))

	_, err := cimd.ValidateClientIDURL(clientID)
	require.NoError(t, err, "the authorization-time gate must accept it too")
}
