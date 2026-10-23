package codemode

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/sandbox"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
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
	return newCodeGKEFixtureWithImage(t, "registry.example.com/code@sha256:"+strings.Repeat("a", 64), mutate)
}

func newCodeGKEFixtureWithImage(t *testing.T, image string, mutate func(*unstructured.Unstructured, *unstructured.Unstructured)) (*GKEProvider, *dynamicfake.FakeDynamicClient, Scope) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{sandbox.Claims: "SandboxClaimList", sandbox.Sandboxes: "SandboxList", sandbox.Pods: "PodList", sandbox.Templates: "SandboxTemplateList"})
	template := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "extensions.agents.x-k8s.io/v1alpha1", "kind": "SandboxTemplate", "metadata": map[string]any{"name": "code-v1", "namespace": "code"}}}
	require.NoError(t, client.Tracker().Create(sandbox.Templates, template, "code"))
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
	// A tagged image models a preview; released environments pin a digest.
	allowTag := !strings.Contains(image, "@sha256:")
	provider, err := NewGKEProvider(t.Context(), GKEConfig{Dynamic: client, Namespace: "code", Template: "code-v1", Image: image, AllowTaggedImage: allowTag, Port: 8081, Token: strings.Repeat("t", 32), Dialer: policy.Dialer(), CIDRs: []*net.IPNet{cidr}, Logger: testenv.NewLogger(t)})
	require.NoError(t, err)
	provider.capacityWait = 50 * time.Millisecond
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, provider.Shutdown(ctx))
	})
	return provider, client, Scope{Organization: "test-org", Project: uuid.New(), Caller: []string{"user-1"}}
}

func TestCodeGKERejectsMutableImage(t *testing.T) {
	t.Parallel()
	_, err := NewGKEProvider(t.Context(), GKEConfig{Logger: testenv.NewLogger(t), Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), Namespace: "code", Template: "code-v1", Image: "registry.example.com/code:0123456789ab", Port: 8081, CIDRs: []*net.IPNet{{}}})
	require.ErrorContains(t, err, "SHA-256-pinned image")
}

func TestCodeGKEPreviewAcceptsTaggedImage(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixtureWithImage(t, "registry.example.com/code:pr-12-abcdef12", nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
}

func TestValidTaggedImageReference(t *testing.T) {
	t.Parallel()
	for image, want := range map[string]bool{
		"gcr.io/project/code-runner:0123456789ab":               true,
		"registry.example.com:5000/code:pr-12-abcdef12":         true,
		"gcr.io/project/code@sha256:" + strings.Repeat("a", 64): false,
		"":                               false,
		"gcr.io/project/code":            false,
		"registry.example.com:5000/code": false,
		"gcr.io/project/code:":           false,
		"gcr.io/project/code:-bad":       false,
		"gcr.io/project/code:has space":  false,
	} {
		require.Equal(t, want, validTaggedImageReference(image), image)
	}
}

func TestValidImageReference(t *testing.T) {
	t.Parallel()
	for image, want := range map[string]bool{
		"gcr.io/project/code-runner:0123456789ab":               false,
		"registry.example.com:5000/code:pr-12-abcdef12":         false,
		"gcr.io/project/code@sha256:" + strings.Repeat("a", 64): true,
		"":                               false,
		"gcr.io/project/code":            false,
		"registry.example.com:5000/code": false,
		"gcr.io/project/code:":           false,
		"gcr.io/project/code:-bad":       false,
		"gcr.io/project/code:has space":  false,
		"gcr.io/project/code@sha256:abc": false,
		"gcr.io/project/code@sha256:" + strings.Repeat("A", 64): false,
	} {
		require.Equal(t, want, validImageReference(image), image)
	}
}

func TestCodeGKEPoolSharesPodsWithinOrganization(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	one, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	otherCaller := scope
	otherCaller.Project = uuid.New()
	otherCaller.Caller = []string{"user-2"}
	two, err := provider.Acquire(t.Context(), otherCaller)
	require.NoError(t, err)
	require.Same(t, one.Client(), two.Client(), "callers and projects in one organization share its pods")
	otherOrganization := scope
	otherOrganization.Organization = "another-org"
	other, err := provider.Acquire(t.Context(), otherOrganization)
	require.NoError(t, err)
	require.NotSame(t, one.Client(), other.Client(), "organizations never share a pod")
	require.NoError(t, one.Release(t.Context()))
	require.NoError(t, one.Release(t.Context()), "release must be idempotent")
	require.NoError(t, two.Release(t.Context()))
	require.NoError(t, other.Release(t.Context()))
}

func TestCodeGKEOrganizationGatewaySharesOrganizationPool(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	project, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	organizationWide := scope
	organizationWide.Project = uuid.Nil
	organizationWide.OrganizationWide = true
	organization, err := provider.Acquire(t.Context(), organizationWide)
	require.NoError(t, err)
	require.Same(t, project.Client(), organization.Client(), "the organization gateway uses its organization's pool")
	missing := scope
	missing.Project = uuid.Nil
	_, err = provider.Acquire(t.Context(), missing)
	require.Error(t, err, "a project scope needs its project")
	both := scope
	both.OrganizationWide = true
	_, err = provider.Acquire(t.Context(), both)
	require.Error(t, err, "an organization-wide scope has no project")
	require.NoError(t, project.Release(t.Context()))
	require.NoError(t, organization.Release(t.Context()))
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
		{"wrong-organization", func(claim, _ *unstructured.Unstructured) {
			labels := claim.GetLabels()
			labels[codeOrganizationLabel] = organizationLabel("another-org")
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
	for _, tc := range []struct {
		name       string
		replaceUID bool
		podIP      string
	}{
		{name: "replaced pod", replaceUID: true, podIP: "10.52.0.2"},
		// The same pod at another admissible address must not keep its connection.
		{name: "reassigned IP", replaceUID: false, podIP: "10.52.0.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			if tc.replaceUID {
				pod.SetUID(types.UID(uuid.NewString()))
			}
			require.NoError(t, unstructured.SetNestedField(pod.Object, tc.podIP, "status", "podIP"))
			_, err = client.Resource(sandbox.Pods).Namespace("code").Update(t.Context(), pod, metav1.UpdateOptions{})
			require.NoError(t, err)
			replacement, err := provider.Acquire(t.Context(), scope)
			require.NoError(t, err, "a changed pod is retired and the execution moves to a new one")
			require.NotSame(t, lease.Client(), replacement.Client())
			require.True(t, lease.Client().closed)
			require.NoError(t, replacement.Release(t.Context()))
		})
	}
}

// A live connection outlasts its pod's state, so reusing it must not admit
// work to a pod that has become unready or been replaced since it connected.
func TestCodeGKEConnectedSessionRevalidatesPod(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	entry := typed.entry
	local, remote := net.Pipe()
	peer, err := yamux.Server(remote, nil)
	require.NoError(t, err)
	session, err := yamux.Client(local, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = session.Close()
		_ = peer.Close()
	})
	entry.client.mu.Lock()
	entry.client.session = session
	entry.client.mu.Unlock()

	pod, err := client.Resource(sandbox.Pods).Namespace("code").Get(t.Context(), entry.name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedSlice(pod.Object, []any{map[string]any{"type": "Ready", "status": "False"}}, "status", "conditions"))
	_, err = client.Resource(sandbox.Pods).Namespace("code").Update(t.Context(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, errCodeSandboxPending, "an unready pod admits no work over its live connection")
	entry.client.mu.Lock()
	closed := entry.client.closed
	entry.client.mu.Unlock()
	require.False(t, closed, "an unready pod keeps its transport for when it recovers")

	require.NoError(t, client.Resource(sandbox.Pods).Namespace("code").Delete(t.Context(), entry.name, metav1.DeleteOptions{}))
	replacement, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err, "a deleted pod retires its live connection and a new pod serves")
	require.NotSame(t, lease.Client(), replacement.Client())
	require.NoError(t, replacement.Release(t.Context()))
	entry.client.mu.Lock()
	closed = entry.client.closed
	entry.client.mu.Unlock()
	require.True(t, closed)
	require.True(t, session.IsClosed())
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
	labels[codeOrganizationLabel] = organizationLabel("another-org")
	claim.SetLabels(labels)
	_, err = client.Resource(sandbox.Claims).Namespace("code").Update(t.Context(), claim, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, lease.Client().dialer.ControlContext(t.Context(), "tcp", "10.52.0.2:8081", nil), "ownership")
}

func TestCodeGKEConcurrentAdmissionScalesToOrganizationCap(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	capacity := defaultMaxOrganizationRunners * RunnerStreams
	var wg sync.WaitGroup
	leases := make(chan Lease, capacity+8)
	refused := make(chan error, capacity+8)
	for range capacity + 8 {
		wg.Go(func() {
			lease, err := provider.Acquire(t.Context(), scope)
			if err != nil {
				refused <- err
				return
			}
			leases <- lease
		})
	}
	wg.Wait()
	close(leases)
	close(refused)
	require.Len(t, leases, capacity, "the organization's pods fill to their stream capacity")
	for err := range refused {
		require.ErrorIs(t, err, ErrCapacity)
	}
	clients := map[*RunnerClient]int{}
	for lease := range leases {
		clients[lease.Client()]++
		require.NoError(t, lease.Release(t.Context()))
	}
	require.Len(t, clients, defaultMaxOrganizationRunners)
	for _, streams := range clients {
		require.Equal(t, RunnerStreams, streams)
	}
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, defaultMaxOrganizationRunners)
}

func TestCodeGKEWaitsForAFreeStream(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	provider.capacityWait = 5 * time.Second
	provider.config.MaxOrganizationRunners = 1
	leases := make([]Lease, 0, RunnerStreams)
	for range RunnerStreams {
		lease, err := provider.Acquire(t.Context(), scope)
		require.NoError(t, err)
		leases = append(leases, lease)
	}
	done := make(chan Lease, 1)
	go func() {
		lease, err := provider.Acquire(t.Context(), scope)
		assert.NoError(t, err)
		done <- lease
	}()
	select {
	case <-done:
		t.Fatal("a full pod admitted another stream")
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, leases[0].Release(t.Context()))
	waited := <-done
	require.Same(t, leases[1].Client(), waited.Client(), "the waiter takes the freed stream")
	require.NoError(t, waited.Release(t.Context()))
	for _, lease := range leases[1:] {
		require.NoError(t, lease.Release(t.Context()))
	}
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

func TestCodeGKEReplacesAgedPodWhileBusy(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	provider.mu.Lock()
	typed.entry.claimed = time.Now().Add(-codeRunnerMaxAge)
	provider.mu.Unlock()
	replacement, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NotSame(t, lease.Client(), replacement.Client(), "a busy pod past its maximum age takes no new executions")
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, 2, "the aged pod keeps its in-flight execution while the replacement serves")
	require.NoError(t, lease.Release(t.Context()))
	claims, err = client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, 1, "the aged pod is released once its last execution ends")
	require.NoError(t, replacement.Release(t.Context()))
}

func TestCodeGKEPlacesByReportedRunnerCapacity(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	first, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	runner := first.Client()
	runner.mu.Lock()
	runner.capacity = 1
	runner.mu.Unlock()
	second, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NotSame(t, runner, second.Client(), "a runner that admits one execution is never sent a second")
	require.NoError(t, first.Release(t.Context()))
	require.NoError(t, second.Release(t.Context()))
}

func TestCodeGKEReplacesPodWithMismatchedCapacity(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	runner := lease.Client()
	runner.mu.Lock()
	runner.capacity = RunnerStreams / 3
	runner.mu.Unlock()
	same, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.Same(t, runner, same.Client(), "a smaller runner keeps serving at its own capacity at first")
	require.NoError(t, same.Release(t.Context()))
	provider.mu.Lock()
	typed.entry.claimed = time.Now().Add(-codeMismatchedRunnerAge)
	provider.mu.Unlock()
	replacement, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NotSame(t, runner, replacement.Client(), "a pod from another template is replaced soon after it is seen")
	require.NoError(t, lease.Release(t.Context()))
	require.NoError(t, replacement.Release(t.Context()))
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

func TestCodeGKEShutdownReservesSharedDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		provider, _, scope := newCodeGKEFixture(t, nil)
		lease, err := provider.Acquire(t.Context(), scope)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		err = provider.Shutdown(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NoError(t, ctx.Err(), "an outstanding lease must leave time for other service drains")
		require.NoError(t, lease.Release(ctx))
		require.NoError(t, provider.Shutdown(ctx))
	})
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

func TestCodeGKEIdleEvictionAcrossOrganizations(t *testing.T) {
	t.Parallel()
	provider, _, scope := newCodeGKEFixture(t, nil)
	provider.config.MaxRunners = 2
	first, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, first.Release(t.Context()))
	second := scope
	second.Organization = "second-org"
	lease, err := provider.Acquire(t.Context(), second)
	require.NoError(t, err)
	third := scope
	third.Organization = "third-org"
	other, err := provider.Acquire(t.Context(), third)
	require.NoError(t, err, "a full replica evicts another organization's idle pod")
	require.True(t, first.Client().closed, "the oldest idle pod is evicted")
	fourth := scope
	fourth.Organization = "fourth-org"
	_, err = provider.Acquire(t.Context(), fourth)
	require.ErrorIs(t, err, ErrCapacity, "busy pods are never evicted")
	require.NoError(t, lease.Release(t.Context()))
	require.NoError(t, other.Release(t.Context()))
}

func TestCodeGKEMissingTemplateIsUnavailable(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	require.True(t, provider.Available())
	require.True(t, provider.Ready())
	require.NoError(t, client.Tracker().Delete(sandbox.Templates, "code", "code-v1"))
	provider.checkTemplate(t.Context())
	require.False(t, provider.Available())
	_, err := provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, ErrRuntimeUnavailable)
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items, "no claim is made while the template is missing")
	template := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "extensions.agents.x-k8s.io/v1alpha1", "kind": "SandboxTemplate", "metadata": map[string]any{"name": "code-v1", "namespace": "code"}}}
	require.NoError(t, client.Tracker().Create(sandbox.Templates, template, "code"))
	provider.checkTemplate(t.Context())
	require.True(t, provider.Available(), "a late template enables Code Mode")
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
}

func TestCodeGKEStartupWithoutTemplateIsNotReady(t *testing.T) {
	t.Parallel()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{sandbox.Templates: "SandboxTemplateList"})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	_, cidr, err := net.ParseCIDR("10.52.0.0/16")
	require.NoError(t, err)
	provider, err := NewGKEProvider(t.Context(), GKEConfig{Dynamic: client, Namespace: "code", Template: "code-v1", Image: "registry.example.com/code@sha256:" + strings.Repeat("a", 64), Port: 8081, Token: strings.Repeat("t", 32), Dialer: policy.Dialer(), CIDRs: []*net.IPNet{cidr}, Logger: testenv.NewLogger(t)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	require.False(t, provider.Available())
	require.False(t, provider.Ready(), "a replica waits for its template before reporting ready")
	provider.started = time.Now().Add(-codeReadyWait)
	require.True(t, provider.Ready(), "the wait for a template is bounded")
}

func TestCodeGKETemplateNotFoundClaimIsUnavailable(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, func(claim, _ *unstructured.Unstructured) {
		claim.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False", "reason": codeTemplateNotFoundReason}}}
	})
	_, err := provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, ErrRuntimeUnavailable)
	require.False(t, provider.Available())
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items, "a claim that can never start is deleted")
}

func TestCodeGKERenewsClaimsInUse(t *testing.T) {
	t.Parallel()
	provider, client, scope := newCodeGKEFixture(t, nil)
	lease, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	typed, ok := lease.(*gkeLease)
	require.True(t, ok)
	entry := typed.entry
	initial := entry.expires
	provider.now = func() time.Time { return initial.Add(-codeClaimRenewMargin + time.Second) }
	provider.renew(t.Context())
	provider.mu.Lock()
	renewed := entry.expires
	provider.mu.Unlock()
	require.True(t, renewed.After(initial), "an active pod's claim is extended")
	claim, err := client.Resource(sandbox.Claims).Namespace("code").Get(t.Context(), entry.name, metav1.GetOptions{})
	require.NoError(t, err)
	shutdown, _, _ := unstructured.NestedString(claim.Object, "spec", "lifecycle", "shutdownTime")
	require.True(t, expiryMatches(shutdown, renewed))
	require.NoError(t, lease.Release(t.Context()))
	next, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err)
	require.Same(t, lease.Client(), next.Client(), "the renewed pod keeps serving")
	require.NoError(t, next.Release(t.Context()))
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
	replacement, err := provider.Acquire(t.Context(), scope)
	require.NoError(t, err, "an evicted pod is retired and replaced")
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
	for range defaultMaxOrganizationRunners {
		_, err := provider.Acquire(t.Context(), scope)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	_, err := provider.Acquire(t.Context(), scope)
	require.ErrorIs(t, err, ErrCapacity)
	claims, err := client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, claims.Items, defaultMaxOrganizationRunners)
	outage.Store(false)
	provider.reap(t.Context())
	claims, err = client.Resource(sandbox.Claims).Namespace("code").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, claims.Items)
	provider.mu.Lock()
	require.Empty(t, provider.draining)
	provider.mu.Unlock()
}

func TestCodeGKEUnreachableTemplateAPIStaysReady(t *testing.T) {
	t.Parallel()
	provider, client, _ := newCodeGKEFixture(t, nil)
	client.PrependReactor("get", "sandboxtemplates", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("temporary API outage")
	})
	provider.checkTemplate(t.Context())
	require.True(t, provider.Ready(), "an API outage must not hold the replica out of rotation")
	require.True(t, provider.Available(), "an API outage keeps the last known availability")
}
