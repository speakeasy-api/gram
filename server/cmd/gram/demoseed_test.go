package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestDemoSeedLocalRequiresSiteURLBeforeConnecting(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "/relative", "ftp://example.test", "https://user@example.test", "https://example.test?query", "https://example.test#fragment", "http://public.example.test"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("demo-seed", flag.ContinueOnError)
			flags.Bool("local", true, "")
			flags.String("site-url", raw, "")
			err := newDemoSeedCommand().Action(cli.NewContext(nil, flags, nil))
			require.ErrorContains(t, err, "local seed site-url")
		})
	}
}

func TestDemoSeedSiteURLFromEnvironment(t *testing.T) {
	t.Setenv("GRAM_SITE_URL", "https://public.example.test")
	flags := flag.NewFlagSet("demo-seed", flag.ContinueOnError)
	for _, f := range newDemoSeedCommand().Flags {
		if f.Names()[0] == "site-url" {
			require.NoError(t, f.Apply(flags))
			siteURL, err := parseSiteURL(cli.NewContext(nil, flags, nil).String("site-url"))
			require.NoError(t, err)
			require.Equal(t, "https://public.example.test", siteURL.String())
			return
		}
	}
	t.Fatal("site-url flag is missing")
}

func TestAssetConsumersRequireValidSiteURLBeforeConnecting(t *testing.T) {
	t.Parallel()
	for _, command := range []*cli.Command{newWorkerCommand(), newAppSeedCommand()} {
		for _, raw := range []string{"", "http://public.example.test", "https://user@example.test", "https://example.test?query", "https://example.test#fragment"} {
			flags := flag.NewFlagSet(command.Name, flag.ContinueOnError)
			flags.String("site-url", raw, "")
			err := command.Action(cli.NewContext(nil, flags, nil))
			require.Error(t, err, command.Name)
			require.Contains(t, err.Error(), "URL", command.Name)
		}
	}
}
