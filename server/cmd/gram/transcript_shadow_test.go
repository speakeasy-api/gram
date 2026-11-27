package gram

import (
	"flag"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func newTranscriptShadowCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()
	c := newOrgEncryptionCLIContext(t, nil)
	set := flag.NewFlagSet("transcript-shadow", flag.ContinueOnError)
	// Apply the flags without their environment variables, so a developer's
	// shell cannot change what these tests see. Parallel tests cannot use
	// t.Setenv, so the isolation lives here.
	for _, f := range []cli.Flag{transcriptShadowFlag()} {
		sliceFlag, ok := f.(*cli.StringSliceFlag)
		require.True(t, ok)
		withoutEnv := *sliceFlag
		withoutEnv.EnvVars = nil
		require.NoError(t, withoutEnv.Apply(set))
	}
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}
	return cli.NewContext(c.App, set, c)
}

func TestTranscriptShadowDisabled(t *testing.T) {
	t.Setenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS", "")
	require.NoError(t, os.Unsetenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"))
	c := newTranscriptShadowCLIContext(t, nil)
	require.Empty(t, c.StringSlice(chatTranscriptShadowProjectIDsFlag))
	shadow, err := configureTranscriptShadow(t.Context(), c, nil, nil, orgEncryptionDeps{}, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.Nil(t, shadow.Store)
	require.Empty(t, shadow.ProjectIDs)
}

func TestTranscriptShadowProjectIDs(t *testing.T) {
	t.Parallel()
	first := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	second := uuid.MustParse("10000000-0000-4000-8000-000000000002")
	ids, err := parseTranscriptShadowProjectIDs([]string{first.String(), " " + second.String() + " ", first.String()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, ids)
	ids, err = parseTranscriptShadowProjectIDs([]string{first.String()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first}, ids)
	for _, value := range []string{"", " ", "not-a-uuid", uuid.Nil.String()} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, err := parseTranscriptShadowProjectIDs([]string{first.String(), value})
			require.Error(t, err)
			require.NotContains(t, err.Error(), first.String())
		})
	}
}

func TestTranscriptShadowRejectsInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"", " ", "not-a-uuid", uuid.Nil.String(), "10000000-0000-4000-8000-000000000001,"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS", "")
			require.NoError(t, os.Unsetenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"))
			c := newTranscriptShadowCLIContext(t, map[string]string{chatTranscriptShadowProjectIDsFlag: value})
			_, err := configureTranscriptShadow(t.Context(), c, nil, nil, orgEncryptionDeps{}, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
			require.Error(t, err)
		})
	}
}

func TestTranscriptShadowFlags(t *testing.T) {
	t.Parallel()
	for _, item := range append(orgEncryptionFlags(), orgEncryptionServiceFlags()...) {
		require.NotContains(t, item.Names(), chatTranscriptShadowProjectIDsFlag)
	}
	for _, command := range []*cli.Command{newAdminCommand(), newNetworkIngressWorkerCommand()} {
		for _, item := range command.Flags {
			require.NotContains(t, item.Names(), chatTranscriptShadowProjectIDsFlag, command.Name)
		}
	}
	for _, command := range []*cli.Command{newStartCommand(), newWorkerCommand(), newMCPCommand(), newNetworkIngressServerCommand()} {
		requireFlag(t, command.Flags, chatTranscriptShadowProjectIDsFlag)
	}
}
