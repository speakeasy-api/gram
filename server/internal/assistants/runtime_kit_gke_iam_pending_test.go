package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKEIAMPendingRecoversUnobservedCreateAndFailedDeletes(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	journalDeleteFails := true
	client.PrependReactor("delete", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		if journalDeleteFails {
			journalDeleteFails = false
			return true, nil, fmt.Errorf("journal deletion unavailable")
		}
		deletion, ok := action.(ktesting.DeleteAction)
		require.True(t, ok)
		opts := deletion.GetDeleteOptions()
		require.EqualValues(t, "uid-"+kitGKEName(r)+"-iam", *opts.Preconditions.UID)
		return false, nil, nil
	})
	const ksa = "kit-generation"
	exists, iamDeleteFails := false, true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/serviceAccounts") {
			// The journal must already be durable when the remote create starts.
			_, err := client.Resource(kitGKEIAMPendingGVR).Namespace("kit-test").Get(req.Context(), kitGKEName(r)+"-iam", metav1.GetOptions{})
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			exists = true
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"response lost after create"}}`))
			return
		}
		if req.Method == http.MethodGet {
			if !exists {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"code":404}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(&iam.ServiceAccount{Email: kitGKEIAMAccount(ksa) + "@identity-project.iam.gserviceaccount.com", UniqueId: "123", Description: "Speakeasy Kit workload kit-test/" + ksa})
			return
		}
		if !assert.Equal(t, http.MethodDelete, req.Method) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !assert.True(t, strings.HasSuffix(req.URL.Path, "/123")) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if iamDeleteFails {
			iamDeleteFails = false
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":503}}`))
			return
		}
		exists = false
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	service, err := iam.NewService(t.Context(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	require.NoError(t, err)
	m := &kitGKEIAM{project: "identity-project", workloadPoolProject: "pool-project", namespace: "kit-test", service: service, dynamic: client}
	ctx := context.WithValue(t.Context(), kitGKEIAMContextKey{}, &kitGKEIAMAttempt{manager: m, runtime: r})
	_, err = kitGKEWorkloadSubject(ctx, ksa)
	require.Error(t, err)
	require.True(t, exists)
	// The remote create succeeded before runtime metadata was published. Retire
	// the durable empty row, deliberately passing stale caller metadata.
	require.NoError(t, k.config.AuthorityStore.Save(t.Context(), r.LifecycleTx, r, []byte("{}"), []byte("{}")))
	r.Retired = true
	r.BackendMetadataJSON = []byte(`{"workload":{"subject":"stale"}}`)
	fenceFails := true
	k.config.Fence = func(ctx context.Context, fenced assistantRuntimeRecord) error {
		require.JSONEq(t, "{}", string(fenced.BackendMetadataJSON))
		if fenceFails {
			return fmt.Errorf("credential fence unavailable")
		}
		if err := m.removeRuntime(ctx, fenced); err != nil {
			return err
		}
		return m.removePending(ctx, fenced)
	}
	require.ErrorContains(t, k.Reap(t.Context(), r), "credential fence unavailable")
	require.True(t, exists)
	fenceFails = false
	// An unobserved create followed by 404 is ambiguous, not deletion proof.
	exists = false
	require.ErrorContains(t, k.Reap(t.Context(), r), "remains uncertain")
	exists = true
	require.Error(t, k.Reap(t.Context(), r)) // IAM delete failed; intent remains.
	require.True(t, exists)
	require.ErrorContains(t, k.ReapStoppedMachine(t.Context(), r), "journal deletion unavailable")
	require.False(t, exists)
	require.NoError(t, k.Reap(t.Context(), r)) // Recorded numeric ID makes 404 safe.
	_, err = client.Resource(kitGKEIAMPendingGVR).Namespace("kit-test").Get(t.Context(), kitGKEName(r)+"-iam", metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	persisted, err := k.config.AuthorityStore.Load(t.Context(), r.LifecycleTx, r)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(persisted))
}

func TestKitGKEIAMUsesFixedGoogleIssuer(t *testing.T) {
	t.Parallel()
	c := KitGKECompositionConfig{
		IAMProject: "identity-project", WorkloadPoolProject: "pool-project",
		KitLocalCompositionConfig: KitLocalCompositionConfig{WorkloadIssuerURL: "https://container.googleapis.com/v1/projects/pool-project/locations/region/clusters/cluster", WorkloadJWKSURL: "https://legacy.example/jwks"},
	}
	configured := kitGKEGoogleIssuerConfig(c)
	require.Equal(t, "https://accounts.google.com", configured.WorkloadIssuerURL)
	require.Equal(t, "https://www.googleapis.com/oauth2/v3/certs", configured.WorkloadJWKSURL)
	require.Equal(t, "identity-project", configured.IAMProject)
	require.Equal(t, "pool-project", configured.WorkloadPoolProject)
	require.NotEqual(t, configured.WorkloadIssuerURL, c.WorkloadIssuerURL)
}

func TestKitGKEIAMPendingCreateFailureClassification(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 403, 404, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
				create, ok := action.(ktesting.CreateAction)
				require.True(t, ok)
				obj, ok := create.GetObject().(*unstructured.Unstructured)
				require.True(t, ok)
				obj.SetUID("pending-uid")
				obj.SetResourceVersion("1")
				return false, nil, nil
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if !assert.Equal(t, http.MethodPost, req.Method) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if !assert.True(t, strings.HasSuffix(req.URL.Path, "/serviceAccounts")) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%d}}`, status)
			}))
			defer server.Close()
			service, err := iam.NewService(t.Context(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
			require.NoError(t, err)
			r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), LifecycleTx: kitGKEFixtureTransaction{}}
			m := &kitGKEIAM{project: "identity-project", workloadPoolProject: "pool-project", namespace: "kit-test", service: service, dynamic: client}
			ctx := context.WithValue(t.Context(), kitGKEIAMContextKey{}, &kitGKEIAMAttempt{manager: m, runtime: r})
			_, err = kitGKEWorkloadSubject(ctx, "kit-generation")
			require.Error(t, err)
			pending, err := client.Resource(kitGKEIAMPendingGVR).Namespace(m.namespace).Get(ctx, kitGKEName(r)+"-iam", metav1.GetOptions{})
			if status == 429 || status == 503 {
				require.NoError(t, err, "transient errors must preserve recovery evidence")
				notCreated, _, err := unstructured.NestedString(pending.Object, "data", "not_created")
				require.NoError(t, err)
				require.NotEqual(t, "true", notCreated)
			} else {
				require.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestKitGKEIAMPendingRejectsForeignOwnershipAndMissingUID(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"runtime", "assistant", "project", "iam_project", "uid"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), LifecycleTx: kitGKEFixtureTransaction{}}
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": kitGKEName(r) + "-iam", "namespace": "kit-test", "uid": "pending-uid"},
				"data": map[string]any{"runtime": r.ID.String(), "assistant": r.AssistantID.String(), "project": r.ProjectID.String(), "iam_project": "identity-project", "account": "kit-generation", "not_created": "true"},
			}}
			if field == "uid" {
				obj.SetUID("")
			} else {
				require.NoError(t, unstructured.SetNestedField(obj.Object, "foreign", "data", field))
			}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
			m := &kitGKEIAM{project: "identity-project", workloadPoolProject: "pool-project", namespace: "kit-test", dynamic: client}
			require.ErrorContains(t, m.removePending(t.Context(), r), "ownership mismatch")
			_, err := client.Resource(kitGKEIAMPendingGVR).Namespace(m.namespace).Get(t.Context(), obj.GetName(), metav1.GetOptions{})
			require.NoError(t, err)
		})
	}
}
