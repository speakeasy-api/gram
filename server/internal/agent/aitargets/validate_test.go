package aitargets_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
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
		"unknown category":           func(x *aitargets.Target) { x.Category = "assistant" },
		"too many bundle ids":        func(x *aitargets.Target) { x.Signatures.BundleIDs = repeat("com.example.app", 17) },
		"bundle id with slash":       func(x *aitargets.Target) { x.Signatures.BundleIDs = []string{"com/openai/chat"} },
		"empty bundle id":            func(x *aitargets.Target) { x.Signatures.BundleIDs = []string{""} },
		"binary with path separator": func(x *aitargets.Target) { x.Signatures.Binaries = []string{"../../etc/passwd"} },
		"binary absolute path":       func(x *aitargets.Target) { x.Signatures.Binaries = []string{"/usr/bin/claude"} },
		"binary dot":                 func(x *aitargets.Target) { x.Signatures.Binaries = []string{"."} },
		"binary dot dot":             func(x *aitargets.Target) { x.Signatures.Binaries = []string{".."} },
		"config dir absolute":        func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"/etc"} },
		"config dir home only":       func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~/"} },
		"config dir bare tilde":      func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~"} },
		"config dir parent walk":     func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~/../.ssh"} },
		"config dir dot segment":     func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~/./.ssh"} },
		"config dir empty segment":   func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~/.claude//x"} },
		"config dir trailing slash":  func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~/.claude/"} },
		"config dir backslash":       func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{`~/.claude\x`} },
		"config dir too long":        func(x *aitargets.Target) { x.Signatures.ConfigDirs = []string{"~/" + strings.Repeat("a", 256)} },
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
