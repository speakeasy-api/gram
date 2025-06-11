package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubernetesClients struct {
	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	logger        *slog.Logger
	enabled       bool
}

var (
	k8sClients *KubernetesClients
	initOnce   sync.Once
)

// InitializeK8sClient initializes and returns KubernetesClients singleton.
func InitializeK8sClient(ctx context.Context, logger *slog.Logger, env string) (*KubernetesClients, error) {
	// not supporting k8s client in local dev mode currently
	if env == "local" {
		return &KubernetesClients{
			Clientset:     nil,
			DynamicClient: nil,
			logger:        logger,
			enabled:       false,
		}, nil
	}

	var initErr error
	initOnce.Do(func() {
		config, err := rest.InClusterConfig()
		if err != nil {
			initErr = fmt.Errorf("failed to get in-cluster config: %w", err)
			return
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			initErr = fmt.Errorf("failed to create typed clientset: %w", err)
			return
		}
		dynamicClient, err := dynamic.NewForConfig(config)
		if err != nil {
			initErr = fmt.Errorf("failed to create dynamic client: %w", err)
			return
		}

		k8sClients = &KubernetesClients{
			Clientset:     clientset,
			DynamicClient: dynamicClient,
			logger:        logger,
			enabled:       true,
		}

		logger.InfoContext(ctx, "Kubernetes clients initialized successfully.")
	})

	return k8sClients, initErr
}
