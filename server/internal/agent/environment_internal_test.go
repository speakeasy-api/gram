package agent

import "testing"

// normalizeEnvironment is total: every input resolves to one of the three
// constants and never to "". The call sites compare against those constants by
// name, so an empty return would silently make an endpoint look like "no
// environment" and re-open the question of which table a heartbeat belongs in.
//
// The three inputs that collapse onto endpoint are the same thing as far as
// this server is concerned — an ordinary device, recorded the way it always
// was. An unrecognized kind degrades rather than erroring, because rejecting
// the poll would stop that device syncing plugins at all.
func TestNormalizeEnvironment_IsTotal(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	for name, tc := range map[string]struct {
		reported *string
		want     string
	}{
		"ephemeral":   {ptr("ephemeral"), environmentEphemeral},
		"server":      {ptr("server"), environmentServer},
		"endpoint":    {ptr("endpoint"), environmentEndpoint},
		"absent":      {nil, environmentEndpoint},
		"empty":       {ptr(""), environmentEndpoint},
		"whitespace":  {ptr("   "), environmentEndpoint},
		"padded":      {ptr("  EPHEMERAL  "), environmentEphemeral},
		"mixed case":  {ptr("Server"), environmentServer},
		"typo":        {ptr("ephemerial"), environmentEndpoint},
		"future kind": {ptr("kiosk"), environmentEndpoint},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := normalizeEnvironment(tc.reported)
			if got != tc.want {
				t.Errorf("normalizeEnvironment = %q, want %q", got, tc.want)
			}
			if got == "" {
				t.Error(`returned ""; call sites compare against the constants by name, so an empty value would read as "no environment"`)
			}
		})
	}
}
