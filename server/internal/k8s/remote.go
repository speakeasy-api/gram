package k8s

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// gkeAuthScope is the OAuth2 scope GKE accepts for authenticating a Google
// access token against the cluster API (mapped to the caller's identity via the
// cluster's RBAC).
const gkeAuthScope = "https://www.googleapis.com/auth/cloud-platform"

// RemoteClientOptions bounds a dedicated client's API request rate.
type RemoteClientOptions struct {
	// QPS caps sustained requests per second across the client.
	QPS float32
	// Burst allows bounded simultaneous admissions after an idle period.
	Burst int
}

// NewRemoteDynamicClient builds a dynamic client for a remote GKE cluster (the
// assistant runtime cluster) authenticated with the caller's Google credentials:
// workload identity when running in a cluster, Application Default Credentials
// locally. Unlike InitializeK8sClient it does not require running inside the
// target cluster, so the gram server reaches a separate assistant cluster the
// same way it reaches Fly — by endpoint and credentials, not in-cluster config.
func NewRemoteDynamicClient(ctx context.Context, endpoint string, caCert []byte, options ...RemoteClientOptions) (dynamic.Interface, error) {
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

	// Accept a bare host (the common case) or a full URL — prepend the scheme
	// only when the caller didn't, so an endpoint that already carries one
	// doesn't become "https://https://...".
	host := endpoint
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}

	config := &rest.Config{
		Host:            host,
		TLSClientConfig: rest.TLSClientConfig{CAData: caCert},
	}
	if len(options) > 1 {
		return nil, fmt.Errorf("at most one remote client option set is accepted")
	}
	if len(options) == 1 {
		if options[0].QPS <= 0 || options[0].Burst <= 0 {
			return nil, fmt.Errorf("remote client QPS and burst must be positive")
		}
		config.QPS, config.Burst = options[0].QPS, options[0].Burst
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
