package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
)

const KitIntegrationImage = "localhost/kit-control-plane:2c4a35e"

// KitInferenceCredentialFilename is a public provider cache key, not a credential.
const KitInferenceCredentialFilename = "9bad6d07edf01f03e226b74e3be944c9fc374754bf1903ac36e2c7ce4cd7f305.json" //nolint:gosec // Public provider cache filename, not a secret.

// KitWorkspacePath is persistent under the runtime's HOME volume. ACP cwd must
// equal this approved canonical gateway project root on new and resumed sessions.
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

// Kit's existing speakeasy/default credential record. No runtime-specific
// credential type, origin metadata, or canonical chat identity is sent to Kit.
type kitInferenceFile struct {
	APIKey         string  `json:"api_key"`
	Project        string  `json:"project"`
	Email          *string `json:"email"`
	OrganizationID *string `json:"organization_id"`
}

func kitInferenceOrigin(raw []byte) (string, error) {
	var credential kitInferenceFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil {
		return "", fmt.Errorf("invalid Kit provider credential JSON")
	}
	validKey := false
	for _, prefix := range []string{"gram_live_", "gram_test_", "gram_local_"} {
		if secret, found := strings.CutPrefix(credential.APIKey, prefix); found && len(secret) == 64 {
			validKey = true
			for _, c := range secret {
				if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
					validKey = false
					break
				}
			}
		}
	}
	if !validKey || credential.Project == "" || strings.ContainsAny(credential.Project, "\r\n") {
		return "", fmt.Errorf("invalid Kit API key or project")
	}
	return "https://ai.speakeasy.com", nil
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
	if _, err := kitInferenceOrigin(b.InferenceJSON); err != nil {
		return err
	}
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b.MCPJSON, &mcp) != nil || mcp.Servers == nil {
		return fmt.Errorf("kit MCP config requires mcpServers object")
	}
	for name, data := range b.MCPFiles {
		for _, c := range name {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
				return fmt.Errorf("invalid Kit machine credential filename")
			}
		}
		if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." || name == KitInferenceCredentialFilename || name == "speakeasy.json" || name == "mcp.json" || name == "gateway-token" || name == "AGENTS.md" || name == "config.toml" || len(name) > 253 || len(data) == 0 {
			return fmt.Errorf("invalid Kit machine credential filename or content")
		}
	}
	return nil
}

// DefaultKitLocalRuntimeConfig supplies the verified image and executable
// contract. Caller must install Bootstrap, Protocol and authenticated Ready.
func DefaultKitLocalRuntimeConfig(origin *url.URL) KitLocalRuntimeConfig {
	command, args := KitGatewayCommand(8081)
	return KitLocalRuntimeConfig{Network: "", Environment: "local", Image: KitIntegrationImage, Command: command, Args: args, GuestPort: 8081, ServerURL: origin, CompatibilityGeneration: "kit-native-api-key-v1", HomeOwnedByRuntimeUser: true, Env: nil, ExtraCACertFile: "", Bootstrap: nil, Protocol: nil, Ready: nil}
}

type KitStartupBootstrapInput struct {
	Model                           string
	Identity                        assistantidentity.Identity
	RuntimeID                       uuid.UUID
	Lifetime                        time.Duration
	Origin                          string
	Environment                     string
	AgentsMD, MCPJSON, GatewayToken []byte
	MCPFiles                        map[string][]byte
}

// PrepareKitStartupBootstrap must run under the caller's runtime admission lock
// before every fresh process start or restore. It revokes all old credentials
// for the row before issuing a replacement; no snapshot credential is reused.
// The caller must likewise refresh/revoke its MCP machine credentials separately.
func PrepareKitStartupBootstrap(ctx context.Context, manager *assistanttokens.Manager, in KitStartupBootstrapInput) (KitLocalBootstrap, uuid.UUID, error) {
	b := KitLocalBootstrap{Model: in.Model, InferenceJSON: nil, AgentsMD: in.AgentsMD, MCPJSON: in.MCPJSON, GatewayToken: in.GatewayToken, MCPFiles: in.MCPFiles}
	b.InferenceJSON, _ = json.Marshal(kitInferenceFile{Email: nil, OrganizationID: nil, APIKey: "gram_local_" + strings.Repeat("a", 64), Project: "validation"})
	if err := validateKitBootstrap(b); err != nil {
		return KitLocalBootstrap{}, uuid.Nil, err
	}
	if manager == nil || in.Lifetime <= 0 || in.Lifetime > assistanttokens.MaxInferenceLifetime || in.RuntimeID == uuid.Nil {
		return KitLocalBootstrap{}, uuid.Nil, fmt.Errorf("invalid Kit startup credential issuer, runtime or lifetime")
	}
	if err := manager.RevokeRuntimeInferenceCredentials(ctx, in.Identity.ProjectID, in.RuntimeID); err != nil {
		return KitLocalBootstrap{}, uuid.Nil, fmt.Errorf("revoke previous kit inference credentials: %w", err)
	}
	credential, err := manager.IssueKitAPIKey(ctx, assistanttokens.InferenceIssueInput{Identity: in.Identity, RuntimeID: in.RuntimeID, Lifetime: in.Lifetime}, in.Environment)
	if err != nil {
		return KitLocalBootstrap{}, uuid.Nil, fmt.Errorf("issue kit inference credential: %w", err)
	}
	b.InferenceJSON, _ = json.Marshal(kitInferenceFile{Email: nil, OrganizationID: nil, APIKey: credential.Key, Project: credential.ProjectSlug})
	return b, credential.ID, nil
}

// Pin the assistant's saved model through Kit's existing startup configuration.
func kitBootstrapConfig(b KitLocalBootstrap) string {
	if b.Model == "" {
		return kitFileConfig
	}
	return kitFileConfig + "model = " + strconv.Quote(b.Model) + "\n"
}
