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
	clientset      kubernetes.Interface
	namespace      string
	backendService string
	logger         *slog.Logger
}

func (p *IngressProvisioner) Kind() ProvisionerKind { return ProvisionerKindIngress }

func (p *IngressProvisioner) Apply(ctx context.Context, config RouteConfig) (SetupResult, error) {
	k8sName, secretName, ingress, err := p.buildIngress(config.Domain, config.IPAllowlist)
	if err != nil {
		return SetupResult{}, fmt.Errorf("build ingress: %w", err)
	}

	if err := p.upsertIngress(ctx, ingress); err != nil {
		return SetupResult{}, err
	}

	rootName, err := RootIngressName(k8sName)
	if err != nil {
		return SetupResult{}, fmt.Errorf("build root ingress name: %w", err)
	}
	wellKnownRootName, err := WellKnownRootIngressName(k8sName)
	if err != nil {
		return SetupResult{}, fmt.Errorf("build well-known root ingress name: %w", err)
	}
	if config.RootTarget == nil {
		if err := p.deleteIngress(ctx, rootName); err != nil {
			return SetupResult{}, err
		}
		if err := p.deleteIngress(ctx, wellKnownRootName); err != nil {
			return SetupResult{}, err
		}
	} else {
		rootIngress := p.buildRootIngress(rootName, config.Domain, secretName, config.IPAllowlist, *config.RootTarget)
		if err := p.upsertIngress(ctx, rootIngress); err != nil {
			return SetupResult{}, err
		}
		wellKnownRootIngress := p.buildWellKnownRootIngress(
			wellKnownRootName,
			config.Domain,
			secretName,
			config.IPAllowlist,
			"/.well-known/$1"+*config.RootTarget,
		)
		if err := p.upsertIngress(ctx, wellKnownRootIngress); err != nil {
			return SetupResult{}, err
		}
	}

	return SetupResult{ResourceName: k8sName, SecretName: secretName}, nil
}

func (p *IngressProvisioner) upsertIngress(ctx context.Context, ingress *networkingv1.Ingress) error {
	ingresses := p.clientset.NetworkingV1().Ingresses(p.namespace)
	existing, err := ingresses.Get(ctx, ingress.Name, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get ingress %s: %w", ingress.Name, err)
		}
		p.logger.InfoContext(ctx, "ingress not found, creating", attr.SlogIngressName(ingress.Name))
		if _, createErr := ingresses.Create(ctx, ingress, metav1.CreateOptions{}); createErr != nil {
			return fmt.Errorf("create ingress %s: %w", ingress.Name, createErr)
		}
		return nil
	}

	p.logger.InfoContext(ctx, "ingress found, updating", attr.SlogIngressName(ingress.Name))
	ingress.ResourceVersion = existing.ResourceVersion
	if _, updateErr := ingresses.Update(ctx, ingress, metav1.UpdateOptions{}); updateErr != nil {
		return fmt.Errorf("update ingress %s: %w", ingress.Name, updateErr)
	}
	return nil
}

func (p *IngressProvisioner) Get(ctx context.Context, resourceName string) error {
	_, err := p.clientset.NetworkingV1().Ingresses(p.namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get ingress: %w", err)
	}
	return nil
}

func (p *IngressProvisioner) Delete(ctx context.Context, resourceName, secretName string) error {
	rootName, err := RootIngressName(resourceName)
	if err != nil {
		return fmt.Errorf("build root ingress name: %w", err)
	}
	wellKnownRootName, err := WellKnownRootIngressName(resourceName)
	if err != nil {
		return fmt.Errorf("build well-known root ingress name: %w", err)
	}
	if err := p.deleteIngress(ctx, wellKnownRootName); err != nil {
		return err
	}
	if err := p.deleteIngress(ctx, rootName); err != nil {
		return err
	}
	if err := p.deleteIngress(ctx, resourceName); err != nil {
		return err
	}

	if secretName == "" {
		return nil
	}
	err = p.clientset.CoreV1().Secrets(p.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete secret %s: %w", secretName, err)
	}
	if err == nil {
		p.logger.InfoContext(ctx, "secret deleted", attr.SlogSecretName(secretName))
	}

	return nil
}

func (p *IngressProvisioner) deleteIngress(ctx context.Context, name string) error {
	err := p.clientset.NetworkingV1().Ingresses(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete ingress %s: %w", name, err)
	}
	if err == nil {
		p.logger.InfoContext(ctx, "ingress deleted", attr.SlogIngressName(name))
	}
	return nil
}

func (p *IngressProvisioner) buildIngress(domain string, ipAllowlist []string) (string, string, *networkingv1.Ingress, error) {
	nginxIngressClassName := "nginx"
	pathTypePrefix := networkingv1.PathTypePrefix
	pathTypeExact := networkingv1.PathTypeExact
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific
	k8sName, err := SanitizeDomainForK8sName(domain)
	if err != nil {
		return "", "", nil, err
	}
	secretName := TLSSecretNameForDomain(domain)

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
				managedByLabelKey:    managedByLabelValue,
				customDomainLabelKey: domain,
				customDomainRoleKey:  customDomainRoleMain,
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
											Name: p.backendService,
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									Path:     "/oauth",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: p.backendService,
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									// Public skill share pages (/shared/skills/{token})
									// are served by the app on custom domains.
									Path:     "/shared/skills",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: p.backendService,
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									Path:     "/.well-known/openai-apps-challenge",
									PathType: &pathTypeExact,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: p.backendService,
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
											Name: p.backendService,
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
								{
									Path:     `/\.well-known/oauth-protected-resource/mcp(/.*)?`,
									PathType: &pathTypeImplementationSpecific,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: p.backendService,
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

func (p *IngressProvisioner) buildRootIngress(name, domain, secretName string, ipAllowlist []string, rootTarget string) *networkingv1.Ingress {
	nginxIngressClassName := "nginx"
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific
	annotations := map[string]string{
		"nginx.ingress.kubernetes.io/proxy-body-size": "15m",
		"nginx.ingress.kubernetes.io/rewrite-target":  rootTarget,
		"nginx.ingress.kubernetes.io/use-regex":       "true",
	}
	if len(ipAllowlist) > 0 {
		annotations["nginx.ingress.kubernetes.io/whitelist-source-range"] = strings.Join(ipAllowlist, ",")
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   p.namespace,
			Annotations: annotations,
			Labels: map[string]string{
				managedByLabelKey:    managedByLabelValue,
				customDomainLabelKey: domain,
				customDomainRoleKey:  customDomainRoleRoot,
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
									Path:     "/$",
									PathType: &pathTypeImplementationSpecific,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: p.backendService,
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
}

func (p *IngressProvisioner) buildWellKnownRootIngress(name, domain, secretName string, ipAllowlist []string, rewriteTarget string) *networkingv1.Ingress {
	nginxIngressClassName := "nginx"
	pathTypeImplementationSpecific := networkingv1.PathTypeImplementationSpecific
	annotations := map[string]string{
		"nginx.ingress.kubernetes.io/proxy-body-size": "15m",
		"nginx.ingress.kubernetes.io/rewrite-target":  rewriteTarget,
		"nginx.ingress.kubernetes.io/use-regex":       "true",
	}
	if len(ipAllowlist) > 0 {
		annotations["nginx.ingress.kubernetes.io/whitelist-source-range"] = strings.Join(ipAllowlist, ",")
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   p.namespace,
			Annotations: annotations,
			Labels: map[string]string{
				managedByLabelKey:    managedByLabelValue,
				customDomainLabelKey: domain,
				customDomainRoleKey:  customDomainRoleWellKnownRoot,
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
									Path:     `/\.well-known/(oauth-protected-resource|oauth-authorization-server)$`,
									PathType: &pathTypeImplementationSpecific,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: p.backendService,
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
}
