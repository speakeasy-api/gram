package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// ateKitCPUMillicores is the vCPU limit booked per ATE Kit actor (1 vCPU).
	ateKitCPUMillicores int64 = 1000

	// ateKitMemoryBytes is the memory limit booked per ATE Kit actor (1 GiB).
	ateKitMemoryBytes int64 = 1 << 30 // 1 GiB

	// ateKitWakeupTimeoutSeconds bounds how long ATE waits for kit-ate-init /readyz on wakeup.
	ateKitWakeupTimeoutSeconds int32 = 120

	// ateHTTPSPort is the sole outbound destination port permitted by actor egress policy.
	ateHTTPSPort int32 = 443
)

var ateKitWorkerLabels = map[string]string{"workload-type": "ate-poc"}

func (k *KitATERuntimeBackend) ensureTemplate(ctx context.Context) error {
	_, err := k.config.Client.GetAtespace(ctx, &ateapiv1.GetAtespaceRequest{
		Atespace: &ateapiv1.ObjectRef{Atespace: "", Name: k.config.Atespace},
	})
	if status.Code(err) == codes.NotFound {
		_, err = k.config.Client.CreateAtespace(ctx, &ateapiv1.CreateAtespaceRequest{
			Atespace: &ateapiv1.Atespace{
				Metadata: &ateapiv1.ResourceMetadata{
					Atespace:   "",
					Name:       k.config.Atespace,
					Uid:        "",
					Version:    0,
					CreateTime: nil,
					UpdateTime: nil,
					DeleteTime: nil,
				},
			},
		})
		if status.Code(err) == codes.AlreadyExists {
			err = nil
		}
	}
	if err != nil {
		return fmt.Errorf("ensure ATE atespace: %w", err)
	}

	desired := &ateapiv1.ActorTemplate{
		Metadata: &ateapiv1.ResourceMetadata{
			Atespace:   k.config.Atespace,
			Name:       k.config.Template,
			Uid:        "",
			Version:    0,
			CreateTime: nil,
			UpdateTime: nil,
			DeleteTime: nil,
		},
		WorkerSelector: &ateapiv1.Selector{MatchLabels: maps.Clone(ateKitWorkerLabels)},
		Containers: []*ateapiv1.Container{{
			Name:    "kit",
			Image:   k.config.Image,
			Command: []string{"/usr/local/bin/kit-ate-init"},
			Args:    nil,
			Env:     nil,
			WakeupProbe: &ateapiv1.WakeupProbe{
				HttpGet:        &ateapiv1.HttpGetAction{Port: ateInitPort, Path: "/readyz"},
				TimeoutSeconds: ateKitWakeupTimeoutSeconds,
			},
			VolumeMounts:    nil,
			SecurityContext: nil,
			Resources:       nil,
		}},
		Volumes: nil,
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

	existing, err := k.config.Client.GetActorTemplate(ctx, &ateapiv1.GetActorTemplateRequest{
		ActorTemplate: k.actorRef(k.config.Template),
	})
	if status.Code(err) == codes.NotFound {
		existing, err = k.config.Client.CreateActorTemplate(ctx, &ateapiv1.CreateActorTemplateRequest{
			ActorTemplate: desired,
		})
		if status.Code(err) == codes.AlreadyExists {
			existing, err = k.config.Client.GetActorTemplate(ctx, &ateapiv1.GetActorTemplateRequest{
				ActorTemplate: k.actorRef(k.config.Template),
			})
		}
	}
	if err != nil {
		return fmt.Errorf("ensure ATE actor template: %w", err)
	}

	if !matchesKitATETemplate(existing, k.config.Image) {
		return errors.New("ATE template does not match the configured Kit image and rootfs contract")
	}
	return nil
}

func matchesKitATETemplate(existing *ateapiv1.ActorTemplate, image string) bool {
	containers := existing.GetContainers()
	if len(containers) != 1 {
		return false
	}
	c := containers[0]
	probe := c.GetWakeupProbe()
	limits := existing.GetResources().GetLimits()
	snap := existing.GetSnapshotConfig()

	return c.GetName() == "kit" &&
		c.GetImage() == image &&
		slices.Equal(c.GetCommand(), []string{"/usr/local/bin/kit-ate-init"}) &&
		maps.Equal(existing.GetWorkerSelector().GetMatchLabels(), ateKitWorkerLabels) &&
		probe.GetHttpGet().GetPort() == ateInitPort &&
		probe.GetHttpGet().GetPath() == "/readyz" &&
		probe.GetTimeoutSeconds() == ateKitWakeupTimeoutSeconds &&
		limits.GetCpuMillicores() == ateKitCPUMillicores &&
		limits.GetMemoryBytes() == ateKitMemoryBytes &&
		snap.GetPreferredFidelity() == ateapiv1.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS &&
		snap.GetGoldenSnapshotConfig().GetMode() == ateapiv1.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_DISABLED
}

func (k *KitATERuntimeBackend) ensureEgress(ctx context.Context, actor string, b KitLocalBootstrap) error {
	hosts := map[string]bool{}
	audience, err := url.Parse(b.Workload.Audience)
	if err != nil || !kitATETrustedDNSHost(strings.ToLower(audience.Hostname())) {
		return errors.New("invalid ATE broker origin")
	}
	trusted := append(append([]string(nil), k.config.EgressHosts...), strings.ToLower(audience.Hostname()))
	add := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
			return errors.New("ATE Kit egress requires an HTTPS origin on port 443")
		}
		host := strings.ToLower(u.Hostname())
		if !kitATETrustedDNSHost(host) || !slices.Contains(trusted, host) {
			return errors.New("ATE egress origin is not explicitly approved")
		}
		hosts[host] = true
		return nil
	}
	for _, raw := range []string{b.InferenceEndpoint, b.Workload.Audience} {
		if err := add(raw); err != nil {
			return err
		}
	}

	var mcp struct {
		Servers map[string]struct {
			URL  string `json:"url"`
			Auth *struct {
				Endpoint string `json:"tokenEndpoint"`
			} `json:"auth,omitempty"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b.MCPJSON, &mcp) != nil {
		return errors.New("invalid ATE Kit MCP configuration")
	}
	for _, server := range mcp.Servers {
		if err := add(server.URL); err != nil {
			return err
		}
		if server.Auth != nil && server.Auth.Endpoint != "" {
			if err := add(server.Auth.Endpoint); err != nil {
				return err
			}
		}
	}

	names := make([]string, 0, len(hosts))
	for host := range hosts {
		names = append(names, host)
	}
	slices.Sort(names)

	desiredRules := []*ateapiv1.EgressRule{{
		Http:  nil,
		Https: nil,
		TlsPassthrough: &ateapiv1.TlsPassthroughRule{
			Hostnames: names,
			Ports:     &ateapiv1.Ports{All: nil, Numbers: []int32{ateHTTPSPort}},
		},
	}}

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

	if !matchesKitATEEgressRules(policy.GetRules(), names) {
		return errors.New("ATE actor egress differs from its configured broker and MCP origins")
	}
	return nil
}

func matchesKitATEEgressRules(rules []*ateapiv1.EgressRule, expectedHosts []string) bool {
	if len(rules) != 1 {
		return false
	}
	rule := rules[0]
	tp := rule.GetTlsPassthrough()
	if tp == nil || rule.GetHttp() != nil || rule.GetHttps() != nil {
		return false
	}
	return slices.Equal(tp.GetHostnames(), expectedHosts) &&
		slices.Equal(tp.GetPorts().GetNumbers(), []int32{ateHTTPSPort}) &&
		tp.GetPorts().GetAll() == nil
}
