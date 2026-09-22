package deviceidentity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
)

func ptr(s string) *string { return &s }

// NormalizeEnvironment is total: every input resolves to one of the three
// constants and never to "". Call sites compare against those constants by
// name, so an empty return would silently make an endpoint look like "no
// environment" and re-open the question of which table a heartbeat belongs
// in.
//
// The three inputs that collapse onto endpoint are the same thing as far as
// this server is concerned — an ordinary device, recorded the way it always
// was. An unrecognized kind degrades rather than erroring, because rejecting
// the poll would stop that device syncing plugins at all.
func TestNormalizeEnvironmentIsTotal(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		reported *string
		want     string
	}{
		"ephemeral":   {ptr("ephemeral"), deviceidentity.EnvironmentEphemeral},
		"server":      {ptr("server"), deviceidentity.EnvironmentServer},
		"endpoint":    {ptr("endpoint"), deviceidentity.EnvironmentEndpoint},
		"absent":      {nil, deviceidentity.EnvironmentEndpoint},
		"empty":       {ptr(""), deviceidentity.EnvironmentEndpoint},
		"whitespace":  {ptr("   "), deviceidentity.EnvironmentEndpoint},
		"padded":      {ptr("  EPHEMERAL  "), deviceidentity.EnvironmentEphemeral},
		"mixed case":  {ptr("Server"), deviceidentity.EnvironmentServer},
		"typo":        {ptr("ephemerial"), deviceidentity.EnvironmentEndpoint},
		"future kind": {ptr("kiosk"), deviceidentity.EnvironmentEndpoint},
	} {
		got := deviceidentity.NormalizeEnvironment(tc.reported)
		require.Equal(t, tc.want, got, "input %q", name)
		require.NotEmpty(t, got, `input %q returned ""; call sites compare against the constants by name, so an empty value would read as "no environment"`, name)
	}
}

// A serial is the dedup key for per-device heartbeats and the grouping key for
// any device count taken over request logs, so both sides must collapse casing
// and whitespace identically or one machine reads as two devices.
func TestNormalizeSerialCanonicalizes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		reported *string
		want     string
	}{
		"plain":       {ptr("c02xk1abcdef"), "c02xk1abcdef"},
		"upper case":  {ptr("C02XK1ABCDEF"), "c02xk1abcdef"},
		"padded":      {ptr("  C02XK1ABCDEF \t"), "c02xk1abcdef"},
		"absent":      {nil, ""},
		"empty":       {ptr(""), ""},
		"whitespace":  {ptr("   "), ""},
		"punctuation": {ptr("FVFX-1234/5678"), "fvfx-1234/5678"},
	} {
		require.Equal(t, tc.want, deviceidentity.NormalizeSerial(tc.reported), "input %q", name)
	}
}

// SMBIOS/DMI defaults are shared by many distinct machines, so they can neither
// attest a device nor be counted as one.
func TestNormalizeSerialRejectsPlaceholders(t *testing.T) {
	t.Parallel()

	for _, reported := range []string{
		"To be filled by O.E.M.",
		"to be filled by oem",
		"Default string",
		"System Serial Number",
		"Not Specified",
		"Not Applicable",
		"unknown",
		"None",
		"N/A",
		"invalid",
		"0",
		"123456789",
		"0123456789",
		"Serial Number",
		"OEM",
		"o.e.m.",
		"  DEFAULT STRING  ",
	} {
		require.Empty(t, deviceidentity.NormalizeSerial(&reported), "placeholder %q must not serve as a device identity", reported)
	}
}
