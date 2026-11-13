package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k *KitATERuntimeBackend) ensureTemplate(ctx context.Context) error {
	var space json.RawMessage
	err := k.call(ctx, "GetAtespace", map[string]any{"atespace": map[string]string{"name": k.config.Atespace}}, &space)
	if status.Code(err) == codes.NotFound {
		err = k.call(ctx, "CreateAtespace", map[string]any{"atespace": map[string]any{"metadata": map[string]string{"name": k.config.Atespace}}}, nil)
		if status.Code(err) == codes.AlreadyExists {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	template := map[string]any{
		"metadata":        k.ref(k.config.Template),
		"worker_selector": map[string]any{"match_labels": map[string]string{"workload-type": "ate-poc"}},
		"containers":      []any{map[string]any{"name": "kit", "image": k.config.Image, "command": []string{"/usr/local/bin/kit-ate-init"}, "wakeup_probe": map[string]any{"http_get": map[string]any{"port": 8082, "path": "/readyz"}, "timeout_seconds": 120}}},
		"snapshot_config": map[string]any{"preferred_fidelity": "SNAPSHOT_FIDELITY_ROOTFS", "golden_snapshot_config": map[string]string{"mode": "GOLDEN_SNAPSHOT_MODE_DISABLED"}},
		"resources":       map[string]any{"limits": map[string]any{"cpu_millicores": 1000, "memory_bytes": 1073741824}},
	}
	var existing struct {
		Containers []struct {
			Name, Image string
			Command     []string
		} `json:"containers"`
		Selector struct {
			Labels map[string]string `json:"match_labels"`
		} `json:"worker_selector"`
		Snapshot struct {
			Fidelity string `json:"preferred_fidelity"`
			Golden   struct {
				Mode string `json:"mode"`
			} `json:"golden_snapshot_config"`
		} `json:"snapshot_config"`
	}
	err = k.call(ctx, "GetActorTemplate", map[string]any{"actor_template": k.ref(k.config.Template)}, &existing)
	if status.Code(err) == codes.NotFound {
		err = k.call(ctx, "CreateActorTemplate", map[string]any{"actor_template": template}, &existing)
		if status.Code(err) == codes.AlreadyExists {
			err = k.call(ctx, "GetActorTemplate", map[string]any{"actor_template": k.ref(k.config.Template)}, &existing)
		}
	}
	if err != nil {
		return err
	}
	if len(existing.Containers) != 1 || existing.Containers[0].Name != "kit" || existing.Containers[0].Image != k.config.Image || !slices.Equal(existing.Containers[0].Command, []string{"/usr/local/bin/kit-ate-init"}) || !reflect.DeepEqual(existing.Selector.Labels, map[string]string{"workload-type": "ate-poc"}) || existing.Snapshot.Fidelity != "SNAPSHOT_FIDELITY_ROOTFS" || existing.Snapshot.Golden.Mode != "GOLDEN_SNAPSHOT_MODE_DISABLED" {
		return errors.New("ATE template does not match this preview's Kit image and rootfs contract")
	}
	return nil
}

func (k *KitATERuntimeBackend) ensureEgress(ctx context.Context, actor string, b KitLocalBootstrap) error {
	hosts := map[string]bool{}
	add := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
			return errors.New("ATE Kit egress requires an HTTPS origin on port 443")
		}
		hosts[strings.ToLower(u.Hostname())] = true
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
			Auth struct {
				Endpoint string `json:"tokenEndpoint"`
			} `json:"auth"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b.MCPJSON, &mcp) != nil {
		return errors.New("invalid ATE Kit MCP configuration")
	}
	for _, server := range mcp.Servers {
		if err := add(server.URL); err != nil {
			return err
		}
		if err := add(server.Auth.Endpoint); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(hosts))
	for host := range hosts {
		names = append(names, host)
	}
	slices.Sort(names)
	rules := []any{map[string]any{"tls_passthrough": map[string]any{"hostnames": names, "ports": map[string]any{"numbers": []int{443}}}}}
	var policy struct {
		Rules []any `json:"rules"`
	}
	err := k.call(ctx, "GetActorEgressPolicy", map[string]any{"actor": k.ref(actor), "name": "default"}, &policy)
	if status.Code(err) == codes.NotFound {
		err = k.call(ctx, "CreateActorEgressPolicy", map[string]any{"actor": k.ref(actor), "egress_policy": map[string]any{"metadata": map[string]string{"atespace": k.config.Atespace, "name": "default"}, "rules": rules}}, &policy)
	}
	if err != nil {
		return err
	}
	actual, _ := json.Marshal(policy.Rules)
	expected, _ := json.Marshal(rules)
	if string(actual) != string(expected) {
		return errors.New("ATE actor egress differs from its configured broker and MCP origins")
	}
	return nil
}
