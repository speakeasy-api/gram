package aitargets

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// These bounds are mirrored by the device agent's decode-time validator and
// the Goa design; keep the three in step.
const (
	// MaxTargets caps the served list.
	MaxTargets = 200

	// MaxSignatureEntries caps each signature list on a target.
	MaxSignatureEntries = 16

	// MaxDisplayNameLength caps the display name.
	MaxDisplayNameLength = 128

	// MaxEnvelopeBytes caps the encoded served list, so a catalog can never
	// turn every plugin poll into a large payload.
	MaxEnvelopeBytes = 256 * 1024
)

var (
	targetIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	bundleIDPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	binaryPattern      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	processNamePattern = regexp.MustCompile(`^[A-Za-z0-9 ._-]{1,64}$`)
	plistKeyPattern    = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
)

// ErrInvalidTarget is wrapped by every validation failure.
var ErrInvalidTarget = errors.New("invalid ai scan target")

// Validate checks the count bound, id uniqueness, and every per-target rule.
func Validate(targets []Target) error {
	if len(targets) > MaxTargets {
		return fmt.Errorf("%w: %d targets exceeds the maximum of %d", ErrInvalidTarget, len(targets), MaxTargets)
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if err := ValidateTarget(target); err != nil {
			return err
		}
		if _, duplicate := seen[target.ID]; duplicate {
			return fmt.Errorf("%w: duplicate target id %q", ErrInvalidTarget, target.ID)
		}
		seen[target.ID] = struct{}{}
	}
	return nil
}

// ValidateServed checks the set that would be served after a write: the
// per-target rules plus the count and encoded-size caps agents enforce. The
// size is measured at the widest list_version so a later revision cannot
// push an accepted set over the cap.
func ValidateServed(targets []Target) error {
	if err := Validate(targets); err != nil {
		return err
	}
	data, err := json.Marshal(NewSnapshot(math.MaxInt32, targets).Envelope())
	if err != nil {
		return fmt.Errorf("encode served list: %w", err)
	}
	if len(data) > MaxEnvelopeBytes {
		return fmt.Errorf("%w: served list is %d bytes, over the %d byte cap", ErrInvalidTarget, len(data), MaxEnvelopeBytes)
	}
	return nil
}

// ValidateTarget checks one target against the rules the device agent
// enforces on receipt.
func ValidateTarget(target Target) error {
	if !targetIDPattern.MatchString(target.ID) {
		return fmt.Errorf("%w: id %q must match %s", ErrInvalidTarget, target.ID, targetIDPattern)
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: target %q: %s", ErrInvalidTarget, target.ID, fmt.Sprintf(format, args...))
	}

	name := strings.TrimSpace(target.DisplayName)
	if name == "" || name != target.DisplayName || utf8.RuneCountInString(target.DisplayName) > MaxDisplayNameLength {
		return fail("display name must be 1-%d characters with no surrounding whitespace", MaxDisplayNameLength)
	}
	if !slices.Contains(KnownCategories(), target.Category) {
		return fail("unknown category %q", target.Category)
	}

	sig := target.Signatures
	for _, list := range [][]string{sig.BundleIDs, sig.Binaries, sig.ConfigDirs, sig.ProcessNames} {
		if len(list) > MaxSignatureEntries {
			return fail("a signature list may hold at most %d entries", MaxSignatureEntries)
		}
	}
	for _, id := range sig.BundleIDs {
		if !bundleIDPattern.MatchString(id) {
			return fail("bundle id %q must match %s", id, bundleIDPattern)
		}
	}
	for _, bin := range sig.Binaries {
		if !binaryPattern.MatchString(bin) || bin == "." || bin == ".." {
			return fail("binary %q must be a bare command name matching %s", bin, binaryPattern)
		}
	}
	for _, proc := range sig.ProcessNames {
		if !processNamePattern.MatchString(proc) {
			return fail("process name %q must match %s", proc, processNamePattern)
		}
	}
	if target.VersionHint != nil && !plistKeyPattern.MatchString(target.VersionHint.PlistKey) {
		return fail("version hint plist key %q must match %s", target.VersionHint.PlistKey, plistKeyPattern)
	}
	if len(sig.BundleIDs)+len(sig.Binaries)+len(sig.ConfigDirs) == 0 {
		return fail("at least one install signature (bundle id, binary, or config dir) is required")
	}
	return nil
}
