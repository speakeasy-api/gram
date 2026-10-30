package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubernetesClients struct {
	Clientset            kubernetes.Interface
	DynamicClient        dynamic.Interface
	logger               *slog.Logger
	namespace            string
	mcpBackendService    string
	serverBackendService string
	enabled              bool
}

var (
	k8sClients *KubernetesClients
	initOnce   sync.Once
)

// InitializeK8sClient initializes and returns KubernetesClients singleton.
// namespace and backend services override where custom domain ingresses are
// created and which services they route to. Empty values fall back to the
// historical gram-<env> and gram-server defaults; an empty server backend uses
// the MCP backend to preserve existing routing during rollout.
func InitializeK8sClient(ctx context.Context, logger *slog.Logger, env string, namespace string, mcpBackendService string, serverBackendService string) (*KubernetesClients, error) {
	// not supporting k8s client in local dev mode currently
	if env == "local" {
		return &KubernetesClients{
			Clientset:            nil,
			DynamicClient:        nil,
			logger:               logger.With(attr.SlogComponent("k8s_client")),
			enabled:              false,
			namespace:            "",
			mcpBackendService:    "",
			serverBackendService: "",
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

		mcpBackendService, serverBackendService = customDomainBackendServices(mcpBackendService, serverBackendService)
		k8sClients = &KubernetesClients{
			Clientset:            clientset,
			DynamicClient:        dynamicClient,
			logger:               logger,
			enabled:              true,
			namespace:            conv.Default(namespace, fmt.Sprintf("gram-%s", env)),
			mcpBackendService:    mcpBackendService,
			serverBackendService: serverBackendService,
		}

		logger.InfoContext(ctx, "Kubernetes clients initialized successfully.")
	})

	return k8sClients, initErr
}

func customDomainBackendServices(mcpBackendService string, serverBackendService string) (string, string) {
	mcpBackendService = conv.Default(mcpBackendService, "gram-server")
	return mcpBackendService, conv.Default(serverBackendService, mcpBackendService)
}

// Provisioner returns a CustomDomainProvisioner for the given kind.
// When k8s is disabled (local env), returns a no-op StubProvisioner for any kind.
func (k *KubernetesClients) Provisioner(kind ProvisionerKind) CustomDomainProvisioner {
	if !k.enabled {
		return &StubProvisioner{kind: kind, logger: k.logger, calls: nil}
	}
	return &IngressProvisioner{
		clientset:            k.Clientset,
		namespace:            k.namespace,
		mcpBackendService:    k.mcpBackendService,
		serverBackendService: k.serverBackendService,
		logger:               k.logger,
	}
}
