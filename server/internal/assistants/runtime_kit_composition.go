package assistants

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// KitMCPProvisionRequest carries authoritative runtime/root identity to the
// composition callback. It does not carry an initiating user's credential.
type KitMCPProvisionRequest struct {
	RuntimeID, ProjectID, AssistantID, TriggerID, UserSessionIssuerID uuid.UUID
	OrganizationID                                                    string
}
type KitMCPProvisionResult struct {
	ClientID, Secret, AuthMethod string
	PrivateKeyPEM                []byte
}
type KitMCPProvisioner func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error)

// KitLocalCompositionConfig is an explicit local-only opt-in. This first
// composition admits one configured, attached, issuer-gated MCP resource.
// Built-in platform endpoints are intentionally unsupported: no human token or
// inference bearer is ever substituted for their missing machine-auth contract.
type KitLocalCompositionConfig struct {
	Environment, Image, ExtraCACertFile, Network string
	Origin                                       *url.URL
	AllowInsecureInference                       bool
	MCPResource, MCPIssuer                       *url.URL
	MCPUserSessionIssuerID, RootTriggerID        uuid.UUID
	ProvisionMCP                                 KitMCPProvisioner
	TurnStream                                   *chat.TurnStream
}

func validateKitLocalComposition(c KitLocalCompositionConfig) error {
	if c.Environment != "local" {
		return errors.New("kit local composition requires the local environment")
	}
	if c.Network == "" || c.ExtraCACertFile == "" {
		return errors.New("local Kit requires an isolated network and trusted local TLS relay for ai.speakeasy.com")
	}
	return validateKitComposition(c)
}

func validateKitComposition(c KitLocalCompositionConfig) error {
	if c.Image == "" || c.ProvisionMCP == nil || c.TurnStream == nil || c.MCPUserSessionIssuerID == uuid.Nil {
		return errors.New("kit composition requires explicit image, resource issuer, provisioning callback and turn stream")
	}
	if c.Origin == nil || c.Origin.String() != "https://ai.speakeasy.com" || c.AllowInsecureInference {
		return errors.New("kit requires the HTTPS Speakeasy provider origin")
	}
	for _, u := range []*url.URL{c.MCPResource, c.MCPIssuer} {
		if u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("kit machine MCP resource and issuer must be explicit HTTPS URLs")
		}
	}
	if !strings.HasPrefix(c.MCPResource.Path, "/mcp/") || strings.Contains(strings.TrimPrefix(c.MCPResource.Path, "/mcp/"), "/") || strings.TrimPrefix(c.MCPResource.Path, "/mcp/") == "" {
		return errors.New("kit composition supports an issuer-gated attached /mcp/<slug> resource only")
	}
	return nil
}

// InstallKitLocalRuntime composes startup provisioning and ACP transport before
// serving work. Legacy v1/v2 dispatch remains on the original backend.
func (s *ServiceCore) InstallKitLocalRuntime(c KitLocalCompositionConfig) error {
	if err := validateKitLocalComposition(c); err != nil {
		return err
	}
	if s.guardianPolicy == nil || s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil {
		return errors.New("kit requires guarded HTTP, transcript, inference and encrypted gateway credential services")
	}
	factory := s.kitProtocolFactory(c, localRuntimeLoopbackCIDRs)
	env := map[string]string{"SPEAKEASY_ORIGIN": c.Origin.String()}
	if c.AllowInsecureInference {
		env["SPEAKEASY_ALLOW_INSECURE_HTTP"] = "1"
	}
	command, args := KitGatewayCommand(8081)
	backend, err := NewKitLocalRuntimeBackend(KitLocalRuntimeConfig{
		Environment: c.Environment, Image: c.Image, Command: command, Args: args, Env: env, GuestPort: 8081, ServerURL: c.Origin,
		ExtraCACertFile: c.ExtraCACertFile, Network: c.Network, CompatibilityGeneration: "kit-native-api-key-v1", HomeOwnedByRuntimeUser: true,
		Bootstrap: func(ctx context.Context, r assistantRuntimeRecord) (KitLocalBootstrap, error) {
			return s.kitCompositionBootstrap(ctx, r, c)
		},
		Protocol: factory,
		Ready: func(ctx context.Context, r assistantRuntimeRecord, endpoint *url.URL) error {
			p, err := factory(ctx, r, endpoint)
			if err != nil {
				return err
			}
			_, err = p.Status(ctx, r)
			if err != nil {
				return fmt.Errorf("check kit runtime readiness: %w", err)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	return s.installKitRuntime(backend)
}

// kitProtocolFactory shares the canonical transcript projector and persisted
// session authority across local and production transports.
func (s *ServiceCore) kitProtocolFactory(c KitLocalCompositionConfig, cidrs []string) func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
	projector := NewKitTranscriptProjector(s.db, s.chatWriter, c.TurnStream, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		bound, err := s.ValidateKitRuntimeSessionInTx(ctx, tx, kitRuntimeSessionKey{ProjectID: b.ProjectID, AssistantID: b.AssistantID, RuntimeID: b.RuntimeID, ThreadID: b.ThreadID}, b.SessionID)
		if err != nil {
			return err
		}
		if bound.ChatID != b.ChatID {
			return ErrKitRuntimeBinding
		}
		return nil
	})
	// Allow runtime network destinations only for this gateway client, not
	// the global egress policy. Do not enable retries for ACP POSTs.
	gatewayClient := s.guardianPolicy.PooledClient(guardian.WithAllowedCIDRBlocks(cidrs...))
	return func(ctx context.Context, r assistantRuntimeRecord, endpoint *url.URL) (runtimeProtocol, error) {
		token, err := s.loadKitGatewayTokenAlreadyLocked(ctx, r)
		if err != nil {
			return nil, err
		}
		protocol, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: s, Endpoint: endpoint, HTTPClient: gatewayClient, Projector: projector, CWD: KitWorkspacePath, GatewayToken: token})
		if err != nil {
			return nil, err
		}
		return &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: protocol}, nil
	}
}

// The verified filesystem config owns the machine-auth connections. Do not
// shadow it with legacy per-turn server overlays or management credentials.
type kitConfiguredRuntimeProtocol struct{ *KitRuntimeProtocol }

func (p *kitConfiguredRuntimeProtocol) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	turn.AuthToken = ""
	turn.MCPServers = nil
	return p.KitRuntimeProtocol.RunTurn(ctx, r, turn)
}

func (s *ServiceCore) kitCompositionBootstrap(ctx context.Context, r assistantRuntimeRecord, c KitLocalCompositionConfig) (KitLocalBootstrap, error) {
	var zero KitLocalBootstrap
	if r.RuntimeVersion != 3 {
		return zero, ErrKitRuntimeBinding
	}
	assistant, err := s.GetAssistant(ctx, r.ProjectID, r.AssistantID)
	if err != nil {
		return zero, err
	}
	if !kitResourceAttached(assistant, s.serverURL, c.MCPResource) {
		return zero, errors.New("configured Kit MCP resource is not an enabled attachment of this assistant")
	}
	trigger := c.RootTriggerID
	if trigger == uuid.Nil {
		roots, err := usersessionsrepo.New(s.db).ListAssistantWorkloadRuntimeRoots(ctx, usersessionsrepo.ListAssistantWorkloadRuntimeRootsParams{ProjectID: r.ProjectID, OrganizationID: assistant.OrganizationID, AssistantID: r.AssistantID})
		if err != nil {
			return zero, fmt.Errorf("compose local kit runtime: %w", err)
		}
		if len(roots) != 1 {
			return zero, errors.New("kit startup requires an explicit root trigger when multiple roots exist")
		}
		trigger = roots[0]
	}
	resolved, err := s.identities.Resolve(ctx, s.db, assistant.OrganizationID, r.ProjectID, r.AssistantID, trigger)
	if err != nil {
		return zero, fmt.Errorf("compose local kit runtime: %w", err)
	}
	if resolved.State != assistantidentity.Active || resolved.Identity == nil {
		return zero, assistantidentity.ErrInvalidIdentity
	}
	client, err := c.ProvisionMCP(ctx, KitMCPProvisionRequest{RuntimeID: r.ID, ProjectID: r.ProjectID, AssistantID: r.AssistantID, OrganizationID: assistant.OrganizationID, TriggerID: trigger, UserSessionIssuerID: c.MCPUserSessionIssuerID})
	if err != nil {
		return zero, err
	}
	mcpJSON, files, err := kitCompositionMCP(c, client)
	if err != nil {
		return zero, err
	}
	gateway, err := s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
	if err != nil {
		return zero, err
	}
	instructions := []byte("# Speakeasy assistant\n\n" + assistant.Instructions + "\n\nUse the configured independent workload MCP connection. Built-in platform endpoints and human-delegated sign-in are unavailable in this runtime. Never request or substitute a user's credentials.\n")
	bootstrap, _, err := PrepareKitStartupBootstrap(ctx, s.assistantTokens, KitStartupBootstrapInput{Model: assistant.Model, Environment: c.Environment, Identity: *resolved.Identity, RuntimeID: r.ID, Lifetime: assistanttokens.MaxInferenceLifetime, Origin: c.Origin.String(), AgentsMD: instructions, MCPJSON: mcpJSON, MCPFiles: files, GatewayToken: []byte(gateway)})
	return bootstrap, err
}

func kitResourceAttached(a assistantRecord, serverURL, resource *url.URL) bool {
	if serverURL == nil || resource == nil {
		return false
	}
	// Endpoint slugs are deployment-local, not authority for an arbitrary origin.
	// Match the same canonical URL used by resolveAssistantMCPServers before
	// provisioning any workload credentials.
	slug := strings.TrimPrefix(resource.Path, "/mcp/")
	for _, attached := range a.MCPServers {
		if attached.EndpointSlug == slug && attached.Visibility != "disabled" &&
			resource.String() == serverURL.JoinPath("mcp", attached.EndpointSlug).String() {
			return true
		}
	}
	return false
}
func kitCompositionMCP(c KitLocalCompositionConfig, client KitMCPProvisionResult) ([]byte, map[string][]byte, error) {
	if client.ClientID == "" {
		return nil, nil, errors.New("kit MCP provisioning returned no confidential client")
	}
	var filename string
	var credential []byte
	var method map[string]string
	switch client.AuthMethod {
	case "", "client_secret_basic":
		if client.Secret == "" || len(client.PrivateKeyPEM) != 0 {
			return nil, nil, errors.New("invalid Kit shared-secret credentials")
		}
		filename = "mcp-workload-secret"
		credential = []byte(client.Secret)
		method = map[string]string{"type": "client_secret", "client_secret_file": "/home/kit/.kit/credentials/" + filename}
	case "private_key_jwt":
		if len(client.PrivateKeyPEM) == 0 || client.Secret != "" {
			return nil, nil, errors.New("invalid Kit private-key credentials")
		}
		filename = "mcp-workload-private-key.pem"
		credential = client.PrivateKeyPEM
		method = map[string]string{"type": "private_key_jwt", "private_key_file": "/home/kit/.kit/credentials/" + filename, "signing_algorithm": "ES256"}
	default:
		return nil, nil, errors.New("unsupported Kit MCP authentication method")
	}
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"workload": map[string]any{
		"type": "streamable-http", "url": c.MCPResource.String(), "auth": map[string]any{
			"type": "client_credentials", "client_id": client.ClientID, "resource": c.MCPResource.String(), "expected_issuer": c.MCPIssuer.String(), "scopes": []string{"mcp:connect"},
			"method": method,
		},
	}}})
	if err != nil {
		return nil, nil, fmt.Errorf("encode kit MCP configuration: %w", err)
	}
	return data, map[string][]byte{filename: credential}, nil
}

// These helpers deliberately do not acquire an advisory lock on a second
// connection: Bootstrap/Ready execute inside the lifecycle caller's lock. The
// SQL still fences the exact live runtime; ciphertext has the same envelope as
// the public session helpers. A stopped process always gets a fresh gateway key.
func (s *ServiceCore) rotateKitGatewayTokenAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("compose local kit runtime: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	plain, err := json.Marshal(kitGatewayTokenEnvelope{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, Token: token})
	if err != nil {
		return "", fmt.Errorf("compose local kit runtime: %w", err)
	}
	encrypted, err := s.encryptionClient.Encrypt(plain)
	if err != nil {
		return "", errKitGatewayCredential
	}
	rows, err := assistantrepo.New(s.db).StoreKitGatewayCredential(ctx, assistantrepo.StoreKitGatewayCredentialParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, TokenCiphertext: encrypted})
	if err != nil {
		return "", fmt.Errorf("store Kit gateway credential: %w", err)
	}
	if rows != 1 {
		return "", ErrKitRuntimeBinding
	}
	return token, nil
}
func (s *ServiceCore) loadKitGatewayTokenAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) (string, error) {
	queries := runtimeSwitchQueries(ctx, s.db)
	if _, err := queries.GetKitRuntimeControlAuthority(ctx, assistantrepo.GetKitRuntimeControlAuthorityParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrKitRuntimeBinding
		}
		return "", fmt.Errorf("validate Kit runtime authority: %w", err)
	}
	ciphertext, err := queries.LoadKitGatewayCredential(ctx, assistantrepo.LoadKitGatewayCredentialParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKitGatewayTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("compose local kit runtime: %w", err)
	}
	plain, err := s.encryptionClient.Decrypt(ciphertext)
	if err != nil {
		return "", errKitGatewayCredential
	}
	var value kitGatewayTokenEnvelope
	if json.Unmarshal([]byte(plain), &value) != nil || value.ProjectID != r.ProjectID || value.AssistantID != r.AssistantID || value.RuntimeID != r.ID || value.Token == "" {
		return "", errKitGatewayCredential
	}
	return value.Token, nil
}
