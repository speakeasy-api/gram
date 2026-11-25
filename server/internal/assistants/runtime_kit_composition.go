package assistants

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

// KitLocalCompositionConfig composes runtime-owned workload authentication.
// Inference and MCP never borrow a human token or an API key.
type KitLocalCompositionConfig struct {
	// Set only by the native GKE composition, never from token claims.
	nativeIdentity                                            *KitGKEIdentityConfig
	OrgHosts                                                  *orghost.Resolver
	Environment, Image, ExtraCACertFile, Network              string
	Origin                                                    *url.URL
	AllowInsecureInference                                    bool
	MCPUserSessionIssuerID                                    uuid.UUID
	WorkloadIssuerURL, WorkloadSubject, WorkloadAssertionFile string
	WorkloadJWKSURL                                           string
	WorkloadNamespace, WorkloadIssuerBaseURL                  string
	TurnStream                                                *chat.TurnStream
}

func validateKitLocalComposition(c KitLocalCompositionConfig) error {
	if c.Environment != "local" {
		return errors.New("kit local composition requires the local environment")
	}
	if c.Network == "" || c.ExtraCACertFile == "" {
		return errors.New("local Kit requires an isolated network and trusted local TLS relay for ai.speakeasy.com")
	}
	if c.WorkloadSubject == "" || c.WorkloadJWKSURL == "" {
		return errors.New("local Kit requires operator-projected assertion file, external subject and JWKS URL")
	}
	if err := validateKitLocalAssertionFile(c.WorkloadAssertionFile); err != nil {
		return err
	}
	return validateKitComposition(c)
}

func validateKitComposition(c KitLocalCompositionConfig) error {
	for _, raw := range []string{c.WorkloadIssuerURL, c.WorkloadJWKSURL, c.WorkloadIssuerBaseURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("kit requires explicit HTTPS external workload issuer, JWKS URL and authorization host")
		}
	}
	if c.Image == "" || c.WorkloadIssuerURL == "" || c.WorkloadIssuerBaseURL == "" || c.TurnStream == nil {
		return errors.New("kit composition requires explicit image, workload issuer and turn stream")
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
		Environment: c.Environment, Image: c.Image, Command: command, Args: args, Env: env, GuestPort: 8081, ServerURL: s.serverURL,
		ExtraCACertFile: c.ExtraCACertFile, WorkloadAssertionFile: c.WorkloadAssertionFile, Network: c.Network, CompatibilityGeneration: "kit-project-gateway-v1", HomeOwnedByRuntimeUser: true,
		Bootstrap: func(ctx context.Context, r assistantRuntimeRecord) (KitLocalBootstrap, error) {
			tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
			if err != nil {
				return KitLocalBootstrap{}, err
			}
			defer release()
			r.LifecycleTx = tx
			bootstrap, err := s.kitCompositionBootstrap(ctx, r, c)
			if err != nil {
				return KitLocalBootstrap{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return KitLocalBootstrap{}, fmt.Errorf("commit local kit bootstrap: %w", err)
			}
			return bootstrap, nil
		},
		Fence: func(ctx context.Context, r assistantRuntimeRecord) error {
			tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
			if err != nil {
				return err
			}
			defer release()
			r.LifecycleTx = tx
			if err := s.fenceKitRuntimeAlreadyLocked(ctx, r); err != nil {
				return err
			}
			return tx.Commit(ctx)
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

var errKitMCPConfigurationUnverified = fmt.Errorf("%w: kit runtime MCP configuration is unverified", errKitRuntimeUnrecoverable)

// The verified filesystem config owns the machine-auth connections. Do not
// shadow it with legacy per-turn server overlays or management credentials.
type kitConfiguredRuntimeProtocol struct {
	*KitRuntimeProtocol
}

func (p *kitConfiguredRuntimeProtocol) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	var metadata struct {
		Workload *KitWorkloadBinding `json:"workload"`
	}
	if err := json.Unmarshal(r.BackendMetadataJSON, &metadata); err != nil {
		return ErrKitRuntimeBinding
	}
	if metadata.Workload == nil || !metadata.Workload.MCPProjectGateway {
		// The resident filesystem must contain the new gateway configuration.
		return errKitMCPConfigurationUnverified
	}
	turn.AuthToken = ""
	turn.MCPServers = nil
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
	gatewayConnection, issuerID, err := s.kitProjectGateway(ctx, db, assistant, c.OrgHosts)
	if err != nil {
		return zero, err
	}
	c.MCPUserSessionIssuerID = issuerID
	slugs, err := s.assistantPlatformSlugs(ctx, assistant)
	if err != nil {
		return zero, err
	}
	// Built-ins exchange the workload assertion for their own resource, never
	// reuse an inference token. Tools request a thread target when no automatic
	// thread context is attached; target selection does not change authorization.
	connections := append([]kitMCPConnection{gatewayConnection}, kitBuiltinMCPConnections(s.serverURL, r.ProjectID, slugs)...)
	if r.LifecycleTx == nil {
		return zero, errors.New("kit bootstrap requires the runtime admission lock")
	}
	identity, err := s.identities.ResolveRuntimeLocked(ctx, r.LifecycleTx, assistant.OrganizationID, r.ProjectID, r.AssistantID)
	if err != nil {
		return zero, fmt.Errorf("compose kit runtime: %w", err)
	}
	// GKE commits cleanup evidence even when startup fails. Keep trust provisioning
	// atomic so a failed bootstrap cannot leave an admission without metadata.
	tx, err := r.LifecycleTx.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin kit bootstrap savepoint: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	r.LifecycleTx = tx
	gateway, err := s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
	if err != nil {
		return zero, err
	}
	workload, err := s.provisionKitWorkload(ctx, r, c, identity, gateway)
	if err != nil {
		return zero, err
	}
	workload.MCPProjectGateway = true
	mcpJSON, err := kitCompositionMCP(connections, workload)
	if err != nil {
		return zero, err
	}

	instructions := []byte("# Speakeasy assistant\n\n" + assistant.Instructions + "\n\n## Project tools\n\nUse the project-gateway MCP connection to discover available servers and tools. List servers again when needed; access can change while the conversation is running.\n")
	bootstrap := KitLocalBootstrap{Model: assistant.Model, ReasoningEffort: assistant.ReasoningEffort, AgentsMD: instructions, MCPJSON: mcpJSON, GatewayToken: []byte(gateway), Workload: workload, AssertionFile: KitWorkloadAssertionPath, InferenceEndpoint: c.Origin.String() + "/chat/completions"}
	if err := validateKitBootstrap(bootstrap); err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit kit bootstrap savepoint: %w", err)
	}
	return bootstrap, nil
}

type kitMCPConnection struct {
	Name      string
	Resource  *url.URL
	ProjectID uuid.UUID
}

func kitBuiltinMCPConnections(base *url.URL, projectID uuid.UUID, slugs []string) []kitMCPConnection {
	connections := make([]kitMCPConnection, 0, len(slugs))
	for _, slug := range slugs {
		resource, _ := url.Parse(platformtools.PlatformToolsetURL(base, slug))
		connections = append(connections, kitMCPConnection{Name: "builtin-" + slug, Resource: resource, ProjectID: projectID})
	}
	return connections
}

func kitCompositionMCP(connections []kitMCPConnection, binding *KitWorkloadBinding) ([]byte, error) {
	if binding == nil || binding.Audience == "" || len(connections) == 0 {
		return nil, errors.New("kit MCP requires workload binding and resource")
	}
	servers := map[string]any{}
	for _, connection := range connections {
		u := connection.Resource
		if u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("kit MCP requires an HTTPS resource")
		}
		if connection.Name == "" {
			return nil, errors.New("kit MCP requires a server name")
		}
		if _, exists := servers[connection.Name]; exists {
			return nil, errors.New("duplicate Kit MCP server name")
		}
		auth := map[string]any{
			"type": "workload_identity", "tokenFile": KitWorkloadAssertionPath, "endpointMode": "explicit", "grant": "jwtBearer",
			"tokenEndpoint": binding.Audience + "/token", "resource": u.String(), "scope": "mcp:connect",
		}
		if connection.ProjectID != uuid.Nil {
			auth["extensions"] = map[string]string{"project_id": connection.ProjectID.String()}
		}
		servers[connection.Name] = map[string]any{
			"type": "streamable-http", "url": u.String(), "headers": map[string]string{}, "auth": auth,
		}
	}
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return nil, fmt.Errorf("encode kit workload MCP configuration: %w", err)
	}
	return data, nil
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

// Sort by stable IDs to avoid fencing a runtime for database row-order changes.
