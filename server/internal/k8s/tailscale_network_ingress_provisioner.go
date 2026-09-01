package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	tailscaleAPIGroup                = "tailscale.com"
	tailscaleAPIVersion              = "v1alpha1"
	networkIngressManagedBy          = "gram-network-ingress"
	networkIngressAppLabel           = "app.kubernetes.io/name"
	networkIngressIDLabel            = "gram.ai/network-ingress-id"
	tailscaleParentResourceType      = "tailscale.com/parent-resource-type"
	tailscaleParentResource          = "tailscale.com/parent-resource"
	tailscaleProxyGroupAnnotation    = "tailscale.com/proxy-group"
	tailscaleTagsAnnotation          = "tailscale.com/tags"
	tailscaleIngressClass            = "tailscale"
	networkIngressAttestorPort       = 8080
	networkIngressAttestorHealthPort = 8081
	networkIngressTokenAudience      = "gram-netingress"             // #nosec G101 -- Kubernetes TokenReview audience, not a credential.
	networkIngressTokenPath          = "/var/run/secrets/gram/token" // #nosec G101 -- projected token path, not token material.
	networkIngressCAPath             = "/var/run/secrets/gram/upstream/ca.crt"
)

var (
	tailnetGVR          = schema.GroupVersionResource{Group: tailscaleAPIGroup, Version: tailscaleAPIVersion, Resource: "tailnets"}
	proxyGroupGVR       = schema.GroupVersionResource{Group: tailscaleAPIGroup, Version: tailscaleAPIVersion, Resource: "proxygroups"}
	proxyGroupPolicyGVR = schema.GroupVersionResource{Group: tailscaleAPIGroup, Version: tailscaleAPIVersion, Resource: "proxygrouppolicies"}
)

type TailscaleCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type TailscaleNetworkIngressConfig struct {
	OperatorNamespace string
	BackendNamespace  string
	BackendPodLabels  map[string]string
	ProxyTag          string
	ServiceTag        string
	AttestorCASecret  string
	KubernetesAPICIDR string
	KubernetesAPIPort int32
	ClusterCIDRs      []string
}

type TailscaleNetworkIngressProvisioner struct {
	clientset kubernetes.Interface
	dynamic   dynamic.Interface
	config    TailscaleNetworkIngressConfig
}

var _ NetworkIngressProvisioner = (*TailscaleNetworkIngressProvisioner)(nil)

func NewTailscaleNetworkIngressProvisioner(clientset kubernetes.Interface, dynamicClient dynamic.Interface, config TailscaleNetworkIngressConfig) (*TailscaleNetworkIngressProvisioner, error) {
	if clientset == nil || dynamicClient == nil {
		return nil, fmt.Errorf("%w: Kubernetes clients are required", ErrNetworkIngressInvalidDesiredState)
	}
	if config.OperatorNamespace == "" || config.BackendNamespace == "" || len(config.BackendPodLabels) == 0 || config.ProxyTag == "" || config.ServiceTag == "" || config.AttestorCASecret == "" {
		return nil, fmt.Errorf("%w: Tailscale provisioner configuration is incomplete", ErrNetworkIngressInvalidDesiredState)
	}
	if config.KubernetesAPICIDR != "" {
		if _, _, err := net.ParseCIDR(config.KubernetesAPICIDR); err != nil || config.KubernetesAPIPort <= 0 {
			return nil, fmt.Errorf("%w: Kubernetes API network is invalid", ErrNetworkIngressInvalidDesiredState)
		}
	}
	config.ClusterCIDRs = append([]string{"169.254.0.0/16"}, config.ClusterCIDRs...)
	for _, cidr := range config.ClusterCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("%w: cluster network is invalid", ErrNetworkIngressInvalidDesiredState)
		}
	}
	return &TailscaleNetworkIngressProvisioner{clientset: clientset, dynamic: dynamicClient, config: config}, nil
}

//nolint:exhaustruct // observations intentionally populate only known current state.
func (p *TailscaleNetworkIngressProvisioner) Apply(ctx context.Context, desired NetworkIngressDesired) (NetworkIngressObservation, error) {
	if err := desired.Validate(); err != nil {
		return NetworkIngressObservation{Status: NetworkIngressStatusError, ErrorCode: NetworkIngressErrorInvalidDesiredState}, err
	}
	if desired.Provider != NetworkIngressProviderTailscale {
		return NetworkIngressObservation{Status: NetworkIngressStatusError, ErrorCode: NetworkIngressErrorUnsupportedProvider}, ErrNetworkIngressUnsupportedProvider
	}
	credentials, err := parseTailscaleCredentials(desired.Credentials)
	if err != nil {
		return NetworkIngressObservation{Status: NetworkIngressStatusError, ErrorCode: NetworkIngressErrorInvalidCredentials}, err
	}

	if err := p.applyNamespace(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyNetworkPolicies(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyCredentialsSecret(ctx, desired.Resources, credentials); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyDynamic(ctx, tailnetGVR, "", tailnetObject(desired)); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.replaceImmutableProxyGroup(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyDynamic(ctx, proxyGroupGVR, "", proxyGroupObject(desired, p.config.ProxyTag)); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyServiceAccount(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyAttestorCASecret(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	expectedHost, err := p.existingIngressHostname(ctx, desired.Resources)
	if err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if expectedHost == "" {
		expectedHost = desired.Hostname
	}
	if err := p.applyDeployment(ctx, desired, expectedHost); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyService(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyDynamic(ctx, proxyGroupPolicyGVR, desired.Resources.Namespace, proxyGroupPolicyObject(desired)); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	if err := p.applyIngress(ctx, desired); err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, err)
	}
	return p.Observe(ctx, desired.Resources)
}

//nolint:exhaustruct // observations intentionally populate only known current state.
func (p *TailscaleNetworkIngressProvisioner) Observe(ctx context.Context, resources NetworkIngressResourceNames) (NetworkIngressObservation, error) {
	if err := resources.Validate(); err != nil {
		return NetworkIngressObservation{Status: NetworkIngressStatusError, ErrorCode: NetworkIngressErrorInvalidDesiredState}, err
	}
	tailnet, err := p.dynamic.Resource(tailnetGVR).Get(ctx, resources.Tailnet, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return NetworkIngressObservation{Status: NetworkIngressStatusPending}, nil
	}
	if err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, fmt.Errorf("get Tailnet: %w", err))
	}
	proxyGroup, err := p.dynamic.Resource(proxyGroupGVR).Get(ctx, resources.ProxyGroup, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return NetworkIngressObservation{Status: NetworkIngressStatusPending}, nil
	}
	if err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, fmt.Errorf("get ProxyGroup: %w", err))
	}
	deployment, err := p.clientset.AppsV1().Deployments(resources.Namespace).Get(ctx, resources.AttestorDeployment, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return NetworkIngressObservation{Status: NetworkIngressStatusPending}, nil
	}
	if err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, fmt.Errorf("get attestor Deployment: %w", err))
	}
	ingress, err := p.clientset.NetworkingV1().Ingresses(resources.Namespace).Get(ctx, resources.Ingress, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return NetworkIngressObservation{Status: NetworkIngressStatusPending}, nil
	}
	if err != nil {
		return p.applyError(NetworkIngressErrorKubernetes, fmt.Errorf("get Tailscale Ingress: %w", err))
	}

	dnsName := ingressHostname(ingress)
	ready := unstructuredConditionTrue(tailnet, "TailnetReady") && unstructuredConditionTrue(proxyGroup, "ProxyGroupReady") && deployment.Status.AvailableReplicas > 0 && dnsName != "" && deploymentExpectedHost(deployment) == dnsName
	if ready {
		return NetworkIngressObservation{Status: NetworkIngressStatusOnline, DNSName: dnsName}, nil
	}
	return NetworkIngressObservation{Status: NetworkIngressStatusPending, DNSName: dnsName}, nil
}

func (p *TailscaleNetworkIngressProvisioner) verifyOwnedResources(ctx context.Context, resources NetworkIngressResourceNames) error {
	ownerID := resources.OwnerID.String()
	checks := []func() (map[string]string, error){
		func() (map[string]string, error) {
			object, err := p.clientset.CoreV1().Namespaces().Get(ctx, resources.Namespace, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("verify namespace ownership: %w", err)
			}
			return nonNilLabels(object.Labels), nil
		},
		func() (map[string]string, error) {
			object, err := p.clientset.CoreV1().Secrets(p.config.OperatorNamespace).Get(ctx, resources.CredentialsSecret, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("verify OAuth Secret ownership: %w", err)
			}
			return nonNilLabels(object.Labels), nil
		},
		func() (map[string]string, error) {
			object, err := p.dynamic.Resource(tailnetGVR).Get(ctx, resources.Tailnet, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("verify Tailnet ownership: %w", err)
			}
			return object.GetLabels(), nil
		},
		func() (map[string]string, error) {
			object, err := p.dynamic.Resource(proxyGroupGVR).Get(ctx, resources.ProxyGroup, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("verify ProxyGroup ownership: %w", err)
			}
			return object.GetLabels(), nil
		},
		func() (map[string]string, error) {
			object, err := p.clientset.NetworkingV1().NetworkPolicies(p.config.OperatorNamespace).Get(ctx, resources.ProxyNetworkPolicy, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("verify proxy NetworkPolicy ownership: %w", err)
			}
			return nonNilLabels(object.Labels), nil
		},
	}
	for _, check := range checks {
		labels, err := check()
		if err != nil {
			return err
		}
		if labels != nil {
			if err := ensureResourceOwned(labels, ownerID); err != nil {
				return fmt.Errorf("refuse to delete unowned resource: %w", err)
			}
		}
	}
	return nil
}

func (p *TailscaleNetworkIngressProvisioner) Delete(ctx context.Context, resources NetworkIngressResourceNames) error {
	if err := resources.Validate(); err != nil {
		return err
	}
	if err := p.verifyOwnedResources(ctx, resources); err != nil {
		return err
	}
	deletes := []func() error{
		func() error {
			return deleteTyped(p.clientset.NetworkingV1().Ingresses(resources.Namespace), ctx, resources.Ingress, "Ingress")
		},
		func() error {
			return deleteDynamic(p.dynamic.Resource(proxyGroupPolicyGVR).Namespace(resources.Namespace), ctx, resources.ProxyGroupPolicy, "ProxyGroupPolicy")
		},

		func() error {
			return deleteTyped(p.clientset.CoreV1().Services(resources.Namespace), ctx, resources.AttestorService, "attestor Service")
		},
		func() error {
			return deleteTyped(p.clientset.AppsV1().Deployments(resources.Namespace), ctx, resources.AttestorDeployment, "attestor Deployment")
		},
		func() error {
			return deleteDynamic(p.dynamic.Resource(proxyGroupGVR), ctx, resources.ProxyGroup, "ProxyGroup")
		},
		func() error { return deleteDynamic(p.dynamic.Resource(tailnetGVR), ctx, resources.Tailnet, "Tailnet") },
		func() error {
			return deleteTyped(p.clientset.CoreV1().Secrets(p.config.OperatorNamespace), ctx, resources.CredentialsSecret, "OAuth Secret")
		},
		func() error {
			return deleteTyped(p.clientset.CoreV1().Secrets(resources.Namespace), ctx, resources.AttestorCASecret, "attestor CA Secret")
		},
		func() error {
			return deleteTyped(p.clientset.CoreV1().ServiceAccounts(resources.Namespace), ctx, resources.AttestorServiceAccount, "attestor ServiceAccount")
		},
		func() error {
			return deleteTyped(p.clientset.NetworkingV1().NetworkPolicies(resources.Namespace), ctx, resources.AttestorNetworkPolicy, "attestor NetworkPolicy")
		},
		func() error {
			return deleteTyped(p.clientset.NetworkingV1().NetworkPolicies(p.config.OperatorNamespace), ctx, resources.ProxyNetworkPolicy, "proxy NetworkPolicy")
		},
		func() error {
			return deleteTyped(p.clientset.CoreV1().Namespaces(), ctx, resources.Namespace, "namespace")
		},
	}
	var errs []error
	for _, remove := range deletes {
		if err := remove(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func parseTailscaleCredentials(encoded []byte) (TailscaleCredentials, error) {
	var credentials TailscaleCredentials
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credentials); err != nil {
		return credentials, fmt.Errorf("%w: invalid Tailscale credential payload", ErrNetworkIngressInvalidDesiredState)
	}
	if credentials.ClientID == "" || credentials.ClientSecret == "" {
		return TailscaleCredentials{}, fmt.Errorf("%w: Tailscale credentials are incomplete", ErrNetworkIngressInvalidDesiredState)
	}
	return credentials, nil
}

//nolint:exhaustruct // error observations intentionally omit unavailable state.
func (p *TailscaleNetworkIngressProvisioner) applyError(code string, err error) (NetworkIngressObservation, error) {
	return NetworkIngressObservation{Status: NetworkIngressStatusError, ErrorCode: code}, err
}

func ingressLabels(desired NetworkIngressDesired) map[string]string {
	return map[string]string{
		managedByLabelKey:      networkIngressManagedBy,
		networkIngressIDLabel:  desired.ID.String(),
		networkIngressAppLabel: "gram-netingress-attestor",
	}
}

func nonNilLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return labels
}

func resourceOwnerLabels(ownerID string) map[string]string {
	return map[string]string{managedByLabelKey: networkIngressManagedBy, networkIngressIDLabel: ownerID}
}

func ensureResourceOwned(labels map[string]string, ownerID string) error {
	if labels[managedByLabelKey] != networkIngressManagedBy || labels[networkIngressIDLabel] != ownerID {
		return fmt.Errorf("resource ownership does not match persisted ingress")
	}
	return nil
}

func mergeLabels(existing, desired map[string]string) map[string]string {
	result := make(map[string]string, len(existing)+len(desired))
	maps.Copy(result, existing)
	maps.Copy(result, desired)
	return result
}

func unstructuredIngressLabels(desired NetworkIngressDesired) map[string]any {
	labels := ingressLabels(desired)
	result := make(map[string]any, len(labels))
	for key, value := range labels {
		result[key] = value
	}
	return result
}

//nolint:exhaustruct // Kubernetes desired-state literals omit API-owned defaults and status.
func (p *TailscaleNetworkIngressProvisioner) applyNamespace(ctx context.Context, desired NetworkIngressDesired) error {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.Namespace, Labels: resourceOwnerLabels(desired.ID.String())}}
	client := p.clientset.CoreV1().Namespaces()
	existing, err := client.Get(ctx, namespace.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, namespace, metav1.CreateOptions{})
		return wrapKubernetesMutation("create namespace", err)
	}
	if err != nil {
		return fmt.Errorf("get namespace: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt namespace: %w", err)
	}
	existing.Labels = mergeLabels(existing.Labels, namespace.Labels)
	_, err = client.Update(ctx, existing, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update namespace", err)
}

//nolint:exhaustruct // Kubernetes desired-state literals omit API-owned defaults and status.
func (p *TailscaleNetworkIngressProvisioner) applyCredentialsSecret(ctx context.Context, resources NetworkIngressResourceNames, credentials TailscaleCredentials) error {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: resources.CredentialsSecret, Namespace: p.config.OperatorNamespace, Labels: resourceOwnerLabels(resources.OwnerID.String())}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"client_id": []byte(credentials.ClientID), "client_secret": []byte(credentials.ClientSecret)}}
	client := p.clientset.CoreV1().Secrets(p.config.OperatorNamespace)
	existing, err := client.Get(ctx, secret.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, secret, metav1.CreateOptions{})
		return wrapKubernetesMutation("create OAuth Secret", err)
	}
	if err != nil {
		return fmt.Errorf("get OAuth Secret: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, resources.OwnerID.String()); err != nil {
		return fmt.Errorf("refuse to adopt OAuth Secret: %w", err)
	}
	secret.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, secret, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update OAuth Secret", err)
}

//nolint:exhaustruct // Kubernetes desired-state literals omit API-owned defaults and status.
func (p *TailscaleNetworkIngressProvisioner) applyAttestorCASecret(ctx context.Context, desired NetworkIngressDesired) error {
	source, err := p.clientset.CoreV1().Secrets(p.config.BackendNamespace).Get(ctx, p.config.AttestorCASecret, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get private listener CA Secret: %w", err)
	}
	ca, ok := source.Data["ca.crt"]
	if !ok || len(ca) == 0 {
		return fmt.Errorf("private listener CA Secret is invalid")
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.AttestorCASecret, Namespace: desired.Resources.Namespace, Labels: ingressLabels(desired)},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"ca.crt": append([]byte(nil), ca...)},
	}
	client := p.clientset.CoreV1().Secrets(desired.Resources.Namespace)
	existing, err := client.Get(ctx, secret.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, secret, metav1.CreateOptions{})
		return wrapKubernetesMutation("create attestor CA Secret", err)
	}
	if err != nil {
		return fmt.Errorf("get attestor CA Secret: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt attestor CA Secret: %w", err)
	}
	secret.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, secret, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update attestor CA Secret", err)
}

//nolint:exhaustruct // Kubernetes desired-state literals omit API-owned defaults and status.
func (p *TailscaleNetworkIngressProvisioner) applyServiceAccount(ctx context.Context, desired NetworkIngressDesired) error {
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.AttestorServiceAccount, Namespace: desired.Resources.Namespace, Labels: ingressLabels(desired)}, AutomountServiceAccountToken: new(false)}
	client := p.clientset.CoreV1().ServiceAccounts(desired.Resources.Namespace)
	existing, err := client.Get(ctx, account.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, account, metav1.CreateOptions{})
		return wrapKubernetesMutation("create attestor ServiceAccount", err)
	}
	if err != nil {
		return fmt.Errorf("get attestor ServiceAccount: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt attestor ServiceAccount: %w", err)
	}
	account.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, account, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update attestor ServiceAccount", err)
}

//nolint:exhaustruct // Kubernetes desired-state literals omit API-owned defaults and status.
func (p *TailscaleNetworkIngressProvisioner) applyDeployment(ctx context.Context, desired NetworkIngressDesired, expectedHost string) error {
	labels := ingressLabels(desired)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.AttestorDeployment, Namespace: desired.Resources.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName:           desired.Resources.AttestorServiceAccount,
					AutomountServiceAccountToken: new(false),
					Containers: []corev1.Container{{
						Name: "attestor", Image: desired.AttestorImage, ImagePullPolicy: corev1.PullIfNotPresent,
						Args:           []string{"netingress-attestor", "--upstream-url=https://" + desired.BackendService + "." + p.config.BackendNamespace + ".svc.cluster.local:" + strconv.Itoa(int(desired.BackendPort)), "--upstream-ca-file=" + networkIngressCAPath, "--expected-host=" + expectedHost, "--token-path=" + networkIngressTokenPath},
						Ports:          []corev1.ContainerPort{{Name: "http", ContainerPort: networkIngressAttestorPort}, {Name: "health", ContainerPort: networkIngressAttestorHealthPort}},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString("health")}}},
						VolumeMounts:   []corev1.VolumeMount{{Name: "attestation-token", MountPath: "/var/run/secrets/gram", ReadOnly: true}, {Name: "upstream-ca", MountPath: "/var/run/secrets/gram/upstream", ReadOnly: true}},
					}},
					Volumes: []corev1.Volume{
						{Name: "attestation-token", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", Audience: networkIngressTokenAudience, ExpirationSeconds: int64Ptr(600)}}}}}},
						{Name: "upstream-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: desired.Resources.AttestorCASecret}}},
					},
				},
			},
		},
	}
	client := p.clientset.AppsV1().Deployments(desired.Resources.Namespace)
	existing, err := client.Get(ctx, deployment.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, deployment, metav1.CreateOptions{})
		return wrapKubernetesMutation("create attestor Deployment", err)
	}
	if err != nil {
		return fmt.Errorf("get attestor Deployment: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt attestor Deployment: %w", err)
	}
	deployment.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, deployment, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update attestor Deployment", err)
}

//nolint:exhaustruct // Kubernetes desired-state literals omit API-owned defaults and status.
func (p *TailscaleNetworkIngressProvisioner) applyService(ctx context.Context, desired NetworkIngressDesired) error {
	labels := ingressLabels(desired)
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.AttestorService, Namespace: desired.Resources.Namespace, Labels: labels}, Spec: corev1.ServiceSpec{Selector: labels, Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http")}}}}
	client := p.clientset.CoreV1().Services(desired.Resources.Namespace)
	existing, err := client.Get(ctx, service.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, service, metav1.CreateOptions{})
		return wrapKubernetesMutation("create attestor Service", err)
	}
	if err != nil {
		return fmt.Errorf("get attestor Service: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt attestor Service: %w", err)
	}
	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP
	service.Spec.ClusterIPs = existing.Spec.ClusterIPs
	service.Spec.IPFamilies = existing.Spec.IPFamilies
	service.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	service.Spec.HealthCheckNodePort = existing.Spec.HealthCheckNodePort
	_, err = client.Update(ctx, service, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update attestor Service", err)
}

func (p *TailscaleNetworkIngressProvisioner) applyIngress(ctx context.Context, desired NetworkIngressDesired) error {
	className := tailscaleIngressClass
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.Ingress, Namespace: desired.Resources.Namespace, Labels: ingressLabels(desired), Annotations: map[string]string{tailscaleProxyGroupAnnotation: desired.Resources.ProxyGroup, tailscaleTagsAnnotation: p.config.ServiceTag}},
		Spec:       networkingv1.IngressSpec{IngressClassName: &className, TLS: []networkingv1.IngressTLS{{Hosts: []string{desired.Hostname}}}, DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: desired.Resources.AttestorService, Port: networkingv1.ServiceBackendPort{Name: "http"}}}},
	}
	client := p.clientset.NetworkingV1().Ingresses(desired.Resources.Namespace)
	existing, err := client.Get(ctx, ingress.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, ingress, metav1.CreateOptions{})
		return wrapKubernetesMutation("create Tailscale Ingress", err)
	}
	if err != nil {
		return fmt.Errorf("get Tailscale Ingress: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt Tailscale Ingress: %w", err)
	}
	ingress.ResourceVersion = existing.ResourceVersion
	ingress.Status = existing.Status
	for key, value := range existing.Annotations {
		if _, owned := ingress.Annotations[key]; !owned {
			ingress.Annotations[key] = value
		}
	}
	_, err = client.Update(ctx, ingress, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update Tailscale Ingress", err)
}

func (p *TailscaleNetworkIngressProvisioner) applyNetworkPolicies(ctx context.Context, desired NetworkIngressDesired) error {
	attestor := p.attestorNetworkPolicy(desired)
	if err := upsertNetworkPolicy(ctx, p.clientset, attestor); err != nil {
		return err
	}
	return upsertNetworkPolicy(ctx, p.clientset, p.proxyNetworkPolicy(desired))
}

func (p *TailscaleNetworkIngressProvisioner) attestorNetworkPolicy(desired NetworkIngressDesired) *networkingv1.NetworkPolicy {
	labels := ingressLabels(desired)
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.AttestorNetworkPolicy, Namespace: desired.Resources.Namespace, Labels: labels},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: labels}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: p.config.OperatorNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{tailscaleParentResourceType: "proxygroup", tailscaleParentResource: desired.Resources.ProxyGroup}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: new(intstr.FromInt(networkIngressAttestorPort))}}}},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: p.config.BackendNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: p.config.BackendPodLabels}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: new(intstr.FromInt32(desired.BackendPort))}}},
				dnsEgressRule(),
			},
		},
	}
}

func (p *TailscaleNetworkIngressProvisioner) proxyNetworkPolicy(desired NetworkIngressDesired) *networkingv1.NetworkPolicy {
	rules := []networkingv1.NetworkPolicyEgressRule{
		{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: desired.Resources.Namespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: ingressLabels(desired)}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: new(intstr.FromInt(networkIngressAttestorPort))}}},
		dnsEgressRule(),
	}
	if p.config.KubernetesAPICIDR != "" {
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: p.config.KubernetesAPICIDR}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: new(intstr.FromInt32(p.config.KubernetesAPIPort))}}})
	}
	rules = append(rules, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0", Except: append([]string(nil), p.config.ClusterCIDRs...)}}}})
	return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.ProxyNetworkPolicy, Namespace: p.config.OperatorNamespace, Labels: ingressLabels(desired)}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{tailscaleParentResourceType: "proxygroup", tailscaleParentResource: desired.Resources.ProxyGroup}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: rules}}
}

func dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	return networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: metav1.NamespaceSystem}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolUDP), Port: new(intstr.FromInt(53))}, {Protocol: protocolPtr(corev1.ProtocolTCP), Port: new(intstr.FromInt(53))}}}
}

func upsertNetworkPolicy(ctx context.Context, clientset kubernetes.Interface, policy *networkingv1.NetworkPolicy) error {
	client := clientset.NetworkingV1().NetworkPolicies(policy.Namespace)
	existing, err := client.Get(ctx, policy.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, policy, metav1.CreateOptions{})
		return wrapKubernetesMutation("create NetworkPolicy", err)
	}
	if err != nil {
		return fmt.Errorf("get NetworkPolicy: %w", err)
	}
	if err := ensureResourceOwned(existing.Labels, policy.Labels[networkIngressIDLabel]); err != nil {
		return fmt.Errorf("refuse to adopt NetworkPolicy: %w", err)
	}
	policy.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, policy, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update NetworkPolicy", err)
}

func (p *TailscaleNetworkIngressProvisioner) applyDynamic(ctx context.Context, gvr schema.GroupVersionResource, namespace string, desired *unstructured.Unstructured) error {
	var client dynamic.ResourceInterface = p.dynamic.Resource(gvr)
	if namespace != "" {
		client = p.dynamic.Resource(gvr).Namespace(namespace)
	}
	existing, err := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, desired, metav1.CreateOptions{})
		return wrapKubernetesMutation("create "+desired.GetKind(), err)
	}
	if err != nil {
		return fmt.Errorf("get %s: %w", desired.GetKind(), err)
	}
	if err := ensureResourceOwned(existing.GetLabels(), desired.GetLabels()[networkIngressIDLabel]); err != nil {
		return fmt.Errorf("refuse to adopt %s: %w", desired.GetKind(), err)
	}
	desired.SetResourceVersion(existing.GetResourceVersion())
	if status, ok := existing.Object["status"]; ok {
		desired.Object["status"] = status
	}
	_, err = client.Update(ctx, desired, metav1.UpdateOptions{})
	return wrapKubernetesMutation("update "+desired.GetKind(), err)
}

func (p *TailscaleNetworkIngressProvisioner) replaceImmutableProxyGroup(ctx context.Context, desired NetworkIngressDesired) error {
	existing, err := p.dynamic.Resource(proxyGroupGVR).Get(ctx, desired.Resources.ProxyGroup, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get ProxyGroup for immutable check: %w", err)
	}
	if existing.GetDeletionTimestamp() != nil {
		return ErrNetworkIngressReplacementPending
	}
	if err := ensureResourceOwned(existing.GetLabels(), desired.ID.String()); err != nil {
		return fmt.Errorf("refuse to adopt ProxyGroup: %w", err)
	}
	tailnet, _, err := unstructured.NestedString(existing.Object, "spec", "tailnet")
	if err != nil {
		return fmt.Errorf("read ProxyGroup tailnet: %w", err)
	}
	if tailnet == desired.Resources.Tailnet {
		return nil
	}
	if err := deleteTyped(p.clientset.NetworkingV1().Ingresses(desired.Resources.Namespace), ctx, desired.Resources.Ingress, "Ingress before ProxyGroup replacement"); err != nil {
		return err
	}
	if err := deleteDynamic(p.dynamic.Resource(proxyGroupGVR), ctx, desired.Resources.ProxyGroup, "immutable ProxyGroup"); err != nil {
		return err
	}
	return ErrNetworkIngressReplacementPending
}

func tailnetObject(desired NetworkIngressDesired) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": tailscaleAPIGroup + "/" + tailscaleAPIVersion, "kind": "Tailnet", "metadata": map[string]any{"name": desired.Resources.Tailnet, "labels": unstructuredIngressLabels(desired)}, "spec": map[string]any{"credentials": map[string]any{"secretName": desired.Resources.CredentialsSecret}}}}
}

func proxyGroupObject(desired NetworkIngressDesired, proxyTag string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": tailscaleAPIGroup + "/" + tailscaleAPIVersion, "kind": "ProxyGroup", "metadata": map[string]any{"name": desired.Resources.ProxyGroup, "labels": unstructuredIngressLabels(desired)}, "spec": map[string]any{"type": "ingress", "replicas": int64(2), "tailnet": desired.Resources.Tailnet, "tags": []any{proxyTag}}}}
}

func proxyGroupPolicyObject(desired NetworkIngressDesired) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": tailscaleAPIGroup + "/" + tailscaleAPIVersion, "kind": "ProxyGroupPolicy", "metadata": map[string]any{"name": desired.Resources.ProxyGroupPolicy, "namespace": desired.Resources.Namespace, "labels": unstructuredIngressLabels(desired)}, "spec": map[string]any{"ingress": []any{desired.Resources.ProxyGroup}, "egress": []any{}}}}
}

func unstructuredConditionTrue(resource *unstructured.Unstructured, conditionType string) bool {
	conditions, found, err := unstructured.NestedSlice(resource.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, candidate := range conditions {
		condition, ok := candidate.(map[string]any)
		if ok && condition["type"] == conditionType && condition["status"] == "True" {
			return true
		}
	}
	return false
}

func ingressHostname(ingress *networkingv1.Ingress) string {
	if ingress == nil || len(ingress.Status.LoadBalancer.Ingress) == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(ingress.Status.LoadBalancer.Ingress[0].Hostname), ".")
}

func (p *TailscaleNetworkIngressProvisioner) existingIngressHostname(ctx context.Context, resources NetworkIngressResourceNames) (string, error) {
	ingress, err := p.clientset.NetworkingV1().Ingresses(resources.Namespace).Get(ctx, resources.Ingress, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get existing Tailscale Ingress hostname: %w", err)
	}
	return ingressHostname(ingress), nil
}

func deploymentExpectedHost(deployment *appsv1.Deployment) string {
	if deployment == nil || len(deployment.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	const prefix = "--expected-host="
	for _, argument := range deployment.Spec.Template.Spec.Containers[0].Args {
		if after, ok := strings.CutPrefix(argument, prefix); ok {
			return after
		}
	}
	return ""
}

func wrapKubernetesMutation(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

type namedDeleter interface {
	Delete(context.Context, string, metav1.DeleteOptions) error
}

func deleteTyped(client namedDeleter, ctx context.Context, name, kind string) error {
	err := client.Delete(ctx, name, metav1.DeleteOptions{})
	if err == nil || k8serrors.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("delete %s: %w", kind, err)
}

func deleteDynamic(client dynamic.ResourceInterface, ctx context.Context, name, kind string) error {
	err := client.Delete(ctx, name, metav1.DeleteOptions{})
	if err == nil || k8serrors.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("delete %s: %w", kind, err)
}

//go:fix inline
func boolPtr(value bool) *bool { return new(value) }

//go:fix inline
func int32Ptr(value int32) *int32 { return new(value) }

//go:fix inline
func int64Ptr(value int64) *int64 { return new(value) }

//go:fix inline
func protocolPtr(value corev1.Protocol) *corev1.Protocol { return new(value) }

//go:fix inline
func intstrPtr(value intstr.IntOrString) *intstr.IntOrString { return new(value) }
