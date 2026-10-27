package assistants

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
)

func TestKitGKEIAMAccount(t *testing.T) {
	name := kitGKEIAMAccount("kit-runtime-generation-one")
	require.Len(t, name, 29)
	require.True(t, strings.HasPrefix(name, "kit-wl-"))
	require.Equal(t, name, kitGKEIAMAccount("kit-runtime-generation-one"))
	require.NotEqual(t, name, kitGKEIAMAccount("kit-runtime-generation-two"))
}
func TestKitGKEIAMProvisionAndCleanup(t *testing.T) {
	const project, namespace, ksa, subject = "test-project", "kit-test", "kit-generation", "123456789012345678901"
	email := kitGKEIAMAccount(ksa) + "@" + project + ".iam.gserviceaccount.com"
	exists := false
	var policyWrites, deletes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/serviceAccounts"):
			var request iam.CreateServiceAccountRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, kitGKEIAMAccount(ksa), request.AccountId)
			if exists {
				w.WriteHeader(409)
				_, _ = w.Write([]byte(`{"error":{"code":409}}`))
				return
			}
			exists = true
			_ = json.NewEncoder(w).Encode(&iam.ServiceAccount{Email: email, UniqueId: subject, Description: "Speakeasy Kit workload kit-test/kit-generation"})
		case strings.HasSuffix(r.URL.Path, ":getIamPolicy"):
			require.Contains(t, r.URL.Path, subject)
			_ = json.NewEncoder(w).Encode(&iam.Policy{Etag: "etag"})
		case strings.HasSuffix(r.URL.Path, ":setIamPolicy"):
			var request iam.SetIamPolicyRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "etag", request.Policy.Etag)
			require.Len(t, request.Policy.Bindings, 1)
			require.Equal(t, "roles/iam.workloadIdentityUser", request.Policy.Bindings[0].Role)
			require.Equal(t, []string{"serviceAccount:pool-project.svc.id.goog[kit-test/kit-generation]"}, request.Policy.Bindings[0].Members)
			policyWrites++
			_ = json.NewEncoder(w).Encode(request.Policy)
		case r.Method == http.MethodGet:
			if !exists {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"code":404}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(&iam.ServiceAccount{Email: email, UniqueId: subject, Description: "Speakeasy Kit workload kit-test/kit-generation"})
		case r.Method == http.MethodDelete:
			require.True(t, strings.HasSuffix(r.URL.Path, "/"+subject))
			exists = false
			deletes++
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected IAM request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	service, err := iam.NewService(t.Context(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	require.NoError(t, err)
	manager := &kitGKEIAM{project: project, workloadPoolProject: "pool-project", namespace: namespace, service: service}
	identity, err := manager.provision(t.Context(), ksa)
	require.NoError(t, err)
	require.Equal(t, subject, identity.subject)
	require.Equal(t, email, identity.email)
	resumed, err := manager.provision(t.Context(), ksa)
	require.NoError(t, err)
	require.Equal(t, identity, resumed)
	require.Equal(t, 2, policyWrites)
	require.NoError(t, manager.remove(t.Context(), identity))
	require.NoError(t, manager.remove(t.Context(), identity))
	require.Equal(t, 1, deletes)
}
func TestKitGKEIAMRejectsUnexpectedPolicy(t *testing.T) {
	for _, binding := range []*iam.Binding{
		{Role: "roles/owner", Members: []string{"allUsers"}},
		{Role: "roles/iam.workloadIdentityUser", Members: []string{"allUsers"}},
		{Role: "roles/iam.workloadIdentityUser", Members: []string{"serviceAccount:pool-project.svc.id.goog[kit-test/kit-generation]"}, Condition: &iam.Expr{Expression: "true"}},
	} {
		t.Run(binding.Role+strings.Join(binding.Members, ","), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/serviceAccounts") {
					_ = json.NewEncoder(w).Encode(&iam.ServiceAccount{Email: kitGKEIAMAccount("kit-generation") + "@test-project.iam.gserviceaccount.com", UniqueId: "123", Description: "Speakeasy Kit workload kit-test/kit-generation"})
					return
				}
				require.True(t, strings.HasSuffix(r.URL.Path, ":getIamPolicy"))
				_ = json.NewEncoder(w).Encode(&iam.Policy{Bindings: []*iam.Binding{binding}})
			}))
			defer server.Close()
			service, err := iam.NewService(t.Context(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
			require.NoError(t, err)
			manager := &kitGKEIAM{project: "test-project", workloadPoolProject: "pool-project", namespace: "kit-test", service: service}
			_, err = manager.provision(t.Context(), "kit-generation")
			require.ErrorContains(t, err, "unexpected IAM grants")
		})
	}
}
