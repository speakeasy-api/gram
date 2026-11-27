package gram

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTranscriptShadowCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()
	return newTranscriptShadowCLIContextIn(t, "", values)
}

func newTranscriptShadowCLIContextIn(t *testing.T, environment string, values map[string]string) *cli.Context {
	t.Helper()
	c := newOrgEncryptionCLIContext(t, map[string]string{"environment": environment})
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
	shadow, err := configureTranscripts(t.Context(), c, nil, nil, orgEncryptionDeps{}, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.Nil(t, shadow)
	require.False(t, shadow.Enabled(uuid.New()))
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
			_, err := configureTranscripts(t.Context(), c, nil, nil, orgEncryptionDeps{}, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
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

// openOne opens one loaded message of a listed project through the store's
// read path.
func openOne(t *testing.T, transcripts *transcriptstore.Transcripts, projectID uuid.UUID) error {
	t.Helper()
	rows := []uuid.UUID{uuid.New()}
	//nolint:wrapcheck // the test checks the store's own error.
	return transcriptstore.OpenChatMessages(t.Context(), transcripts, "org", projectID, rows, func(id *uuid.UUID) transcriptstore.OpenTarget {
		return transcriptstore.OpenTarget{MessageID: *id, Set: func(transcriptcrypto.ProtectedFields) {}}
	})
}

func TestReadOnlyTranscripts(t *testing.T) {
	t.Parallel()
	none, err := configureReadOnlyTranscripts(newTranscriptShadowCLIContext(t, nil), testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.Nil(t, none, "no project keeps shadows")

	listed := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	c := newTranscriptShadowCLIContext(t, map[string]string{chatTranscriptShadowProjectIDsFlag: listed.String()})
	transcripts, err := configureReadOnlyTranscripts(c, testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.NotNil(t, transcripts)
	require.False(t, transcripts.Enabled(listed), "a read-only process cannot open shadows")

	_, err = configureReadOnlyTranscripts(newTranscriptShadowCLIContext(t, map[string]string{chatTranscriptShadowProjectIDsFlag: "not-a-uuid"}), testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.Error(t, err)
}

func TestReadOnlyTranscriptsStrictInLocalDevelopment(t *testing.T) {
	t.Parallel()
	listed := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	values := map[string]string{chatTranscriptShadowProjectIDsFlag: listed.String()}

	local, err := configureReadOnlyTranscripts(newTranscriptShadowCLIContextIn(t, "local", values), testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.ErrorIs(t, openOne(t, local, listed), transcriptstore.ErrPlaintextRead, "local development rejects the plaintext read")

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	prod, err := configureReadOnlyTranscripts(newTranscriptShadowCLIContextIn(t, "prod", values), testenv.NewLogger(t), provider)
	require.NoError(t, err)
	require.NoError(t, openOne(t, prod, listed), "production serves, logs and counts")

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	var points []metricdata.DataPoint[int64]
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "chat.transcript_shadow.plaintext_reads" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			points = sum.DataPoints
		}
	}
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
	require.Equal(t, attribute.NewSet(
		attr.ProjectID(listed.String()),
		attr.TranscriptShadowReader("OpenChatMessages"),
		attr.Outcome("unopened"),
	), points[0].Attributes)

	blank := newTranscriptShadowCLIContextIn(t, "prod", map[string]string{chatTranscriptShadowProjectIDsFlag: ""})
	_, err = configureReadOnlyTranscripts(blank, testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.Error(t, err, "an explicitly blank list must not disable reporting")
}

func TestReaderTranscriptsFallBackWithoutOrgEncryption(t *testing.T) {
	t.Parallel()
	listed := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	c := newTranscriptShadowCLIContextIn(t, "local", map[string]string{chatTranscriptShadowProjectIDsFlag: listed.String()})
	transcripts, err := configureReaderTranscripts(t.Context(), c, nil, orgEncryptionDeps{}, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.NotNil(t, transcripts)
	require.False(t, transcripts.Enabled(listed), "without organization encryption streams cannot open shadows")
	require.ErrorIs(t, openOne(t, transcripts, listed), transcriptstore.ErrPlaintextRead, "so its reads go through the guard")
}
