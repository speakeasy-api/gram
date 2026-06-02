package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type IngressProvisioner struct {
	clientset kubernetes.Interface
	namespace string
	logger    *slog.Logger
}

func (p *IngressProvisioner) Kind() ProvisionerKind { return ProvisionerKindIngress }

func (p *IngressProvisioner) Setup(ctx context.Context, domain string, ipAllowlist []string) (SetupResult, error) {
	k8sName, secretName, ingress, err := p.buildIngress(domain, ipAllowlist)
	if err != nil {
		return SetupResult{}, fmt.Errorf("build ingress: %w", err)
	}

	existing, err := p.clientset.NetworkingV1().Ingresses(p.namespace).Get(ctx, k8sName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			p.logger.InfoContext(ctx, "ingress not found, creating", attr.SlogIngressName(k8sName))
			if _, createErr := p.clientset.NetworkingV1().Ingresses(p.namespace).Create(ctx, ingress, metav1.CreateOptions{}); createErr != nil {
				return SetupResult{}, fmt.Errorf("create ingress %s: %w", k8sName, createErr)
			}
		} else {
			return SetupResult{}, fmt.Errorf("get ingress %s: %w", k8sName, err)
		}
	} else {
		p.logger.InfoContext(ctx, "ingress found, updating", attr.SlogIngressName(k8sName))
		ingress.ResourceVersion = existing.ResourceVersion
		if _, updateErr := p.clientset.NetworkingV1().Ingresses(p.namespace).Update(ctx, ingress, metav1.UpdateOptions{}); updateErr != nil {
			return SetupResult{}, fmt.Errorf("update ingress %s: %w", k8sName, updateErr)
		}
	}

	return SetupResult{ResourceName: k8sName, SecretName: secretName}, nil
}

func (p *IngressProvisioner) Get(ctx context.Context, resourceName string) error {
	_, err := p.clientset.NetworkingV1().Ingresses(p.namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get ingress: %w", err)
	}
	return nil
}

func (p *IngressProvisioner) Delete(ctx context.Context, resourceName, secretName string) error {
	if err := p.clientset.NetworkingV1().Ingresses(p.namespace).Delete(ctx, resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete ingress %s: %w", resourceName, err)
	}
	p.logger.InfoContext(ctx, "ingress deleted", attr.SlogIngressName(resourceName))

	if err := p.clientset.CoreV1().Secrets(p.namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete secret %s: %w", secretName, err)
	}
	p.logger.InfoContext(ctx, "secret deleted", attr.SlogSecretName(secretName))

	return nil
}

func (p *IngressProvisioner) buildIngress(domain string, ipAllowlist []string) (string, string, *networkingv1.Ingress, error) {
	nginxIngressClassName := "nginx"
	pathTypePrefix := networkingv1.PathTypePrefix
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific
	k8sName, err := SanitizeDomainForK8sName(domain)
	if err != nil {
		return "", "", nil, err
	}
	secretName := strings.ReplaceAll(domain, ".", "-") + "-tls"

	annotations := map[string]string{
		"cert-manager.io/cluster-issuer":              "gram-letsencrypt",
		"nginx.ingress.kubernetes.io/proxy-body-size": "15m",
		"nginx.ingress.kubernetes.io/use-regex":       "true",
	}
	// A non-empty allowlist restricts inbound traffic to the given IPv4 sources.
	// An empty list omits the annotation entirely, which removes any prior
	// restriction since Update replaces the whole object.
	if len(ipAllowlist) > 0 {
		annotations["nginx.ingress.kubernetes.io/whitelist-source-range"] = strings.Join(ipAllowlist, ",")
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        k8sName,
			Namespace:   p.namespace,
			Annotations: annotations,
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
									// NGINX ingress validator rejects .well-known paths with pathType Prefix.
									// Using regex with ImplementationSpecific bypasses this validation.
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
