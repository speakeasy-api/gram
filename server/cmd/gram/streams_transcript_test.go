package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// Flag names must be unique across the streams command.
//
// urfave/cli applies every flag to one flag.FlagSet, and the standard library
// panics on a redefinition — so a duplicate is not a startup warning, it is the
// streams process failing to boot at all. That is easy to reintroduce by
// merging: two branches that each add the same flag conflict at different
// offsets in the slice, so git takes both additions without reporting a
// conflict and nothing catches it until the command is constructed.
func TestStreamsCommand_FlagNamesAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for _, f := range newStreamsCommand().Flags {
		for _, name := range f.Names() {
			seen[name]++
		}
	}

	for name, count := range seen {
		require.Equalf(t, 1, count,
			"flag %q is defined %d times; urfave/cli applies all of them to one FlagSet and the duplicate panics at startup",
			name, count)
	}
}

// The transcript writer's wakes depend on Temporal, and the streams command is
// what supplies it. Asserting the flags exist keeps the two in step: the
// command reads them to build the client it hands to newTranscriptWriter, so a
// rename that misses one turns configured wakes into a failure to start.
func TestStreamsCommand_DefinesTemporalFlags(t *testing.T) {
	t.Parallel()

	defined := map[string]bool{}
	for _, f := range newStreamsCommand().Flags {
		if sf, ok := f.(*cli.StringFlag); ok {
			defined[sf.Name] = true
		}
	}

	for _, name := range []string{
		"temporal-address",
		"temporal-namespace",
		"temporal-task-queue",
		"temporal-client-cert",
		"temporal-client-key",
	} {
		require.Truef(t, defined[name], "streams command must define %q", name)
	}
}
