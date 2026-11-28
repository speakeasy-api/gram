package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/atecred"
)

const (
	// ateKitCPUMillicores is the vCPU limit booked per ATE Kit actor (1 vCPU).
	ateKitCPUMillicores int64 = 1000

	// ateKitMemoryBytes is the memory limit booked per ATE Kit actor (1 GiB).
	ateKitMemoryBytes int64 = 1 << 30 // 1 GiB

	// ateKitWakeupTimeoutSeconds bounds how long ATE waits for kit-ate-init /readyz on wakeup.
	ateKitWakeupTimeoutSeconds int32 = 120

	// ateHTTPPort is the standard cleartext HTTP destination port permitted for broad web access.
	ateHTTPPort int32 = 80

	// ateHTTPSPort is the default HTTPS destination port.
	ateHTTPSPort int32 = 443

	// ateEgressTrustBundleVolumeName names the projected SystemInfo trust-bundle volume on ActorTemplates.
	ateEgressTrustBundleVolumeName = "egress-trust-bundle"

	// ateEgressTrustBundleMountPath is the read-only mount point for ATE's projected CA trust bundle.
	ateEgressTrustBundleMountPath = "/run/ate/egress-trust-bundle"

	// ateEgressTrustBundleFilePath is the relative path within the projected trust-bundle volume.
	ateEgressTrustBundleFilePath = "trust-bundle.pem"

	// ateEgressMITMTrustBundleName is ATE's predefined egress gateway MITM CA bundle name.
	ateEgressMITMTrustBundleName = "egress-mitm.ate.dev"

	// ateSystemRootsTrustBundleName is ATE's predefined public CA root bundle name.
	ateSystemRootsTrustBundleName = "system-roots.ate.dev"

	// ateTemplateDigestSuffixLen is the number of lowercase hex characters from
	// the image sha256 digest appended to the ActorTemplate base name.
	ateTemplateDigestSuffixLen = 12

	// maxATETemplateBaseLen bounds the base template name so "<base>-<12-hex>" fits in a 63-char DNS label.
	maxATETemplateBaseLen = maxATEDNSLabelLen - 1 - ateTemplateDigestSuffixLen
)

var (
	ateImageDigestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	ateDigestSuffixPattern     = regexp.MustCompile(`^[0-9a-f]{12}$`)
	ateWorkerLabelKeyPattern   = regexp.MustCompile(`^(?:(?:[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?)\.)*[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?/[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?$|^[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?$`)
	ateWorkerLabelValuePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?$`)
)

func validKitATEWorkerLabel(key, val string) bool {
	if prefix, _, hasPrefix := strings.Cut(key, "/"); hasPrefix && len(prefix) > 253 {
		return false
	}
	return len(val) <= maxATEDNSLabelLen && ateWorkerLabelKeyPattern.MatchString(key) && ateWorkerLabelValuePattern.MatchString(val)
}

// ParseKitATEWorkerSelector parses a comma-separated list of key=value worker
// selector labels (for example "workload-type=ate-assistants,tier=prod").
func ParseKitATEWorkerSelector(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("ATE worker selector is required")
	}
	labels := map[string]string{}
	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		key, val, ok := strings.Cut(pair, "=")
		if !ok || key == "" || val == "" || strings.Contains(val, "=") {
			return nil, errors.New("ATE worker selector must be comma-separated key=value pairs")
		}
		if !validKitATEWorkerLabel(key, val) {
			return nil, fmt.Errorf("invalid ATE worker selector label %q=%q", key, val)
		}
		if _, dup := labels[key]; dup {
			return nil, fmt.Errorf("duplicate ATE worker selector label %q", key)
		}
		labels[key] = val
	}
	if len(labels) == 0 {
		return nil, errors.New("ATE worker selector must contain at least one label")
	}
	return labels, nil
}

func validateKitATEWorkerSelector(labels map[string]string) error {
	if len(labels) == 0 {
		return errors.New("ATE worker selector must contain at least one label")
	}
	for key, val := range labels {
		if !validKitATEWorkerLabel(key, val) {
			return fmt.Errorf("invalid ATE worker selector label %q=%q", key, val)
		}
	}
	return nil
}

// kitATEImageDigest extracts the canonical "sha256:<64-lowercase-hex>" digest
// from an explicit digest-pinned OCI image reference, ignoring any mutable tag
// component (for example "<repo>:sha-<commit>@sha256:<digest>").
func kitATEImageDigest(ref string) (string, error) {
	if !kitExplicitImage(ref) {
		return "", errors.New("ATE image must be an explicit digest-pinned reference")
	}
	_, digest, ok := strings.Cut(ref, "@")
	if !ok || !ateImageDigestPattern.MatchString(digest) {
		return "", errors.New("ATE image must include a canonical @sha256:<64-hex> digest")
	}
	return digest, nil
}

func kitATETemplateVersionName(baseTemplate, imageDigest string) string {
	hexDigest := strings.TrimPrefix(imageDigest, "sha256:")
	return baseTemplate + "-" + hexDigest[:ateTemplateDigestSuffixLen]
}

func kitATETemplateVersionMatches(baseTemplate, actual string) bool {
	if actual == baseTemplate {
		return true
	}
	suffix, ok := strings.CutPrefix(actual, baseTemplate+"-")
	return ok && ateDigestSuffixPattern.MatchString(suffix)
}

func (k *KitATERuntimeBackend) ensureTemplate(ctx context.Context) error {
	_, err := k.config.Client.GetAtespace(ctx, &ateapiv1.GetAtespaceRequest{
		Atespace: &ateapiv1.ObjectRef{Atespace: "", Name: k.config.Atespace},
	})
	if status.Code(err) == codes.NotFound {
		return fmt.Errorf("ATE atespace %q does not exist; pre-create it before enabling the ATE backend", k.config.Atespace)
	}
	if err != nil {
		return fmt.Errorf("verify ATE atespace: %w", err)
	}

	templateName := k.templateVersionName()
	desired := k.desiredTemplate(templateName)

	existing, err := k.config.Client.GetActorTemplate(ctx, &ateapiv1.GetActorTemplateRequest{
		ActorTemplate: k.actorRef(templateName),
	})
	if status.Code(err) == codes.NotFound {
		existing, err = k.config.Client.CreateActorTemplate(ctx, &ateapiv1.CreateActorTemplateRequest{
			ActorTemplate: desired,
		})
		if status.Code(err) == codes.AlreadyExists {
			existing, err = k.config.Client.GetActorTemplate(ctx, &ateapiv1.GetActorTemplateRequest{
				ActorTemplate: k.actorRef(templateName),
			})
		}
	}
	if err != nil {
		return fmt.Errorf("ensure ATE actor template: %w", err)
	}

	if !matchesKitATETemplate(existing, desired, k.imageDigest) {
		return errors.New("ATE template does not match the configured Kit image digest, worker selector, and rootfs contract")
	}
	return nil
}

func (k *KitATERuntimeBackend) desiredTemplate(templateName string) *ateapiv1.ActorTemplate {
	return &ateapiv1.ActorTemplate{
		Metadata: &ateapiv1.ResourceMetadata{
			Atespace:   k.config.Atespace,
			Name:       templateName,
			Uid:        "",
			Version:    0,
			CreateTime: nil,
			UpdateTime: nil,
			DeleteTime: nil,
		},
		WorkerSelector: &ateapiv1.Selector{MatchLabels: maps.Clone(k.config.WorkerSelector)},
		Containers: []*ateapiv1.Container{{
			Name:    "kit",
			Image:   k.config.Image,
			Command: nil,
			Args:    nil,
			Env:     nil,
			WakeupProbe: &ateapiv1.WakeupProbe{
				HttpGet:        &ateapiv1.HttpGetAction{Port: ateInitPort, Path: "/readyz"},
				TimeoutSeconds: ateKitWakeupTimeoutSeconds,
			},
			VolumeMounts: []*ateapiv1.VolumeMount{{
				Name:      ateEgressTrustBundleVolumeName,
				MountPath: ateEgressTrustBundleMountPath,
			}},
			SecurityContext: nil,
			Resources:       nil,
		}},
		Volumes: []*ateapiv1.Volume{{
			Name:       ateEgressTrustBundleVolumeName,
			DurableDir: nil,
			External:   nil,
			SystemInfo: &ateapiv1.SystemInfoVolumeSource{
				DataSources: []*ateapiv1.SystemInfoDataSource{{
					ActorMetadata: nil,
					TrustBundle: &ateapiv1.TrustBundleDataSource{
						Names: []string{ateEgressMITMTrustBundleName, ateSystemRootsTrustBundleName},
						Path:  ateEgressTrustBundleFilePath,
					},
				}},
			},
			Image: nil,
		}},
		SnapshotConfig: &ateapiv1.SnapshotConfig{
			StorageLocation:      "",
			PreferredFidelity:    ateapiv1.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS,
			GoldenSnapshotConfig: &ateapiv1.GoldenSnapshotConfig{Mode: ateapiv1.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_DISABLED},
		},
		Resources: &ateapiv1.ResourceRequirements{
			Limits: &ateapiv1.Resources{
				CpuMillicores: ateKitCPUMillicores,
				MemoryBytes:   ateKitMemoryBytes,
			},
		},
		GoldenEgressPolicy: nil,
		Status:             nil,
	}
}

func matchesKitATETemplate(existing, desired *ateapiv1.ActorTemplate, expectedDigest string) bool {
	if existing == nil || desired == nil {
		return false
	}
	containers := existing.GetContainers()
	if len(containers) != 1 || len(desired.GetContainers()) != 1 {
		return false
	}
	existingDigest, err := kitATEImageDigest(containers[0].GetImage())
	if err != nil || existingDigest != expectedDigest {
		return false
	}
	normExisting, ok := proto.Clone(existing).(*ateapiv1.ActorTemplate)
	if !ok {
		return false
	}
	normDesired, ok := proto.Clone(desired).(*ateapiv1.ActorTemplate)
	if !ok {
		return false
	}
	normExisting.Metadata = nil
	normExisting.Status = nil
	normExisting.Containers[0].Image = normDesired.Containers[0].Image
	normDesired.Metadata = nil
	normDesired.Status = nil
	sortTrustBundleNames(normExisting)
	sortTrustBundleNames(normDesired)
	return proto.Equal(normExisting, normDesired)
}

func sortTrustBundleNames(tmpl *ateapiv1.ActorTemplate) {
	for _, v := range tmpl.GetVolumes() {
		for _, src := range v.GetSystemInfo().GetDataSources() {
			if tb := src.GetTrustBundle(); tb != nil {
				slices.Sort(tb.Names)
			}
		}
	}
}

type kitATEEgressEndpoint struct {
	host string
	port int32
}

func parseKitATEHTTPSOrigin(raw string) (kitATEEgressEndpoint, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return kitATEEgressEndpoint{}, errors.New("ATE Speakeasy egress target requires an explicit HTTPS URL")
	}
	host := strings.ToLower(u.Hostname())
	if !kitATETrustedDNSHost(host) {
		return kitATEEgressEndpoint{}, errors.New("ATE Speakeasy egress target must be a concrete DNS hostname without wildcards or IPs")
	}
	port := ateHTTPSPort
	if rawPort := u.Port(); rawPort != "" {
		parsed, err := strconv.ParseInt(rawPort, 10, 32)
		if err != nil || parsed < 1 || parsed > 65535 {
			return kitATEEgressEndpoint{}, errors.New("ATE Speakeasy egress target has an invalid port")
		}
		port = int32(parsed)
	}
	return kitATEEgressEndpoint{host: host, port: port}, nil
}

// BuildKitATEEgressPolicyRules constructs the complete per-actor EgressPolicy
// rule set in one place so credential injection can never be attached to a
// wildcard:
//   - TLS-terminating HTTPS rules with Authorization credentialUri replacement
//     ONLY for the exact Speakeasy Project Gateway and inference host:port
//     endpoints. The credential URI names Speakeasy's custom credential
//     provider and pins the exact (atespace, actorName, actorUID) tuple.
//   - Broad public web access via bare "*" tls_passthrough (:443) and plain
//     http (:80) with strictly nil Effects (never any credential injection).
func BuildKitATEEgressPolicyRules(atespace, actorName, actorUID, gatewayURL, inferenceURL string) ([]*ateapiv1.EgressRule, error) {
	credentialURI, err := atecred.BuildCredentialURI(atespace, actorName, actorUID)
	if err != nil {
		return nil, fmt.Errorf("build ATE credential URI: %w", err)
	}
	gatewayTarget, err := parseKitATEHTTPSOrigin(gatewayURL)
	if err != nil {
		return nil, fmt.Errorf("parse ATE Project Gateway egress origin: %w", err)
	}
	inferenceTarget, err := parseKitATEHTTPSOrigin(inferenceURL)
	if err != nil {
		return nil, fmt.Errorf("parse ATE inference egress origin: %w", err)
	}

	targetSet := map[kitATEEgressEndpoint]struct{}{
		gatewayTarget:   {},
		inferenceTarget: {},
	}
	targets := make([]kitATEEgressEndpoint, 0, len(targetSet))
	for target := range targetSet {
		targets = append(targets, target)
	}
	slices.SortFunc(targets, func(a, b kitATEEgressEndpoint) int {
		if cmp := strings.Compare(a.host, b.host); cmp != 0 {
			return cmp
		}
		if a.port < b.port {
			return -1
		}
		if a.port > b.port {
			return 1
		}
		return 0
	})

	rules := make([]*ateapiv1.EgressRule, 0, len(targets)+2)
	for _, target := range targets {
		rules = append(rules, &ateapiv1.EgressRule{
			Http: nil,
			Https: &ateapiv1.HttpsRule{
				Hostnames: []string{target.host},
				Ports:     &ateapiv1.Ports{All: nil, Numbers: []int32{target.port}},
				Effects: &ateapiv1.HttpRuleEffects{
					ReplaceHeaders: []*ateapiv1.CredentialHeader{{
						Header:        "Authorization",
						Prefix:        "Bearer ",
						CredentialUri: credentialURI,
						ActorJwt:      nil,
					}},
				},
			},
			TlsPassthrough: nil,
		})
	}

	// Broad public web access: passthrough HTTPS (:443) and cleartext HTTP (:80)
	// with zero header effects. Private/link-local/metadata destinations are
	// blocked by ATE's ateworker nftables (169.254.0.0/16, fe80::/10), hostname-
	// only DNS authorization, and WorkerPool NetworkPolicy.
	rules = append(rules,
		&ateapiv1.EgressRule{
			Http:  nil,
			Https: nil,
			TlsPassthrough: &ateapiv1.TlsPassthroughRule{
				Hostnames: []string{"*"},
				Ports:     &ateapiv1.Ports{All: nil, Numbers: []int32{ateHTTPSPort}},
			},
		},
		&ateapiv1.EgressRule{
			Http: &ateapiv1.HttpRule{
				Hostnames: []string{"*"},
				Ports:     &ateapiv1.Ports{All: nil, Numbers: []int32{ateHTTPPort}},
				Effects:   nil,
			},
			Https:          nil,
			TlsPassthrough: nil,
		},
	)

	if err := validateKitATEEgressRulesNoWildcardEffects(rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// validateKitATEEgressRulesNoWildcardEffects enforces that no rule matching any
// wildcard hostname can ever carry header replacement or actorJwt effects.
func validateKitATEEgressRulesNoWildcardEffects(rules []*ateapiv1.EgressRule) error {
	for _, rule := range rules {
		if h := rule.GetHttp(); h != nil {
			for _, host := range h.GetHostnames() {
				if strings.Contains(host, "*") && (h.GetEffects() != nil || len(h.GetEffects().GetReplaceHeaders()) > 0) {
					return errors.New("wildcard HTTP egress rule must never carry header effects")
				}
			}
		}
		if hs := rule.GetHttps(); hs != nil {
			for _, host := range hs.GetHostnames() {
				if strings.Contains(host, "*") && (hs.GetEffects() != nil || len(hs.GetEffects().GetReplaceHeaders()) > 0) {
					return errors.New("wildcard HTTPS egress rule must never carry header effects or actorJwt")
				}
			}
		}
	}
	return nil
}

func kitATEBootstrapProjectGatewayURL(mcpJSON []byte) (string, error) {
	var parsed struct {
		Servers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpJSON, &parsed); err != nil {
		return "", errors.New("invalid ATE Kit MCP configuration")
	}
	gw, ok := parsed.Servers[kitProjectGatewayServerName]
	if !ok || len(parsed.Servers) != 1 || gw.URL == "" {
		return "", fmt.Errorf("ATE Kit MCP config requires only %q", kitProjectGatewayServerName)
	}
	return gw.URL, nil
}

func (k *KitATERuntimeBackend) ensureEgress(ctx context.Context, actor, actorUID string, b KitLocalBootstrap) error {
	gatewayURL, err := kitATEBootstrapProjectGatewayURL(b.MCPJSON)
	if err != nil {
		return err
	}
	desiredRules, err := BuildKitATEEgressPolicyRules(k.config.Atespace, actor, actorUID, gatewayURL, b.InferenceEndpoint)
	if err != nil {
		return err
	}

	policy, err := k.config.Client.GetActorEgressPolicy(ctx, &ateapiv1.GetActorEgressPolicyRequest{
		Actor: k.actorRef(actor),
		Name:  "default",
	})
	if status.Code(err) == codes.NotFound {
		policy, err = k.config.Client.CreateActorEgressPolicy(ctx, &ateapiv1.CreateActorEgressPolicyRequest{
			Actor: k.actorRef(actor),
			EgressPolicy: &ateapiv1.EgressPolicy{
				Metadata: &ateapiv1.ResourceMetadata{
					Atespace:   k.config.Atespace,
					Name:       "default",
					Uid:        "",
					Version:    0,
					CreateTime: nil,
					UpdateTime: nil,
					DeleteTime: nil,
				},
				Rules: desiredRules,
			},
		})
	}
	if err != nil {
		return fmt.Errorf("ensure ATE actor egress policy: %w", err)
	}

	if !matchesKitATEEgressRules(policy.GetRules(), desiredRules) {
		return errors.New("ATE actor egress policy differs from configured Speakeasy and web rules")
	}
	return nil
}

func matchesKitATEEgressRules(actual, desired []*ateapiv1.EgressRule) bool {
	if len(actual) != len(desired) {
		return false
	}
	for i := range desired {
		if !egressRuleEqual(actual[i], desired[i]) {
			return false
		}
	}
	return true
}

func egressRuleEqual(a, b *ateapiv1.EgressRule) bool {
	if (a.GetHttp() == nil) != (b.GetHttp() == nil) || (a.GetHttps() == nil) != (b.GetHttps() == nil) || (a.GetTlsPassthrough() == nil) != (b.GetTlsPassthrough() == nil) {
		return false
	}
	if ah := a.GetHttp(); ah != nil {
		bh := b.GetHttp()
		if !slices.Equal(ah.GetHostnames(), bh.GetHostnames()) || !egressPortsEqual(ah.GetPorts(), bh.GetPorts()) || !egressEffectsEqual(ah.GetEffects(), bh.GetEffects()) {
			return false
		}
	}
	if ahs := a.GetHttps(); ahs != nil {
		bhs := b.GetHttps()
		if !slices.Equal(ahs.GetHostnames(), bhs.GetHostnames()) || !egressPortsEqual(ahs.GetPorts(), bhs.GetPorts()) || !egressEffectsEqual(ahs.GetEffects(), bhs.GetEffects()) {
			return false
		}
	}
	if atp := a.GetTlsPassthrough(); atp != nil {
		btp := b.GetTlsPassthrough()
		if !slices.Equal(atp.GetHostnames(), btp.GetHostnames()) || !egressPortsEqual(atp.GetPorts(), btp.GetPorts()) {
			return false
		}
	}
	return true
}

func egressPortsEqual(a, b *ateapiv1.Ports) bool {
	return (a.GetAll() == nil) == (b.GetAll() == nil) && slices.Equal(a.GetNumbers(), b.GetNumbers())
}

func egressEffectsEqual(a, b *ateapiv1.HttpRuleEffects) bool {
	ar := a.GetReplaceHeaders()
	br := b.GetReplaceHeaders()
	if len(ar) != len(br) {
		return false
	}
	for i := range ar {
		if ar[i].GetHeader() != br[i].GetHeader() ||
			ar[i].GetPrefix() != br[i].GetPrefix() ||
			ar[i].GetCredentialUri() != br[i].GetCredentialUri() ||
			(ar[i].GetActorJwt() == nil) != (br[i].GetActorJwt() == nil) {
			return false
		}
		if aj := ar[i].GetActorJwt(); aj != nil {
			bj := br[i].GetActorJwt()
			if aj.GetExpirationSeconds() != bj.GetExpirationSeconds() || !slices.Equal(aj.GetAudiences(), bj.GetAudiences()) {
				return false
			}
		}
	}
	return true
}
