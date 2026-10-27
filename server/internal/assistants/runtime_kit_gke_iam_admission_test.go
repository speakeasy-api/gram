package assistants

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKEIAMAdmissionUsesAgentAndRejectsRetiredSubject(t *testing.T) {
	core, db, r, identity, c, _ := kitWorkloadDBFixture(t)
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	r.LifecycleTx = tx
	c.WorkloadIssuerURL, c.WorkloadJWKSURL = "https://accounts.google.com", "https://www.googleapis.com/oauth2/v3/certs"
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		obj := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		obj.SetUID("pending-uid")
		obj.SetResourceVersion("1")
		return false, nil, nil
	})
	var account *iam.ServiceAccount
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/serviceAccounts"):
			var create iam.CreateServiceAccountRequest
			require.NoError(t, json.NewDecoder(req.Body).Decode(&create))
			account = create.ServiceAccount
			account.Email = create.AccountId + "@identity-project.iam.gserviceaccount.com"
			// Simulate an attempted replay of a retired Google subject. Real Google
			// creates new numeric IDs; admission must still independently fence replay.
			account.UniqueId = "123456789012345678901"
			_ = json.NewEncoder(w).Encode(account)
		case strings.HasSuffix(req.URL.Path, ":getIamPolicy"), strings.HasSuffix(req.URL.Path, ":setIamPolicy"):
			_, _ = w.Write([]byte(`{}`))
		case req.Method == http.MethodGet:
			if account == nil {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"code":404}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(account)
		case req.Method == http.MethodDelete:
			account = nil
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected IAM request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	service, err := iam.NewService(t.Context(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	require.NoError(t, err)
	manager := &kitGKEIAM{project: "identity-project", workloadPoolProject: "pool-project", namespace: c.WorkloadNamespace, service: service, dynamic: client}
	ctx := context.WithValue(t.Context(), kitGKEIAMContextKey{}, &kitGKEIAMAttempt{manager: manager, runtime: r})
	binding, err := core.provisionKitWorkload(ctx, r, c, identity, "google-generation-one")
	require.NoError(t, err)
	require.Equal(t, "123456789012345678901", binding.Subject)
	require.Equal(t, identity.AgentID, binding.Identity.AgentID)
	assignments, err := workloadrepo.New(tx).GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: binding.IssuerID, Subject: binding.Subject, MatchKind: "exact"})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, identity.AgentID, assignments[0].AgentID)
	require.NoError(t, core.revokeKitWorkload(ctx, tx, kitWorkloadMetadata(t, r, binding)))
	// Also remove the issuer: historical withdrawn rows under deleted issuers
	// must still prevent subject resurrection.
	_, err = workloadrepo.New(tx).SoftDeleteWorkloadIssuer(ctx, workloadrepo.SoftDeleteWorkloadIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true}, ID: binding.IssuerID})
	require.NoError(t, err)
	_, err = core.provisionKitWorkload(ctx, r, c, identity, "google-generation-two")
	require.ErrorContains(t, err, "cannot be reused")
	require.NoError(t, manager.removePending(ctx, r))
}
