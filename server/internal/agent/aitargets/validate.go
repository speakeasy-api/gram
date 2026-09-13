package aitargets

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
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

	// MaxGatewayClientEntries caps each gateway-client matcher list.
	MaxGatewayClientEntries = 16

	// MaxOAuthClientIDLength caps an OAuth client id or CIMD catalog URL.
	MaxOAuthClientIDLength = 512

	// MaxClientInfoNameLength caps a reported MCP client name.
	MaxClientInfoNameLength = 128
)

var (
	targetIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	bundleIDPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	binaryPattern      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	processNamePattern = regexp.MustCompile(`^[A-Za-z0-9 ._-]{1,64}$`)
	plistKeyPattern    = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	vendorKeyPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// ErrInvalidTarget is wrapped by every validation failure.
var ErrInvalidTarget = errors.New("invalid ai scan target")

// Validate checks the count bound, id uniqueness, and every per-target rule.
func Validate(targets []Target) error {
	if len(targets) > MaxTargets {
		return fmt.Errorf("%w: %d targets exceeds the maximum of %d", ErrInvalidTarget, len(targets), MaxTargets)
	}
	seen := make(map[string]struct{}, len(targets))
	// A gateway matcher answers "which tool is this caller?", so two targets
	// claiming the same matcher would make a block on either silently cover
	// the other. Claims are tracked per layer, because MatchGatewayCaller
	// resolves the layers in order and only a tie within one is ambiguous.
	vendorKeys := make(map[string]string, len(targets))
	clientIDs := make(map[string]string, len(targets))
	for _, target := range targets {
		if err := ValidateTarget(target); err != nil {
			return err
		}
		if _, duplicate := seen[target.ID]; duplicate {
			return fmt.Errorf("%w: duplicate target id %q", ErrInvalidTarget, target.ID)
		}
		seen[target.ID] = struct{}{}
		// Only served targets are matched against callers, so only they can
		// collide.
		if !target.Enabled {
			continue
		}
		for _, claim := range []struct {
			kind   string
			values []string
			owners map[string]string
		}{
			{kind: "CIMD vendor key", values: target.GatewayClient.CIMDVendorKeys, owners: vendorKeys},
			{kind: "OAuth client id", values: target.GatewayClient.OAuthClientIDs, owners: clientIDs},
		} {
			for _, value := range claim.values {
				if owner, taken := claim.owners[value]; taken {
					return fmt.Errorf("%w: targets %q and %q both claim the %s %q", ErrInvalidTarget, owner, target.ID, claim.kind, value)
				}
				claim.owners[value] = target.ID
			}
		}
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
	return validateGatewayClient(target.Category, target.GatewayClient, fail)
}

func isSpaceOrControl(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}

// validateGatewayClient checks the matcher lists. They are held to shapes the
// server can actually produce — a catalog vendor key, or a CIMD document URL
// it can verify — so a matcher that could never match is rejected at write
// time rather than read as a silently inert block.
func validateGatewayClient(category Category, gateway GatewayClient, fail func(string, ...any) error) error {
	// A harness and an assistant both call Gram's MCP gateway, so both can
	// name a caller. An open model run locally is software on a laptop and
	// nothing else, so a matcher on one would name a caller that cannot exist.
	if !CallsGateway(category) && !gateway.IsZero() {
		return fail("only a harness or assistant can carry gateway client matchers; %q never calls the MCP gateway", category)
	}
	for _, list := range [][]string{gateway.CIMDVendorKeys, gateway.OAuthClientIDs, gateway.ClientInfoNames} {
		if len(list) > MaxGatewayClientEntries {
			return fail("a gateway client list may hold at most %d entries", MaxGatewayClientEntries)
		}
	}
	for _, key := range gateway.CIMDVendorKeys {
		if !vendorKeyPattern.MatchString(key) {
			return fail("cimd vendor key %q must match %s", key, vendorKeyPattern)
		}
	}
	// Blocking is CIMD-only, and a CIMD client_id IS the https URL its
	// document is served from. Holding the column to that shape is what keeps
	// the rule true in the data rather than only in the matcher: a
	// dynamically registered client's opaque id cannot be written here at
	// all, so nobody can record a block that looks real and covers one
	// laptop's registration.
	for _, id := range gateway.OAuthClientIDs {
		switch {
		case id == "":
			return fail("client id metadata document url must not be empty")
		case utf8.RuneCountInString(id) > MaxOAuthClientIDLength:
			return fail("client id metadata document url %q exceeds %d characters", id, MaxOAuthClientIDLength)
		case strings.ContainsFunc(id, isSpaceOrControl):
			return fail("client id metadata document url %q must not contain whitespace or control characters", id)
		case !strings.HasPrefix(id, "https://"):
			return fail("client id metadata document url %q must be an https URL; only CIMD-published clients can be blocked at the gateway", id)
		}
	}
	for _, name := range gateway.ClientInfoNames {
		switch {
		case strings.TrimSpace(name) == "":
			return fail("client info name must not be blank")
		case utf8.RuneCountInString(name) > MaxClientInfoNameLength:
			return fail("client info name %q exceeds %d characters", name, MaxClientInfoNameLength)
		}
	}
	return nil
}
