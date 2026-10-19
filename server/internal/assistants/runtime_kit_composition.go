package assistants

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// KitMCPProvisionRequest carries authoritative runtime/root identity to the
// composition callback. It does not carry an initiating user's credential.
type KitMCPProvisionRequest struct {
	RuntimeID, ProjectID, AssistantID, TriggerID, UserSessionIssuerID uuid.UUID
	OrganizationID                                                    string
}
type KitMCPProvisionResult struct{ ClientID, Secret string }
type KitMCPProvisioner func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error)

// KitLocalCompositionConfig is an explicit local-only opt-in. This first
// composition admits one configured, attached, issuer-gated MCP resource.
// Built-in platform endpoints are intentionally unsupported: no human token or
// inference bearer is ever substituted for their missing machine-auth contract.
type KitLocalCompositionConfig struct {
	Environment, Image, ExtraCACertFile   string
	Origin                                *url.URL
	AllowInsecureInference                bool
	MCPResource, MCPIssuer                *url.URL
	MCPUserSessionIssuerID, RootTriggerID uuid.UUID
	ProvisionMCP                          KitMCPProvisioner
	TurnStream                            *chat.TurnStream
}

func validateKitLocalComposition(c KitLocalCompositionConfig) error {
	if c.Environment != "local" || c.Image == "" || c.ProvisionMCP == nil || c.TurnStream == nil || c.MCPUserSessionIssuerID == uuid.Nil {
		return errors.New("Kit local composition requires explicit image, resource issuer, provisioning callback and turn stream")
	}
	if c.Origin == nil || c.Origin.Host == "" || c.Origin.User != nil || c.Origin.Path != "" || c.Origin.RawQuery != "" || c.Origin.Fragment != "" || (c.Origin.Scheme != "https" && !(c.Origin.Scheme == "http" && c.AllowInsecureInference)) {
		return errors.New("Kit inference requires an origin and explicit local HTTP opt-in when insecure")
	}
	for _, u := range []*url.URL{c.MCPResource, c.MCPIssuer} {
		if u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("Kit machine MCP resource and issuer must be explicit HTTPS URLs")
		}
	}
	if !strings.HasPrefix(c.MCPResource.Path, "/mcp/") || strings.Contains(strings.TrimPrefix(c.MCPResource.Path, "/mcp/"), "/") || strings.TrimPrefix(c.MCPResource.Path, "/mcp/") == "" {
		return errors.New("Kit composition supports an issuer-gated attached /mcp/<slug> resource only")
	}
	return nil
}

// InstallKitLocalRuntime composes startup provisioning and ACP transport before
// serving work. Legacy v1/v2 dispatch remains on the original backend.
func (s *ServiceCore) InstallKitLocalRuntime(c KitLocalCompositionConfig) error {
	if err := validateKitLocalComposition(c); err != nil {
		return err
	}
	if s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil {
		return errors.New("Kit requires transcript, inference and encrypted gateway credential services")
	}
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
	factory := func(ctx context.Context, r assistantRuntimeRecord, endpoint *url.URL) (runtimeProtocol, error) {
		token, err := s.loadKitGatewayTokenAlreadyLocked(ctx, r)
		if err != nil {
			return nil, err
		}
		protocol, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: s, Endpoint: endpoint, HTTPClient: &http.Client{}, Projector: projector, CWD: KitWorkspacePath, GatewayToken: token})
		if err != nil {
			return nil, err
		}
		return &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: protocol}, nil
	}
	env := map[string]string{"SPEAKEASY_ORIGIN": c.Origin.String()}
	if c.AllowInsecureInference {
		env["SPEAKEASY_ALLOW_INSECURE_HTTP"] = "1"
	}
	command, args := KitGatewayCommand(8081)
	backend, err := NewKitLocalRuntimeBackend(KitLocalRuntimeConfig{
		Environment: c.Environment, Image: c.Image, Command: command, Args: args, Env: env, GuestPort: 8081, ServerURL: c.Origin,
		ExtraCACertFile: c.ExtraCACertFile, CompatibilityGeneration: "kit-c3cbc0e-v1", HomeOwnedByRuntimeUser: true,
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
			return err
		},
	})
	if err != nil {
		return err
	}
	return s.installKitRuntime(backend)
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
	if !kitResourceAttached(assistant, c.MCPResource) {
		return zero, errors.New("configured Kit MCP resource is not an enabled attachment of this assistant")
	}
	trigger := c.RootTriggerID
	if trigger == uuid.Nil {
		roots, err := usersessionsrepo.New(s.db).ListAssistantWorkloadRuntimeRoots(ctx, usersessionsrepo.ListAssistantWorkloadRuntimeRootsParams{ProjectID: r.ProjectID, OrganizationID: assistant.OrganizationID, AssistantID: r.AssistantID})
		if err != nil {
			return zero, err
		}
		if len(roots) != 1 {
			return zero, errors.New("Kit startup requires an explicit root trigger when multiple roots exist")
		}
		trigger = roots[0]
	}
	resolved, err := s.identities.Resolve(ctx, s.db, assistant.OrganizationID, r.ProjectID, r.AssistantID, trigger)
	if err != nil {
		return zero, err
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
	bootstrap, _, err := PrepareKitStartupBootstrap(ctx, s.assistantTokens, KitStartupBootstrapInput{Identity: *resolved.Identity, RuntimeID: r.ID, Lifetime: assistanttokens.MaxInferenceLifetime, Origin: c.Origin.String(), AgentsMD: instructions, MCPJSON: mcpJSON, MCPFiles: files, GatewayToken: []byte(gateway)})
	return bootstrap, err
}

func kitResourceAttached(a assistantRecord, resource *url.URL) bool {
	if resource == nil {
		return false
	}
	slug := strings.TrimPrefix(resource.Path, "/mcp/")
	for _, attached := range a.MCPServers {
		if attached.EndpointSlug == slug && attached.Visibility != "disabled" {
			return true
		}
	}
	return false
}
func kitCompositionMCP(c KitLocalCompositionConfig, client KitMCPProvisionResult) ([]byte, map[string][]byte, error) {
	if client.ClientID == "" || client.Secret == "" {
		return nil, nil, errors.New("Kit MCP provisioning returned no confidential client")
	}
	const filename = "mcp-workload-secret"
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"workload": map[string]any{
		"type": "streamable-http", "url": c.MCPResource.String(), "auth": map[string]any{
			"type": "client_credentials", "client_id": client.ClientID, "resource": c.MCPResource.String(), "expected_issuer": c.MCPIssuer.String(), "scopes": []string{"mcp:connect"},
			"method": map[string]string{"type": "client_secret", "client_secret_file": "/home/kit/.kit/credentials/" + filename},
		},
	}}})
	return data, map[string][]byte{filename: []byte(client.Secret)}, err
}

// These helpers deliberately do not acquire an advisory lock on a second
// connection: Bootstrap/Ready execute inside the lifecycle caller's lock. The
// SQL still fences the exact live runtime; ciphertext has the same envelope as
// the public session helpers. A stopped process always gets a fresh gateway key.
func (s *ServiceCore) rotateKitGatewayTokenAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	plain, err := json.Marshal(kitGatewayTokenEnvelope{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, Token: token})
	if err != nil {
		return "", err
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
	ciphertext, err := assistantrepo.New(s.db).LoadKitGatewayCredential(ctx, assistantrepo.LoadKitGatewayCredentialParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKitGatewayTokenNotFound
	}
	if err != nil {
		return "", err
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
