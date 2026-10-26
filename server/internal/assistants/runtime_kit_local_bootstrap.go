package assistants

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const KitIntegrationImage = "localhost/kit-control-plane:v0.2.19"

// KitWorkloadAssertionPath is an external projection, never part of persistent HOME.
const KitWorkloadAssertionPath = "/var/run/secrets/speakeasy/workload/assertion"

// KitWorkspacePath is the approved persistent gateway project root.
const KitWorkspacePath = "/home/kit/workspace"
const kitFileConfig = "provider = \"speakeasy\"\ncredential_store = \"file\"\ncredential_dir = \"/home/kit/.kit/credentials\"\nmcp_config = \"/home/kit/.kit/credentials/mcp.json\"\n"

func KitGatewayCommand(port int) ([]string, []string) {
	return []string{"/usr/local/bin/kit"}, []string{"gateway", "--listen", "0.0.0.0:" + strconv.Itoa(port), "--allow-wildcard-bind", "--credential-file", "/home/kit/.kit/credentials/gateway-token", "--project", KitWorkspacePath}
}
func validateKitGatewayCommand(command, args []string, port int) error {
	expectedCommand, expectedArgs := KitGatewayCommand(port)
	if !slices.Equal(command, expectedCommand) || !slices.Equal(args, expectedArgs) {
		return fmt.Errorf("kit requires the authenticated wildcard gateway command and approved project root")
	}
	return nil
}

func validateKitBootstrap(b KitLocalBootstrap) error {
	if len(b.AgentsMD) == 0 || len(b.GatewayToken) == 0 {
		return fmt.Errorf("kit requires global instructions and gateway bearer")
	}
	for _, c := range b.GatewayToken {
		if c <= 32 || c > 126 {
			return fmt.Errorf("kit gateway bearer must be one printable ASCII token")
		}
	}
	if b.Model == "" || b.Workload == nil || b.Workload.IssuerID == uuid.Nil || b.Workload.Subject == "" || !strings.HasPrefix(b.AssertionFile, "/") {
		return fmt.Errorf("kit requires explicit model and workload assertion binding")
	}
	for _, raw := range []string{b.InferenceEndpoint, b.Workload.Audience} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("kit requires explicit HTTPS workload endpoints")
		}
	}
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b.MCPJSON, &mcp) != nil || mcp.Servers == nil {
		return fmt.Errorf("kit MCP config requires mcpServers object")
	}
	for _, raw := range mcp.Servers {
		var server struct {
			Type string `json:"type"`
			URL  string `json:"url"`
			Auth struct {
				Type          string `json:"type"`
				TokenFile     string `json:"tokenFile"`
				TokenEndpoint string `json:"tokenEndpoint"`
				EndpointMode  string `json:"endpointMode"`
				Grant         string `json:"grant"`
				Resource      string `json:"resource"`
				Scope         string `json:"scope"`
			} `json:"auth"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&server); err != nil {
			return fmt.Errorf("invalid Kit workload MCP configuration: %w", err)
		}
		if server.Type != "streamable-http" || server.Auth.Type != "workload_identity" || server.Auth.TokenFile != b.AssertionFile || server.Auth.TokenEndpoint != b.Workload.Audience+"/token" || server.Auth.EndpointMode != "explicit" || server.Auth.Grant != "jwtBearer" || server.Auth.Resource != server.URL {
			return fmt.Errorf("kit MCP requires the bound generic workload assertion")
		}
		u, err := url.Parse(server.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return fmt.Errorf("invalid Kit MCP resource")
		}
	}

	return nil
}

// DefaultKitLocalRuntimeConfig supplies the verified image and executable
// contract. Caller must install Bootstrap, Protocol and authenticated Ready.
func DefaultKitLocalRuntimeConfig(origin *url.URL) KitLocalRuntimeConfig {
	command, args := KitGatewayCommand(8081)
	return KitLocalRuntimeConfig{WorkloadAssertionFile: "", Fence: nil, Network: "", Environment: "local", Image: KitIntegrationImage, Command: command, Args: args, GuestPort: 8081, ServerURL: origin, CompatibilityGeneration: "kit-workload-assertion-v1", HomeOwnedByRuntimeUser: true, Env: nil, ExtraCACertFile: "", Bootstrap: nil, Protocol: nil, Ready: nil}
}

// kitBootstrapConfig is the public Kit v0.2.19 generic workload contract.
func kitBootstrapConfig(b KitLocalBootstrap) string {
	if b.Workload == nil {
		return ""
	}
	return kitFileConfig + "model = " + strconv.Quote(b.Model) + "\n" +
		"[inference_providers.speakeasy]\nendpoint = " + strconv.Quote(b.InferenceEndpoint) + "\n" +
		"[inference_providers.speakeasy.workload]\ntokenFile = " + strconv.Quote(b.AssertionFile) + "\nendpointMode = \"explicit\"\ngrant = \"jwtBearer\"\ntokenEndpoint = " + strconv.Quote(b.Workload.Audience+"/token") + "\nresource = \"urn:speakeasy:inference\"\n" +
		"[inference_providers.speakeasy.workload.extensions]\nproject_id = " + strconv.Quote(b.Workload.Identity.ProjectID.String()) + "\n"
}
