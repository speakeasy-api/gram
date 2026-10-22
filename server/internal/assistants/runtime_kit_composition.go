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
)

// KitMCPProvisionRequest carries authoritative runtime/root identity to the
// composition callback. It does not carry an initiating user's credential.
type KitMCPProvisionRequest struct {
	Resource                                                          *url.URL
	LifecycleTx                                                       pgx.Tx
	RuntimeID, ProjectID, AssistantID, TriggerID, UserSessionIssuerID uuid.UUID
	OrganizationID                                                    string
}
type KitMCPProvisionResult struct {
	Resource, Issuer             *url.URL
	ClientID, Secret, AuthMethod string
	PrivateKeyPEM                []byte
}
type KitMCPProvisioner func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error)

// KitLocalCompositionConfig configures inference startup and per-turn MCP transport.
type KitLocalCompositionConfig struct {
	// Legacy machine-provisioning options are retained for configuration compatibility.
	// MCP connections now use the ordinary per-turn server set and credentials.
	PersistedMCP                                 bool
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
	if c.Image == "" || c.TurnStream == nil {
		return errors.New("kit composition requires an explicit image and turn stream")
	}
	if c.Origin == nil || c.Origin.String() != "https://ai.speakeasy.com" || c.AllowInsecureInference {
		return errors.New("kit requires the HTTPS Speakeasy provider origin")
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
	if err := s.installKitRuntime(backend); err != nil {
		return err
	}
	// Local installation explicitly opts new assistants into this backend.
	// Existing assistants retain their persisted runtime selection.
	s.newAssistantRuntimeVersion = 3
	return nil
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
		if endpoint == nil || (r.Backend == runtimeBackendGKE && endpoint.Scheme != "https") {
			return nil, ErrKitRuntimeBinding
		}
		token, err := s.loadKitGatewayTokenAlreadyLocked(ctx, r)
		if err != nil {
			return nil, err
		}
		client := gatewayClient
		if endpoint.Scheme == "https" {
			var metadata kitGKEBackendMetadata
			if err := json.Unmarshal(r.BackendMetadataJSON, &metadata); err != nil || metadata.RuntimeID != r.ID || metadata.AssistantID != r.AssistantID || metadata.ProjectID != r.ProjectID {
				return nil, ErrKitRuntimeBinding
			}
			client, err = kitGKEGatewayClient(s.guardianPolicy, cidrs, metadata.TLSCertificate, kitGKEName(r)+".kit.internal")
			if err != nil {
				return nil, err
			}
		}
		protocol, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: s, Endpoint: endpoint, HTTPClient: client, Projector: projector, CWD: KitWorkspacePath, GatewayToken: token})
		if err != nil {
			return nil, err
		}
		return &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: protocol}, nil
	}
}

// Per-turn MCP configuration uses the same scoped credentials as the default runtime.
type kitExecutionEventKey struct{}
type kitExecutionTriggerKey struct{}

var errKitStartupUndispatched = errors.New("kit startup did not dispatch event")

type kitConfiguredRuntimeProtocol struct{ *KitRuntimeProtocol }

func (p *kitConfiguredRuntimeProtocol) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	// Nil means discovery was unavailable, not an intentionally empty set.
	// Retry authoritative discovery before changing any resident connection.
	if turn.MCPServers == nil {
		assistant, err := p.config.Core.getAssistantForDispatch(ctx, r.AssistantID)
		if err != nil {
			return fmt.Errorf("retry Kit MCP discovery: %w", err)
		}
		if assistant.ProjectID != r.ProjectID {
			return ErrKitRuntimeBinding
		}
		turn.MCPServers = p.config.Core.currentRuntimeMCPServers(ctx, assistant)
		if turn.MCPServers == nil {
			return errors.New("kit MCP discovery unavailable; existing connections retained")
		}
	}
	return p.KitRuntimeProtocol.RunTurn(ctx, r, turn)
}

func (s *ServiceCore) kitCompositionBootstrap(ctx context.Context, r assistantRuntimeRecord, c KitLocalCompositionConfig) (KitLocalBootstrap, error) {
	var zero KitLocalBootstrap
	if r.RuntimeVersion != 3 {
		return zero, ErrKitRuntimeBinding
	}
	db := s.kitLifecycleDB(r)
	q := assistantrepo.New(db)
	row, err := q.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID})
	if err != nil {
		return zero, fmt.Errorf("get Kit assistant: %w", err)
	}
	assistant := assistantRecordFromGetRow(row)
	// Inference startup follows the event selected by the ordinary execution path.
	// Warmup has no event and uses the same dashboard fallback as wake turns.
	event, _ := ctx.Value(kitExecutionEventKey{}).(assistantThreadEventRecord)
	trigger, _ := ctx.Value(kitExecutionTriggerKey{}).(uuid.UUID)
	if trigger == uuid.Nil {
		trigger, err = s.executionTrigger(ctx, assistant, event)
		if err != nil {
			return zero, err
		}
	}
	var resolved assistantidentity.Resolution
	if r.LifecycleTx != nil {
		resolved, err = s.identities.ResolveLocked(ctx, r.LifecycleTx, assistant.OrganizationID, r.ProjectID, r.AssistantID, trigger)
	} else {
		resolved, err = s.identities.Resolve(ctx, s.db, assistant.OrganizationID, r.ProjectID, r.AssistantID, trigger)
	}
	if err != nil {
		return zero, fmt.Errorf("compose local kit runtime: %w", err)
	}
	if resolved.State != assistantidentity.Active || resolved.Identity == nil {
		return zero, assistantidentity.ErrInvalidIdentity
	}
	// Connections are supplied per turn, not as a singleton machine attachment.
	// An unavailable attachment must not prevent inference or other connections.
	mcpJSON := []byte(`{"mcpServers":{}}`)
	gateway, err := s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
	if err != nil {
		return zero, err
	}
	instructions := []byte("# Speakeasy assistant\n\n" + assistant.Instructions + "\n\nUse the configured MCP tools. If a connection reports an authentication error, explain it and continue with available tools. Never request secrets or invent credentials.\n")
	bootstrap, _, err := PrepareKitStartupBootstrap(ctx, s.assistantTokens, KitStartupBootstrapInput{LifecycleTx: r.LifecycleTx, Model: assistant.Model, Environment: c.Environment, Identity: *resolved.Identity, RuntimeID: r.ID, Lifetime: assistanttokens.MaxInferenceLifetime, Origin: c.Origin.String(), AgentsMD: instructions, MCPJSON: mcpJSON, MCPFiles: nil, GatewayToken: []byte(gateway)})
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
	rows, err := assistantrepo.New(s.kitLifecycleDB(r)).StoreKitGatewayCredential(ctx, assistantrepo.StoreKitGatewayCredentialParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, TokenCiphertext: encrypted})
	if err != nil {
		return "", fmt.Errorf("store Kit gateway credential: %w", err)
	}
	if rows != 1 {
		return "", ErrKitRuntimeBinding
	}
	return token, nil
}
func (s *ServiceCore) loadKitGatewayTokenAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) (string, error) {
	queries := runtimeSwitchQueries(ctx, s.kitLifecycleDB(r))
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

// kitLifecycleDB keeps credential and attachment queries on the connection that
// owns the assistant admission fence. Local callers without a transaction keep
// their existing pool-backed behavior.
func (s *ServiceCore) kitLifecycleDB(r assistantRuntimeRecord) assistantrepo.DBTX {
	if r.LifecycleTx != nil {
		return r.LifecycleTx
	}
	return s.db
}
