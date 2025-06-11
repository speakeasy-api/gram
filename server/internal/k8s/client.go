package k8s

import (
	"fmt"
	"log"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubernetesClients struct {
	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
}

var (
	k8sClients *KubernetesClients
	initOnce   sync.Once
)

// InitializeK8sClient initializes and returns KubernetesClients singleton.
func InitializeK8sClient() (*KubernetesClients, error) {
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
		}

		log.Println("Kubernetes clients initialized successfully.")
	})

	return k8sClients, initErr
}
