package assistants

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKitGKEReapTombstoneDoesNotRequireLiveAdmission(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	r.Retired = true
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		t.Fatal("tombstone cleanup must not request live admission")
		return nil, ErrKitRuntimeBinding
	}
	require.NoError(t, k.Reap(t.Context(), r))
	require.Equal(t, 1, *fences)
	_, err = client.Resource(kitClaimGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
	require.NoError(t, k.Reap(t.Context(), r), "permanent cleanup is retryable")
	_, err = k.Ensure(t.Context(), r)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
}

func TestKitGKELegacyNativeExpiryDoesNotExpireWorkload(t *testing.T) {
	t.Parallel()
	k, _, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	// Old persisted native-inference expiry is an unknown field, not workload
	// admission authority. Projected tokens rotate without replacing the process.
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &metadata))
	metadata["authority_not_after"], err = json.Marshal(time.Now().Add(-time.Minute))
	require.NoError(t, err)
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, k.config.AuthorityStore.Save(t.Context(), r.LifecycleTx, r, result.BackendMetadataJSON, raw))
	r.BackendMetadataJSON = raw
	_, err = k.protocol(t.Context(), r)
	require.NoError(t, err)
	_, err = k.protocolForLifecycle(t.Context(), r, true)
	require.NoError(t, err)
	reused, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.False(t, reused.ColdStart)
	require.Zero(t, *fences)
}

func TestKitGKEReapTombstoneFenceFailurePreservesResources(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	r.Retired = true
	failure := errors.New("revocation unavailable")
	k.config.Fence = func(context.Context, assistantRuntimeRecord) error { return failure }
	require.ErrorIs(t, k.Reap(t.Context(), r), failure)
	_, err = client.Resource(kitClaimGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.NoError(t, err)
}

// Exercise the actual Postgres fence, not an in-process replacement mutex.
func TestKitGKELifecycleSharesAssistantFenceAcrossWorkers(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	err := assistantrepo.New(db).SetAssistantRuntimeBackendForTest(t.Context(), assistantrepo.SetAssistantRuntimeBackendForTestParams{Backend: runtimeBackendGKE, RuntimeID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3, Backend: runtimeBackendGKE}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- core.kitGKELifecycle(t.Context(), r, func(ctx context.Context, locked assistantRuntimeRecord) error {
			if locked.LifecycleTx == nil {
				return errors.New("missing explicit lifecycle transaction")
			}
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("first worker: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first worker did not acquire fence")
	}
	other := &ServiceCore{db: db}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	err = other.kitGKELifecycle(ctx, r, func(context.Context, assistantRuntimeRecord) error {
		t.Error("second worker entered a fenced assistant lifecycle")
		return nil
	})
	close(release)
	require.Error(t, err)
	require.NoError(t, <-done)
}

func TestKitGKELifecycleCommitsRecoveryEvidenceAfterFailure(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	err := assistantrepo.New(db).SetAssistantRuntimeBackendForTest(t.Context(), assistantrepo.SetAssistantRuntimeBackendForTestParams{Backend: runtimeBackendGKE, RuntimeID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: key.RuntimeID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3, Backend: runtimeBackendGKE}
	failure := errors.New("external effect failed")
	err = core.kitGKELifecycle(t.Context(), r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if err := assistantrepo.New(locked.LifecycleTx).UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, BackendMetadataJson: []byte(`{"checkpoint":{"phase":"closing"}}`)}); err != nil {
			return fmt.Errorf("update recovery evidence: %w", err)
		}
		return failure
	})
	require.ErrorIs(t, err, failure)
	row, err := assistantrepo.New(db).GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: key.ProjectID, ID: key.RuntimeID})
	require.NoError(t, err)
	require.JSONEq(t, `{"checkpoint":{"phase":"closing"}}`, string(row.BackendMetadataJson))
	require.NoError(t, assistantrepo.New(db).DeleteAssistant(t.Context(), assistantrepo.DeleteAssistantParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
	require.NoError(t, core.kitGKELifecycle(t.Context(), r, func(_ context.Context, locked assistantRuntimeRecord) error {
		require.True(t, locked.Retired, "cleanup authority must include the assistant tombstone")
		return nil
	}))
}

func TestKitGKEGatewayRenewalUsesCertificateExpiry(t *testing.T) {
	t.Parallel()
	certPEM, _, err := newKitGKETLSCertificate("kit-renewal.kit.internal")
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	due, err := kitGKETLSRenewalDue(certPEM, "kit-renewal.kit.internal", cert.NotAfter.Add(-24*time.Hour-time.Second))
	require.NoError(t, err)
	require.False(t, due)
	due, err = kitGKETLSRenewalDue(certPEM, "kit-renewal.kit.internal", cert.NotAfter.Add(-24*time.Hour))
	require.NoError(t, err)
	require.True(t, due)
	_, err = kitGKETLSRenewalDue(certPEM, "wrong.kit.internal", time.Now())
	require.Error(t, err)
	_, err = kitGKETLSRenewalDue([]byte("invalid"), "kit-renewal.kit.internal", time.Now())
	require.Error(t, err)
}
