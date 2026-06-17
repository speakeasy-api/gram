package k8s

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// gkeAuthScope is the OAuth2 scope GKE accepts for authenticating a Google
// access token against the cluster API (mapped to the caller's identity via the
// cluster's RBAC).
const gkeAuthScope = "https://www.googleapis.com/auth/cloud-platform"

// NewRemoteDynamicClient builds a dynamic client for a remote GKE cluster (the
// assistant runtime cluster) authenticated with the caller's Google credentials:
// workload identity when running in a cluster, Application Default Credentials
// locally. Unlike InitializeK8sClient it does not require running inside the
// target cluster, so the gram server reaches a separate assistant cluster the
// same way it reaches Fly — by endpoint and credentials, not in-cluster config.
func NewRemoteDynamicClient(ctx context.Context, endpoint string, caCert []byte) (dynamic.Interface, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("remote cluster endpoint is required")
	}
	if len(caCert) == 0 {
		return nil, fmt.Errorf("remote cluster CA certificate is required")
	}

	tokenSource, err := google.DefaultTokenSource(ctx, gkeAuthScope)
	if err != nil {
		return nil, fmt.Errorf("resolve google token source for remote cluster: %w", err)
	}

	config := &rest.Config{
		Host:            "https://" + endpoint,
		TLSClientConfig: rest.TLSClientConfig{CAData: caCert},
	}
	config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &oauth2.Transport{Source: tokenSource, Base: rt}
	})

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create remote dynamic client: %w", err)
	}
	return client, nil
}
