package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

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

//nolint:exhaustruct // We intentionally do not exhaustively fill all struct fields for Kubernetes API objects, as only a subset are required and others are left to their zero values or managed by the API server.
func (k *KubernetesClients) CreateOrUpdateIngress(ctx context.Context, ingressName string, ingress *networkingv1.Ingress) error {
	existingIngress, err := k.GetIngress(ctx, ingressName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			k.logger.InfoContext(ctx, "Ingress not found, creating new one.",
				slog.String("name", ingressName),
			)
			_, createErr := k.Clientset.NetworkingV1().Ingresses(k.namespace).Create(ctx, ingress, metav1.CreateOptions{})
			if createErr != nil {
				k.logger.ErrorContext(ctx, "Failed to create Ingress",
					slog.String("name", ingressName),
					slog.Any("error", createErr),
				)
				return fmt.Errorf("failed to create ingress %s: %w", ingressName, createErr)
			}
			k.logger.InfoContext(ctx, "Ingress created successfully",
				slog.String("name", ingressName),
			)
		} else {
			k.logger.ErrorContext(ctx, "Failed to get Ingress",
				slog.String("name", ingressName),
				slog.Any("error", err),
			)
			return fmt.Errorf("failed to get ingress %s: %w", ingressName, err)
		}
	} else {
		k.logger.InfoContext(ctx, "Ingress found, attempting to update.",
			slog.String("name", ingressName),
		)
		ingress.ResourceVersion = existingIngress.ResourceVersion // Required for updates
		_, updateErr := k.Clientset.NetworkingV1().Ingresses(k.namespace).Update(ctx, ingress, metav1.UpdateOptions{})
		if updateErr != nil {
			k.logger.ErrorContext(ctx, "Failed to update Ingress",
				slog.String("name", ingressName),
				slog.Any("error", updateErr),
			)
			return fmt.Errorf("failed to update ingress %s: %w", ingressName, updateErr)
		}
		k.logger.InfoContext(ctx, "Ingress updated successfully",
			slog.String("name", ingressName),
		)
	}
	return nil
}

//nolint:exhaustruct // We intentionally do not exhaustively fill all struct fields for Kubernetes API objects, as only a subset are required and others are left to their zero values or managed by the API server.
func (k *KubernetesClients) GetIngress(ctx context.Context, ingressName string) (*networkingv1.Ingress, error) {
	return k.Clientset.NetworkingV1().Ingresses(k.namespace).Get(ctx, ingressName, metav1.GetOptions{})
}

//nolint:exhaustruct // We intentionally do not exhaustively fill all struct fields for Kubernetes API objects, as only a subset are required and others are left to their zero values or managed by the API server.
func (k *KubernetesClients) DeleteIngress(ctx context.Context, ingressName string, secretName string) error {
	ingressErr := k.Clientset.NetworkingV1().Ingresses(k.namespace).Delete(ctx, ingressName, metav1.DeleteOptions{})
	if ingressErr != nil {
		k.logger.ErrorContext(ctx, "Failed to delete ingress",
			slog.String("name", ingressName),
			slog.Any("error", ingressErr),
		)
		return fmt.Errorf("failed to delete ingress %s: %w", ingressName, ingressErr)
	}
	k.logger.InfoContext(ctx, "Ingress deleted successfully",
		slog.String("name", ingressName),
	)

	secretErr := k.Clientset.CoreV1().Secrets(k.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if secretErr != nil {
		k.logger.ErrorContext(ctx, "Failed to delete secret",
			slog.String("name", secretName),
			slog.Any("error", secretErr),
		)
		return fmt.Errorf("failed to delete secret %s: %w", secretName, secretErr)
	}
	k.logger.InfoContext(ctx, "Secret deleted successfully",
		slog.String("name", secretName),
	)

	return nil
}

//nolint:exhaustruct // We intentionally do not exhaustively fill all struct fields for Kubernetes API objects, as only a subset are required and others are left to their zero values or managed by the API server.
func (k *KubernetesClients) CreateCustomDomainIngressCharts(domain string) (string, string, *networkingv1.Ingress, error) {
	nginxIngressClassName := "nginx"
	pathTypePrefix := networkingv1.PathTypePrefix
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
