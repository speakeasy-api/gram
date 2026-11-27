package assistants

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/distribution/reference"
	"github.com/google/uuid"
)

// KitWorkloadAssertionPath is an external projection, never part of persistent HOME.
const KitWorkloadAssertionPath = "/var/run/secrets/speakeasy/workload/assertion"

// KitWorkspacePath is the approved persistent Kit project root.
const KitWorkspacePath = "/home/kit/workspace"

const (
	// kitFileConfig configures local Kit runtimes to exchange the externally
	// projected workload assertion for Speakeasy inference tokens.
	kitFileConfig = "provider = \"speakeasy\"\ncredential_store = \"file\"\ncredential_dir = \"/home/kit/.kit/credentials\"\nmcp_config = \"/home/kit/.kit/credentials/mcp.json\"\n"

	// kitEgressFileConfig configures ATE Kit runtimes for egress-injected bearer auth.
	kitEgressFileConfig = "provider = \"openrouter\"\ncredential_store = \"file\"\ncredential_dir = \"/home/kit/.kit/credentials\"\nmcp_config = \"/home/kit/.kit/credentials/mcp.json\"\n"
)

// kitExplicitImage accepts explicit non-latest tags as well as digest-pinned image references.
func kitExplicitImage(image string) bool {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return false
	}
	// A digest selects immutable content even when a mutable tag is also present.
	if _, digested := named.(reference.Digested); digested {
		return true
	}
	tag, tagged := named.(reference.Tagged)
	return tagged && !strings.EqualFold(tag.Tag(), "latest")
}

func validateKitBootstrap(b KitLocalBootstrap) error {
	if _, err := kitReasoningEffort(b.ReasoningEffort); err != nil {
		return err
	}
	if len(b.AgentsMD) == 0 || len(b.GatewayToken) == 0 {
		return fmt.Errorf("kit requires global instructions and ACP bearer")
	}
	for _, c := range b.GatewayToken {
		if c <= 32 || c > 126 {
			return fmt.Errorf("kit ACP bearer must be one printable ASCII token")
		}
	}
	if b.Model == "" || b.Workload == nil || b.Workload.IssuerID == uuid.Nil || b.Workload.Subject == "" {
		return fmt.Errorf("kit requires explicit model and workload assertion binding")
	}
	if b.EgressInjectedAuth {
		if b.AssertionFile != "" {
			return fmt.Errorf("egress-injected kit bootstrap must not specify an assertion file")
		}
		u, err := url.Parse(b.InferenceEndpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("kit requires explicit HTTPS inference endpoint")
		}
	} else {
		if !strings.HasPrefix(b.AssertionFile, "/") {
			return fmt.Errorf("kit requires explicit model and workload assertion binding")
		}
		for _, raw := range []string{b.InferenceEndpoint, b.Workload.Audience} {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("kit requires explicit HTTPS workload endpoints")
			}
		}
	}
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b.MCPJSON, &mcp) != nil || mcp.Servers == nil {
		return fmt.Errorf("kit MCP config requires mcpServers object")
	}
	if len(mcp.Servers) != 1 {
		return fmt.Errorf("kit MCP config requires exactly one %q server entry", kitProjectGatewayServerName)
	}
	for name, raw := range mcp.Servers {
		if name != kitProjectGatewayServerName {
			return fmt.Errorf("kit MCP config only permits %q server entry, got %q", kitProjectGatewayServerName, name)
		}
		var server struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers,omitempty"`
			Auth    *struct {
				Type          string            `json:"type"`
				TokenFile     string            `json:"tokenFile"`
				TokenEndpoint string            `json:"tokenEndpoint"`
				EndpointMode  string            `json:"endpointMode"`
				Grant         string            `json:"grant"`
				Resource      string            `json:"resource"`
				Scope         string            `json:"scope"`
				Extensions    map[string]string `json:"extensions,omitempty"`
			} `json:"auth,omitempty"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&server); err != nil {
			return fmt.Errorf("invalid Kit workload MCP configuration: %w", err)
		}
		if server.Type != "streamable-http" {
			return fmt.Errorf("kit MCP requires streamable-http transport")
		}
		if b.EgressInjectedAuth {
			if server.Auth != nil {
				return fmt.Errorf("egress-injected kit MCP must not configure in-sandbox workload auth")
			}
			authHeader := strings.TrimSpace(server.Headers["Authorization"])
			if authHeader != "Bearer "+kitATEEgressAuthPlaceholder {
				return fmt.Errorf("egress-injected kit MCP requires the fixed placeholder Authorization Bearer header")
			}
		} else {
			if server.Auth == nil || server.Auth.Type != "workload_identity" || server.Auth.TokenFile != b.AssertionFile || server.Auth.TokenEndpoint != b.Workload.Audience+"/token" || server.Auth.EndpointMode != "explicit" || server.Auth.Grant != "jwtBearer" || server.Auth.Resource != server.URL {
				return fmt.Errorf("kit MCP requires the bound generic workload assertion")
			}
			// The Project Gateway binds its project through its resource URL and
			// never carries OAuth token-exchange extensions.
			if len(server.Auth.Extensions) != 0 {
				return fmt.Errorf("kit project gateway resource must not carry extensions")
			}
		}
		u, err := url.Parse(server.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return fmt.Errorf("invalid Kit MCP resource")
		}
	}

	return nil
}

// kitLocalBootstrapPayload builds the versioned kit-ate-init wire v2 payload for
// a local thread container, delivering bootstrap files and non-secret env vars
// over the loopback init endpoint.
func kitLocalBootstrapPayload(b KitLocalBootstrap, extraEnv map[string]string) ([]byte, error) {
	if err := validateKitLocalBootstrap(b); err != nil {
		return nil, err
	}
	if len(b.GatewayToken) < 32 || bytes.ContainsAny(b.GatewayToken, " \r\n\x00") {
		return nil, errors.New("invalid local Kit ACP bearer token")
	}
	env := maps.Clone(extraEnv)
	if env == nil {
		env = map[string]string{}
	}
	payload := kitATEBootstrapRequest{
		Version: ateInitWireVersion,
		Dirs: []kitATEBootstrapDir{
			{Path: ".kit", Mode: 0o700},
			{Path: ".kit/credentials", Mode: 0o700},
			{Path: "workspace", Mode: 0o700},
		},
		Files: []kitATEBootstrapFile{
			{Path: ".kit/AGENTS.md", Mode: 0o600, Content: b.AgentsMD},
			{Path: ".kit/config.toml", Mode: 0o600, Content: []byte(kitBootstrapConfig(b))},
			{Path: ".kit/credentials/mcp.json", Mode: 0o600, Content: b.MCPJSON},
			{Path: ".kit/credentials/gateway-token", Mode: 0o600, Content: b.GatewayToken},
		},
		Env: env,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode local Kit bootstrap payload: %w", err)
	}
	return raw, nil
}

// kitBootstrapConfig is the public Kit v0.3.1 generic workload contract.
// When EgressInjectedAuth is true, the sandbox holds no assertion file or
// workload token-exchange block: Kit uses OpenAI-compatible bearer auth
// (OPENROUTER_BASE_URL + placeholder OPENROUTER_API_KEY) and ATE's egress
// gateway replaces the Authorization header with the actor's signed JWT.
// When EgressInjectedAuth is false (local development only), Kit exchanges
// the externally projected assertion file via RFC 7523 jwtBearer.
func kitBootstrapConfig(b KitLocalBootstrap) string {
	if b.Workload == nil {
		return ""
	}
	effort, err := kitReasoningEffort(b.ReasoningEffort)
	if err != nil {
		return ""
	}
	if b.EgressInjectedAuth {
		return kitEgressFileConfig + "model = " + strconv.Quote(b.Model) + "\nreasoning_effort = " + strconv.Quote(effort) + "\n"
	}
	base := kitFileConfig + "model = " + strconv.Quote(b.Model) + "\nreasoning_effort = " + strconv.Quote(effort) + "\n" +
		"[inference_providers.speakeasy]\nendpoint = " + strconv.Quote(b.InferenceEndpoint) + "\n"
	return base +
		"[inference_providers.speakeasy.workload]\ntokenFile = " + strconv.Quote(b.AssertionFile) + "\nendpointMode = \"explicit\"\ngrant = \"jwtBearer\"\ntokenEndpoint = " + strconv.Quote(b.Workload.Audience+"/token") + "\nresource = \"urn:speakeasy:inference\"\n" +
		"[inference_providers.speakeasy.workload.extensions]\nproject_id = " + strconv.Quote(b.Workload.Identity.ProjectID.String()) + "\n"
}

// Kit's provider and ACP contracts support only low, medium and high. In
// particular, its "default" selector is not a request to disable reasoning.
func kitReasoningEffort(effort string) (string, error) {
	switch effort {
	case "":
		return "medium", nil
	case "low", "medium", "high":
		return effort, nil
	default:
		return "", fmt.Errorf("kit does not support reasoning effort %q; select low, medium or high", effort)
	}
}
