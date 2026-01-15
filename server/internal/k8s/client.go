package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/attr"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubernetesClients struct {
	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
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
			logger:        logger,
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

		k8sClients = &KubernetesClients{
			Clientset:     clientset,
			DynamicClient: dynamicClient,
			logger:        logger,
			enabled:       true,
			namespace:     fmt.Sprintf("gram-%s", env),
		}

		logger.InfoContext(ctx, "Kubernetes clients initialized successfully.")
	})

	return k8sClients, initErr
}

func (k *KubernetesClients) Enabled() bool {
	return k.enabled
}

func (k *KubernetesClients) CreateOrUpdateIngress(ctx context.Context, ingressName string, ingress *networkingv1.Ingress) error {
	existingIngress, err := k.GetIngress(ctx, ingressName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			k.logger.InfoContext(ctx, "Ingress not found, creating new one.",
				attr.SlogIngressName(ingressName),
			)
			_, createErr := k.Clientset.NetworkingV1().Ingresses(k.namespace).Create(ctx, ingress, metav1.CreateOptions{})
			if createErr != nil {
				k.logger.ErrorContext(ctx, "Failed to create Ingress",
					attr.SlogIngressName(ingressName),
					attr.SlogError(createErr),
				)
				return fmt.Errorf("failed to create ingress %s: %w", ingressName, createErr)
			}
			k.logger.InfoContext(ctx, "Ingress created successfully",
				attr.SlogIngressName(ingressName),
			)
		} else {
			k.logger.ErrorContext(ctx, "Failed to get Ingress",
				attr.SlogIngressName(ingressName),
				attr.SlogError(err),
			)
			return fmt.Errorf("failed to get ingress %s: %w", ingressName, err)
		}
	} else {
		k.logger.InfoContext(ctx, "Ingress found, attempting to update.",
			attr.SlogIngressName(ingressName),
		)
		ingress.ResourceVersion = existingIngress.ResourceVersion // Required for updates
		_, updateErr := k.Clientset.NetworkingV1().Ingresses(k.namespace).Update(ctx, ingress, metav1.UpdateOptions{})
		if updateErr != nil {
			k.logger.ErrorContext(ctx, "Failed to update Ingress",
				attr.SlogIngressName(ingressName),
				attr.SlogError(updateErr),
			)
			return fmt.Errorf("failed to update ingress %s: %w", ingressName, updateErr)
		}
		k.logger.InfoContext(ctx, "Ingress updated successfully",
			attr.SlogIngressName(ingressName),
		)
	}
	return nil
}

func (k *KubernetesClients) GetIngress(ctx context.Context, ingressName string) (*networkingv1.Ingress, error) {
	ingress, err := k.Clientset.NetworkingV1().Ingresses(k.namespace).Get(ctx, ingressName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get ingress: %w", err)
	}

	return ingress, nil
}

func (k *KubernetesClients) DeleteIngress(ctx context.Context, ingressName string, secretName string) error {
	ingressErr := k.Clientset.NetworkingV1().Ingresses(k.namespace).Delete(ctx, ingressName, metav1.DeleteOptions{})
	if ingressErr != nil {
		k.logger.ErrorContext(ctx, "Failed to delete ingress",
			attr.SlogIngressName(ingressName),
			attr.SlogError(ingressErr),
		)
		return fmt.Errorf("failed to delete ingress %s: %w", ingressName, ingressErr)
	}
	k.logger.InfoContext(ctx, "Ingress deleted successfully",
		attr.SlogIngressName(ingressName),
	)

	secretErr := k.Clientset.CoreV1().Secrets(k.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if secretErr != nil {
		k.logger.ErrorContext(ctx, "Failed to delete secret",
			attr.SlogSecretName(secretName),
			attr.SlogError(secretErr),
		)
		return fmt.Errorf("failed to delete secret %s: %w", secretName, secretErr)
	}
	k.logger.InfoContext(ctx, "Secret deleted successfully",
		attr.SlogSecretName(secretName),
	)

	return nil
}

func (k *KubernetesClients) CreateCustomDomainIngressCharts(domain string) (string, string, *networkingv1.Ingress, error) {
	nginxIngressClassName := "nginx"
	pathTypePrefix := networkingv1.PathTypePrefix
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific
	k8sName, err := SanitizeDomainForK8sName(domain)
	if err != nil {
		return "", "", nil, err
	}
	secretName := strings.ReplaceAll(domain, ".", "-") + "-tls"

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sName,
			Namespace: k.namespace,
			Annotations: map[string]string{
				"cert-manager.io/cluster-issuer":              "gram-letsencrypt",
				"nginx.ingress.kubernetes.io/proxy-body-size": "15m",
				"nginx.ingress.kubernetes.io/use-regex":       "true",
			},
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "custom-domain-chart",
				"custom-domain":                domain,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &nginxIngressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/mcp",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "gram-server",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									Path:     "/oauth",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "gram-server",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									// NGINX ingress validator rejects .well-known paths with pathType Prefix
									// Using regex with ImplementationSpecific bypasses this validation while achieving the same prefix matching behavior.
									// This matches: /.well-known/oauth-authorization-server/mcp and any subpaths
									Path:     `/\.well-known/oauth-authorization-server/mcp(/.*)?`,
									PathType: &pathTypeImplementationSpecific,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "gram-server",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									Path:     `/\.well-known/oauth-protected-resource/mcp(/.*)?`,
									PathType: &pathTypeImplementationSpecific,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "gram-server",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{domain},
					SecretName: secretName,
				},
			},
		},
	}
	return k8sName, secretName, ingress, nil
}
