package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestValidateNetworkIngressWorkerTemporalTLS(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, environment, cert, key string
		wantErr                      string
	}{
		{name: "local plaintext", environment: "local"},
		{name: "dev mtls", environment: "dev", cert: "cert", key: "key"},
		{name: "prod missing", environment: "prod", wantErr: "mTLS is required"},
		{name: "cert only", environment: "local", cert: "cert", wantErr: "configured together"},
		{name: "key only", environment: "local", key: "key", wantErr: "configured together"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateNetworkIngressWorkerTemporalTLS(test.environment, test.cert, test.key)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestCheckNetworkIngressWorkerKubernetesRequiresInventoryRBAC(t *testing.T) {
	t.Parallel()
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "allowed", false: "denied"}[allowed], func(t *testing.T) {
			t.Parallel()
			clientset := fake.NewSimpleClientset()
			clientset.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				create := action.(ktesting.CreateAction)
				review := create.GetObject().(*authorizationv1.SelfSubjectAccessReview)
				require.Equal(t, "list", review.Spec.ResourceAttributes.Verb)
				require.Equal(t, "namespaces", review.Spec.ResourceAttributes.Resource)
				return true, &authorizationv1.SelfSubjectAccessReview{
					ObjectMeta: metav1.ObjectMeta{},
					Status:     authorizationv1.SubjectAccessReviewStatus{Allowed: allowed},
				}, nil
			})
			err := checkNetworkIngressWorkerKubernetes(t.Context(), clientset)
			if allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "not allowed")
			}
		})
	}
}
