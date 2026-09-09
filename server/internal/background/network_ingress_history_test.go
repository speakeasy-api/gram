package background

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const ingressHistorySecret = "synthetic-history-secret-not-a-real-credential"

func TestNetworkIngressRealHistoryOmitsCredentialsOnSuccess(t *testing.T) {
	t.Parallel()
	verifyIngressHistorySecrecy(t, false)
}

func TestNetworkIngressRealHistoryOmitsCredentialsOnProviderFailure(t *testing.T) {
	t.Parallel()
	verifyIngressHistorySecrecy(t, true)
}

func verifyIngressHistorySecrecy(t *testing.T, failing bool) {
	t.Helper()
	c, id, provider, logs := newIngressHistoryTest(t, failing)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	run, err := c.start(ctx, id)
	require.NoError(t, err)
	err = run.Get(ctx, nil)
	if failing {
		require.Error(t, err)
		require.NotContains(t, err.Error(), ingressHistorySecret)
	} else {
		require.NoError(t, err)
	}
	select {
	case credentials := <-provider.credentialSeen:
		require.Contains(t, string(credentials), ingressHistorySecret, "activity must actually decrypt the sentinel")
	default:
		t.Fatal("provider never received decrypted credentials")
	}
	history := c.Client.GetWorkflowHistory(ctx, run.GetID(), run.GetRunID(), false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	events := 0
	activityScheduled := false
	var captured strings.Builder
	for history.HasNext() {
		event, err := history.Next()
		require.NoError(t, err)
		payload, err := protojson.Marshal(event)
		require.NoError(t, err)
		captured.Write(payload)
		// protojson encodes payload bytes in base64. Scan the wire event too,
		// where plaintext inside failure details and payload data stays visible.
		require.NotContains(t, event.String(), ingressHistorySecret)
		require.NotContains(t, event.String(), "client_secret")
		activityScheduled = activityScheduled || event.GetEventType() == enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED
		events++
	}
	require.True(t, activityScheduled)
	require.Greater(t, events, 5)
	require.NotContains(t, captured.String(), ingressHistorySecret)
	require.NotContains(t, logs.text(), ingressHistorySecret)
	require.NotContains(t, logs.text(), "client_secret")
	if failing {
		require.Contains(t, logs.text(), "invalid_credentials", "exercise the provider failure logging path")
		require.Contains(t, logs.text(), "activity failed", "exercise the production activity logging interceptor")
		require.Contains(t, logs.text(), "workflow failed", "exercise the production workflow logging interceptor")
	}
	t.Logf("captured %d real Temporal events (%d JSON bytes); provider logs %d bytes; decrypted sentinel absent", events, captured.Len(), len(logs.text()))
}
