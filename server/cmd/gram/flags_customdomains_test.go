package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestCustomDomainCNAMEFromCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "trailing dot", value: "cname.getgram.ai.", want: "cname.getgram.ai"},
		{name: "mixed case and whitespace", value: " CName.GetGram.AI ", want: "cname.getgram.ai"},
		{name: "already normalized", value: "cname.getgram.ai", want: "cname.getgram.ai"},
		{name: "unset", value: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("customdomains", flag.ContinueOnError)
			for _, f := range customDomainFlags() {
				require.NoError(t, f.Apply(flags))
			}
			require.NoError(t, flags.Set("custom-domain-cname", tc.value))
			ctx := cli.NewContext(cli.NewApp(), flags, nil)

			require.Equal(t, tc.want, customDomainCNAMEFromCLI(ctx))
		})
	}
}
