package assistants

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
)

func TestKitGKEIAMAccount(t *testing.T) {
	t.Parallel()
	name := kitGKEIAMAccount("kit-runtime-generation-one")
	require.Len(t, name, 29)
	require.True(t, strings.HasPrefix(name, "kit-wl-"))
	require.Equal(t, name, kitGKEIAMAccount("kit-runtime-generation-one"))
	require.NotEqual(t, name, kitGKEIAMAccount("kit-runtime-generation-two"))
}
func TestKitGKEIAMProvisionAndCleanup(t *testing.T) {
	t.Parallel()
	const project, namespace, ksa, subject = "test-project", "kit-test", "kit-generation", "123456789012345678901"
	email := kitGKEIAMAccount(ksa) + "@" + project + ".iam.gserviceaccount.com"
	exists := false
	var policyWrites, deletes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/serviceAccounts"):
			var request iam.CreateServiceAccountRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !assert.Equal(t, kitGKEIAMAccount(ksa), request.AccountId) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if exists {
				w.WriteHeader(409)
				_, _ = w.Write([]byte(`{"error":{"code":409}}`))
				return
			}
			exists = true
			_ = json.NewEncoder(w).Encode(&iam.ServiceAccount{Email: email, UniqueId: subject, Description: "Speakeasy Kit workload kit-test/kit-generation"})
		case strings.HasSuffix(r.URL.Path, ":getIamPolicy"):
			if !assert.Contains(t, r.URL.Path, subject) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(&iam.Policy{Etag: "etag"})
		case strings.HasSuffix(r.URL.Path, ":setIamPolicy"):
			var request iam.SetIamPolicyRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !assert.NotNil(t, request.Policy) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !assert.Equal(t, "etag", request.Policy.Etag) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !assert.Len(t, request.Policy.Bindings, 1) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !assert.Equal(t, "roles/iam.workloadIdentityUser", request.Policy.Bindings[0].Role) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if !assert.Equal(t, []string{"serviceAccount:pool-project.svc.id.goog[kit-test/kit-generation]"}, request.Policy.Bindings[0].Members) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
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
			if !assert.True(t, strings.HasSuffix(r.URL.Path, "/"+subject)) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
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
	t.Parallel()
	for _, binding := range []*iam.Binding{
		{Role: "roles/owner", Members: []string{"allUsers"}},
		{Role: "roles/iam.workloadIdentityUser", Members: []string{"allUsers"}},
		{Role: "roles/iam.workloadIdentityUser", Members: []string{"serviceAccount:pool-project.svc.id.goog[kit-test/kit-generation]"}, Condition: &iam.Expr{Expression: "true"}},
	} {
		t.Run(binding.Role+strings.Join(binding.Members, ","), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/serviceAccounts") {
					_ = json.NewEncoder(w).Encode(&iam.ServiceAccount{Email: kitGKEIAMAccount("kit-generation") + "@test-project.iam.gserviceaccount.com", UniqueId: "123", Description: "Speakeasy Kit workload kit-test/kit-generation"})
					return
				}
				if !assert.True(t, strings.HasSuffix(r.URL.Path, ":getIamPolicy")) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
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

func TestKitGKEIAMCreateFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status   int
		rejected bool
	}{
		{http.StatusBadRequest, true}, {http.StatusForbidden, true}, {http.StatusNotFound, true},
		{http.StatusTooManyRequests, false}, {http.StatusInternalServerError, false},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			manager := newKitGKEIAMTestManager(t, func(w http.ResponseWriter, r *http.Request) {
				if !assert.Equal(t, http.MethodPost, r.Method) || !assert.Equal(t, "/v1/projects/test-project/serviceAccounts", r.URL.Path) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(tc.status)
			})
			identity, err := manager.provision(t.Context(), "kit-generation")
			require.Error(t, err)
			require.Equal(t, kitGKEIAMIdentity{}, identity)
			var rejected kitGKEIAMCreateRejectedError
			require.Equal(t, tc.rejected, errors.As(err, &rejected))
			require.True(t, kitGKEIAMStatus(err, tc.status), "original API status must be retained")
		})
	}
}

func TestKitGKEIAMRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	for _, resumed := range []bool{false, true} {
		name := "created"
		if resumed {
			name = "resumed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name   string
				change func(*iam.ServiceAccount)
			}{
				{"email", func(sa *iam.ServiceAccount) { sa.Email = "other@test-project.iam.gserviceaccount.com" }},
				{"description", func(sa *iam.ServiceAccount) { sa.Description = "another workload" }},
				{"disabled", func(sa *iam.ServiceAccount) { sa.Disabled = true }},
				{"non-numeric subject", func(sa *iam.ServiceAccount) { sa.UniqueId = "not-numeric" }},
				{"empty subject", func(sa *iam.ServiceAccount) { sa.UniqueId = "" }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					sa := kitGKEIAMTestAccount()
					tc.change(sa)
					manager := newKitGKEIAMTestManager(t, func(w http.ResponseWriter, r *http.Request) {
						switch {
						case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/test-project/serviceAccounts":
							if resumed {
								w.WriteHeader(http.StatusConflict)
								return
							}
						case resumed && r.Method == http.MethodGet && r.URL.Path == "/v1/projects/test-project/serviceAccounts/"+kitGKEIAMTestAccount().Email:
						default:
							t.Errorf("unexpected IAM request: %s %s", r.Method, r.URL.Path)
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						_ = json.NewEncoder(w).Encode(sa)
					})
					identity, err := manager.provision(t.Context(), "kit-generation")
					require.ErrorContains(t, err, "account identity mismatch")
					require.Equal(t, kitGKEIAMIdentity{}, identity)
				})
			}
		})
	}
}

func TestKitGKEIAMRemoveRefusesUnsafeIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		subject, account string
		change           func(*iam.ServiceAccount)
		unbound          bool
	}{
		{name: "empty subject", account: "kit-generation", unbound: true},
		{name: "non-numeric subject", subject: "not-numeric", account: "kit-generation", unbound: true},
		{name: "empty account", subject: "123", unbound: true},
		{name: "mismatched email", subject: "123", account: "kit-generation", change: func(sa *iam.ServiceAccount) { sa.Email = "other@test-project.iam.gserviceaccount.com" }},
		{name: "mismatched description", subject: "123", account: "kit-generation", change: func(sa *iam.ServiceAccount) { sa.Description = "another workload" }},
		{name: "mismatched subject", subject: "123", account: "kit-generation", change: func(sa *iam.ServiceAccount) { sa.UniqueId = "456" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sa := kitGKEIAMTestAccount()
			if tc.change != nil {
				tc.change(sa)
			}
			manager := newKitGKEIAMTestManager(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.unbound || r.Method != http.MethodGet || r.URL.Path != "/v1/projects/test-project/serviceAccounts/123" {
					t.Errorf("unexpected IAM request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(sa)
			})
			err := manager.remove(t.Context(), kitGKEIAMIdentity{subject: tc.subject, account: tc.account})
			if tc.unbound {
				require.ErrorContains(t, err, "refusing to delete unbound")
			} else {
				require.ErrorContains(t, err, "refusing to delete mismatched")
			}
		})
	}
}

func newKitGKEIAMTestManager(t *testing.T, handler http.HandlerFunc) *kitGKEIAM {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	service, err := iam.NewService(t.Context(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	require.NoError(t, err)
	return &kitGKEIAM{project: "test-project", workloadPoolProject: "pool-project", namespace: "kit-test", service: service}
}

func kitGKEIAMTestAccount() *iam.ServiceAccount {
	return &iam.ServiceAccount{
		Email:       kitGKEIAMAccount("kit-generation") + "@test-project.iam.gserviceaccount.com",
		UniqueId:    "123",
		Description: "Speakeasy Kit workload kit-test/kit-generation",
	}
}
