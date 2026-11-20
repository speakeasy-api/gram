package gram

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestTranscriptShadowDisabled(t *testing.T) {
	t.Setenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS", "")
	require.NoError(t, os.Unsetenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"))
	c := newOrgEncryptionCLIContext(t, nil)
	require.Empty(t, c.StringSlice(chatTranscriptShadowProjectIDsFlag))
	require.NoError(t, configureTranscriptShadow(t.Context(), c, nil, nil, orgEncryptionDeps{}))
}

func TestTranscriptShadowProjectIDs(t *testing.T) {
	t.Parallel()
	first := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	second := uuid.MustParse("10000000-0000-4000-8000-000000000002")
	ids, err := parseTranscriptShadowProjectIDs([]string{first.String(), " " + second.String() + " ", first.String()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, ids)
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
	for _, value := range []string{"", " ", "not-a-uuid", uuid.Nil.String(), "10000000-0000-4000-8000-000000000001,", "10000000-0000-4000-8000-000000000001"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS", "")
			require.NoError(t, os.Unsetenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"))
			c := newOrgEncryptionCLIContext(t, map[string]string{chatTranscriptShadowProjectIDsFlag: value})
			require.Error(t, configureTranscriptShadow(t.Context(), c, nil, nil, orgEncryptionDeps{}))
		})
	}
}

func TestTranscriptShadowFlags(t *testing.T) {
	t.Parallel()
	for _, item := range orgEncryptionServiceFlags() {
		require.NotContains(t, item.Names(), chatTranscriptShadowProjectIDsFlag)
	}
	for _, command := range []*cli.Command{newStartCommand(), newWorkerCommand(), newMCPCommand(), newNetworkIngressServerCommand()} {
		requireFlag(t, command.Flags, chatTranscriptShadowProjectIDsFlag)
	}
}
