package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

var _ CustomDomainProvisioner = (*StubProvisioner)(nil)
var _ CustomDomainProvisioner = (*IngressProvisioner)(nil)
var _ CustomDomainProvisioner = (*GatewayProvisioner)(nil)

type KubernetesClients struct {
	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	gatewayClient gatewayclient.Interface
	logger        *slog.Logger
	namespace     string
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
			gatewayClient: nil,
			logger:        logger.With(attr.SlogComponent("k8s_client")),
			enabled:       false,
			namespace:     "",
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

		gatewayClient, err := gatewayclient.NewForConfig(config)
		if err != nil {
			initErr = fmt.Errorf("failed to create gateway client: %w", err)
			return
		}

		k8sClients = &KubernetesClients{
			Clientset:     clientset,
			DynamicClient: dynamicClient,
			gatewayClient: gatewayClient,
			logger:        logger,
			enabled:       true,
			namespace:     fmt.Sprintf("gram-%s", env),
		}

		logger.InfoContext(ctx, "Kubernetes clients initialized successfully.")
	})

	return k8sClients, initErr
}

// Provisioner returns a CustomDomainProvisioner for the given kind.
// When k8s is disabled (local env), returns a no-op StubProvisioner for any kind.
func (k *KubernetesClients) Provisioner(kind ProvisionerKind) CustomDomainProvisioner {
	if !k.enabled {
		return &StubProvisioner{kind: kind, logger: k.logger, calls: nil}
	}
	if kind == ProvisionerKindGateway {
		return &GatewayProvisioner{client: k.gatewayClient, namespace: k.namespace, logger: k.logger}
	}
	return &IngressProvisioner{clientset: k.Clientset, namespace: k.namespace, logger: k.logger}
}
