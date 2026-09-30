package codemode

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/sandbox"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newCodeGKEFixture simulates the Kubernetes API's UID assignment and a warm-pool
// controller adopting one pod per claim. No real cluster is contacted.
func newCodeGKEFixture(t *testing.T, mutate func(*unstructured.Unstructured, *unstructured.Unstructured)) (*GKEProvider, *dynamicfake.FakeDynamicClient, Scope) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{sandbox.Claims: "SandboxClaimList", sandbox.Sandboxes: "SandboxList", sandbox.Pods: "PodList"})
	image := "registry.example.com/code@sha256:" + strings.Repeat("a", 64)
	client.PrependReactor("create", "sandboxclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(k8stesting.CreateAction)
		require.True(t, ok)
		original, ok := create.GetObject().(*unstructured.Unstructured)
		require.True(t, ok)
		claim := original.DeepCopy()
		claim.SetUID(types.UID(uuid.NewString()))
		name := claim.GetName()
		claim.Object["status"] = map[string]any{"sandbox": map[string]any{"name": name}}
		resource := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "agents.x-k8s.io/v1alpha1", "kind": "Sandbox", "metadata": map[string]any{"name": name, "namespace": "code"}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}}
		pod := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "code", "uid": uuid.NewString(), "labels": map[string]any{sandbox.ClaimUIDLabel: string(claim.GetUID())}},
			"spec":   map[string]any{"runtimeClassName": "gvisor", "automountServiceAccountToken": false, "containers": []any{map[string]any{"name": "code-runner", "image": image}}},
			"status": map[string]any{"phase": "Running", "podIP": "10.52.0.2", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}, "containerStatuses": []any{map[string]any{"name": "code-runner", "imageID": "containerd://sha256:" + strings.Repeat("a", 64), "ready": true}}},
		}}
		if mutate != nil {
			mutate(claim, pod)
		}
		for _, item := range []struct {
			resource schema.GroupVersionResource
			value    *unstructured.Unstructured
		}{{sandbox.Sandboxes, resource}, {sandbox.Pods, pod}, {sandbox.Claims, claim}} {
			if err := client.Tracker().Create(item.resource, item.value, "code"); err != nil {
				return true, nil, fmt.Errorf("seed fake controller: %w", err)
			}
		}
		return true, claim, nil
	})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	_, cidr, err := net.ParseCIDR("10.52.0.0/16")
	require.NoError(t, err)
	provider, err := NewGKEProvider(t.Context(), GKEConfig{Dynamic: client, Namespace: "code", Template: "code-v1", Image: image, Port: 8081, Token: strings.Repeat("t", 32), Dialer: policy.Dialer(), CIDRs: []*net.IPNet{cidr}, Logger: testenv.NewLogger(t)})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, provider.Shutdown(ctx))
	})
	return provider, client, Scope{Organization: "test-org", Project: uuid.New()}
}

func TestCodeGKEPoolReusesOnlyWithinProject(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	one, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	two, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.Same(t, one.Client(), two.Client())
	otherScope := scope
	otherScope.Project = uuid.New()
	other, err := provider.Acquire(t.Context(), otherScope)
	require.NoError(t, err)
	require.NotSame(t, one.Client(), other.Client())
	require.NoError(t, one.Release(t.Context()))
	require.NoError(t, one.Release(t.Context()), "release must be idempotent")
	require.NoError(t, two.Release(t.Context()))
	require.NoError(t, other.Release(t.Context()))
}

func TestCodeGKERejectsUnsafeWarmPod(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*unstructured.Unstructured, *unstructured.Unstructured)
	}{
		{"pruned-expiry", func(claim, _ *unstructured.Unstructured) {
			unstructured.RemoveNestedField(claim.Object, "spec", "lifecycle", "shutdownTime")
		}},
		{"wrong-purpose", func(claim, _ *unstructured.Unstructured) {
			labels := claim.GetLabels()
			labels[codePurposeLabel] = "assistant"
			claim.SetLabels(labels)
		}},
		{"wrong-project", func(claim, _ *unstructured.Unstructured) {
			labels := claim.GetLabels()
			labels[codeProjectLabel] = uuid.NewString()
			claim.SetLabels(labels)
		}},
		{"wrong-image", func(_, pod *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(pod.Object, []any{map[string]any{"name": "code-runner", "image": "wrong"}}, "spec", "containers")
		}},
		{"stale-pulled-image", func(_, pod *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(pod.Object, []any{map[string]any{"name": "code-runner", "ready": true, "imageID": "containerd://sha256:" + strings.Repeat("b", 64)}}, "status", "containerStatuses")
		}},
		{"without-gvisor", func(_, pod *unstructured.Unstructured) {
			unstructured.RemoveNestedField(pod.Object, "spec", "runtimeClassName")
		}},
		{"outside-cidr", func(_, pod *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(pod.Object, "169.254.169.254", "status", "podIP")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provider, client, scope := newCodeGKEFixture(t, tc.mutate)
			_, err := provider.Acquire(t.Context(), scope)
			require.Error(t, err)
			claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
			require.NoError(t, err)
			require.Empty(t, claims.Items, "owned invalid claim must be deleted")
		})
	}
}

func TestCodeGKERejectsReassignedPodIP(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	entry := typed.entry
	pod, err := client.Resource(sandbox.Pods).Namespace("code").Get(t.Context(), entry.name, metav1.GetOptions{})
	require.NoError(t, err)
	pod.SetUID(types.UID(uuid.NewString()))
	_, err = client.Resource(sandbox.Pods).Namespace("code").Update(t.Context(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = provider.Acquire(t.Context(), scope)
	require.ErrorContains(t, err, "identity changed")
	require.True(t, lease.Client().closed)
}

func TestCodeGKEReconnectRevalidatesOwnership(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	defer func() { require.NoError(t, lease.Release(t.Context())) }()
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	entry := typed.entry
	claim, err := client.Resource(sandbox.Claims).Namespace("code").Get(t.Context(), entry.name, metav1.GetOptions{})
	require.NoError(t, err)
	labels := claim.GetLabels()
	labels[codeProjectLabel] = uuid.NewString()
	claim.SetLabels(labels)
	_, err = client.Resource(sandbox.Claims).Namespace("code").Update(t.Context(), claim, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, lease.Client().dialer.ControlContext(t.Context(), "tcp", "10.52.0.2:8081", nil), "ownership")
}

func TestCodeGKEConcurrentAdmissionIsBounded(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	var wg sync.WaitGroup
	leases := make(chan Lease, 32)
	for range 32 {
		wg.Go(func() {
			lease, err := provider.Acquire(t.Context(), scope)
			if err == nil {
				leases <- lease
			}
		})
	}
	wg.Wait()
	close(leases)
	require.Len(t, leases, maxRunnerStreams)
	for lease := range leases {
		require.NoError(t, lease.Release(t.Context()))
	}
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, 1)
}

func TestCodeGKEDrainsBeforeExpiryAndDeletesWithUID(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	entry := typed.entry
	provider.mu.Lock()
	entry.expires = time.Now().Add(codeDrainMargin - time.Second)
	provider.mu.Unlock()
	replacement, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NotSame(t, replacement.Client(), lease.Client())
	defer func() { require.NoError(t, replacement.Release(t.Context())) }()
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, 2, "replacement warms while the active lease drains")
	require.NoError(t, lease.Release(t.Context()))
	var deletes int
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" && action.GetResource() == sandbox.Claims {
			deletes++
			deletion, ok := action.(k8stesting.DeleteAction)
			require.True(t, ok)
			options := deletion.GetDeleteOptions()
			require.NotNil(t, options.Preconditions)
			require.Equal(t, entry.uid, *options.Preconditions.UID)
		}
	}
	require.Equal(t, 1, deletes)
}

func TestCodeGKEIdleReapAndShutdown(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	provider.mu.Lock()
	typed.entry.idle = time.Now().Add(-codeIdleTTL)
	provider.mu.Unlock()
	provider.reap(t.Context())
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items)
	require.True(t, lease.Client().closed)
	require.NoError(t, provider.Shutdown(t.Context()))
	_, err = provider.Acquire(t.Context(), scope)
	require.ErrorContains(t, err, "stopping")
}

func TestCodeGKETransientAdmissionFailurePreservesPod(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	first, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, first.Release(t.Context()))
	var fail atomic.Bool
	fail.Store(true)
	client.PrependReactor("get", "sandboxclaims", func(k8stesting.Action) (bool, runtime.Object, error) {
		if fail.Swap(false) {
			return true, nil, context.Canceled
		}
		return false, nil, nil
	})
	_, err = provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, context.Canceled)
	next, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.Same(t, first.Client(), next.Client())
	require.NoError(t, next.Release(t.Context()))
}

func TestCodeGKECancelledRequestDoesNotCancelWarmup(t *testing.T) {
	t.Parallel()
	entered, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(unblock) })
	provider, client, scope := newCodeGKEFixture(t, func(_, _ *unstructured.Unstructured) { close(entered); <-unblock })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := provider.Acquire(ctx, scope); done <- err }()
	<-entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	once.Do(func() { close(unblock) })
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, 1)
}

func TestCodeGKEIdleEvictionAndOrganizationCap(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	first, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, first.Release(t.Context()))
	for range maxOrganizationRunners {
		scope.Project = uuid.New()
		lease, err := provider.Acquire(t.Context(), scope)
		require.NoError(t, err)
		require.NoError(t, lease.Release(t.Context()))
	}
	require.True(t, first.Client().closed, "oldest idle entry should be evicted")
	leases := make([]Lease, 0, maxOrganizationRunners)
	for range maxOrganizationRunners {
		scope.Project = uuid.New()
		lease, err := provider.Acquire(t.Context(), scope)
		require.NoError(t, err)
		leases = append(leases, lease)
	}
	scope.Project = uuid.New()
	_, err = provider.Acquire(t.Context(), scope)
	require.ErrorContains(t, err, "capacity")
	scope.Organization = "another-org"
	other, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err, "one organization cannot exhaust the replica")
	require.NoError(t, other.Release(t.Context()))
	for _, lease := range leases {
		require.NoError(t, lease.Release(t.Context()))
	}
}

func TestCodeGKEAcceptsEquivalentExpiryTimestamp(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, func(claim, _ *unstructured.Unstructured) {
		encoded, _, _ := unstructured.NestedString(claim.Object, "spec", "lifecycle", "shutdownTime")
		expiry, err := time.Parse(time.RFC3339, encoded)
		require.NoError(t, err)
		require.NoError(t, unstructured.SetNestedField(claim.Object, expiry.In(time.FixedZone("offset", 3600)).Format(time.RFC3339), "spec", "lifecycle", "shutdownTime"))
	})
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
}

func TestCodeGKEAmbiguousCreateRecoversOwnedUID(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	controller := client.ReactionChain[0]
	client.PrependReactor("create", "sandboxclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		_, _, err := controller.React(action)
		require.NoError(t, err)
		return true, nil, context.DeadlineExceeded
	})
	_, err := provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items)
}

func TestCodeGKEShutdownDuringWarmupCleansClaim(t *testing.T) {
	t.Parallel()
	entered, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(unblock) })
	provider, client, scope := newCodeGKEFixture(t, func(_, _ *unstructured.Unstructured) { close(entered); <-unblock })
	done := make(chan error, 1)
	go func() { _, err := provider.Acquire(t.Context(), scope); done <- err }()
	<-entered
	provider.cancel()
	once.Do(func() { close(unblock) })
	require.NoError(t, provider.Shutdown(t.Context()))
	require.Error(t, <-done)
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items)
}

func TestCodeGKEEvictedPodRetiresPinnedRuntime(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	require.NoError(t, client.Resource(sandbox.Pods).Namespace("code").Delete(t.Context(), typed.entry.name, metav1.DeleteOptions{}))
	_, err = provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, errCodeSandboxMismatch)
	replacement, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NotSame(t, lease.Client(), replacement.Client())
	require.NoError(t, replacement.Release(t.Context()))
}

func TestCodeGKEColdStartSurvivesTransientRead(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	var failed atomic.Bool
	client.PrependReactor("get", "sandboxclaims", func(k8stesting.Action) (bool, runtime.Object, error) {
		if !failed.Swap(true) {
			return true, nil, fmt.Errorf("temporary API outage")
		}
		return false, nil, nil
	})
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, 1)
}

func TestCodeGKEAmbiguousOrphansCountAgainstCapacity(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	controller := client.ReactionChain[0]
	var outage atomic.Bool
	outage.Store(true)
	client.PrependReactor("create", "sandboxclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		_, _, err := controller.React(action)
		require.NoError(t, err)
		return true, nil, context.DeadlineExceeded
	})
	client.PrependReactor("get", "sandboxclaims", func(k8stesting.Action) (bool, runtime.Object, error) {
		if outage.Load() {
			return true, nil, fmt.Errorf("temporary API outage")
		}
		return false, nil, nil
	})
	for range maxOrganizationRunners {
		_, err := provider.Acquire(t.Context(), scope)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	_, err := provider.Acquire(t.Context(), scope)
	require.ErrorContains(t, err, "capacity")
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, maxOrganizationRunners)
	outage.Store(false)
	provider.reap(t.Context())
	claims, err = client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items)
	provider.mu.Lock()
	require.Empty(t, provider.draining)
	provider.mu.Unlock()
}
