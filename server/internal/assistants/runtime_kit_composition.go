package assistants

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

// kitProjectGatewayServerName is the single MCP server name configured in
// Kit's mcp.json. Both project MCP servers and built-in Speakeasy toolsets are
// served through this Project Gateway entry according to the agent's grants.
const kitProjectGatewayServerName = "project-gateway"

// KitLocalCompositionConfig composes runtime-owned workload authentication.
// Inference and MCP never borrow a human token or an API key.
type KitLocalCompositionConfig struct {
	// Set only by ATE composition after selecting an exact thread actor.
	ateIdentity           *kitATEWorkloadIdentity
	bootstrapGatewayToken string
	bootstrapWorkload     *KitWorkloadBinding
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
		ExtraCACertFile: c.ExtraCACertFile, WorkloadAssertionFile: c.WorkloadAssertionFile, Network: c.Network, CompatibilityGeneration: "kit-project-gateway-v2", HomeOwnedByRuntimeUser: true,
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
	// Projects without a served Project Gateway keep the legacy default.
	s.newAssistantRuntimeEligible = func(ctx context.Context, tx pgx.Tx, assistant assistantRecord) (bool, error) {
		return s.kitAssistantEligible(ctx, tx, assistant, c.OrgHosts)
	}
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

// The verified filesystem config owns the single Project Gateway connection.
// Do not shadow it with legacy per-turn server overlays or management credentials.
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
		// An older runtime lacks the single-entry Project Gateway config.
		// The turn worker revokes its credentials and replaces it cleanly.
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
	gatewayURL, issuerID, err := s.kitProjectGateway(ctx, db, assistant, c.OrgHosts)
	if err != nil {
		return zero, err
	}
	c.MCPUserSessionIssuerID = issuerID
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
	gateway := c.bootstrapGatewayToken
	if gateway == "" {
		gateway, err = s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
		if err != nil {
			return zero, err
		}
	}
	workload := c.bootstrapWorkload
	if workload != nil {
		if err := s.validateKitATEBootstrapWorkload(ctx, r, c, workload, identity, gateway); err != nil {
			return zero, err
		}
		recovered := *workload
		workload = &recovered
	} else {
		workload, err = s.provisionKitWorkload(ctx, r, c, identity, gateway)
		if err != nil {
			return zero, err
		}
	}
	if c.bootstrapWorkload != nil && !workload.MCPProjectGateway {
		return zero, errKitMCPConfigurationUnverified
	}
	workload.MCPProjectGateway = true
	var mcpJSON []byte
	if c.ateIdentity != nil {
		mcpJSON, err = kitProjectGatewayMCPJSON(gatewayURL, nil, nil)
	} else {
		mcpJSON, err = kitCompositionMCP(gatewayURL, workload)
	}
	if err != nil {
		return zero, err
	}

	baseInstructions := assistant.Instructions
	switch managed, mErr := assistantrepo.New(db).GetManagedAssistantByProject(ctx, assistant.ProjectID); {
	case mErr == nil:
		if managed.ID == assistant.ID {
			baseInstructions = RenderManagedAssistantInstructions(currentAssistantInstructions(assistant.Instructions), MCPDeliveryProjectGateway)
		}
	case errors.Is(mErr, pgx.ErrNoRows):
	default:
		return zero, fmt.Errorf("resolve Kit managed assistant: %w", mErr)
	}
	instructions := []byte(renderKitAgentsMD(baseInstructions))
	egressInjected := c.ateIdentity != nil
	assertionFile := KitWorkloadAssertionPath
	if egressInjected {
		assertionFile = ""
	}
	bootstrap := KitLocalBootstrap{
		Model:              assistant.Model,
		ReasoningEffort:    assistant.ReasoningEffort,
		AgentsMD:           instructions,
		MCPJSON:            mcpJSON,
		GatewayToken:       []byte(gateway),
		Workload:           workload,
		AssertionFile:      assertionFile,
		EgressInjectedAuth: egressInjected,
		InferenceEndpoint:  c.Origin.String() + "/chat/completions",
	}
	if err := validateKitBootstrap(bootstrap); err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit kit bootstrap savepoint: %w", err)
	}
	return bootstrap, nil
}

// renderKitAgentsMD builds Kit's AGENTS.md from the assistant's instructions
// (already rendered for MCPDeliveryProjectGateway when managed) plus concise
// Project Gateway discovery guidance. Kit does not inject MCP initialize
// instructions into the model context, so AGENTS.md names the single
// project-gateway server and its Progressive meta-tools.
func renderKitAgentsMD(baseInstructions string) string {
	return "# Speakeasy assistant\n\n" + baseInstructions + "\n\n## Project tools\n\nAll project MCP servers and built-in Speakeasy toolsets (such as `assistants`, and `platform` or Slack toolsets when granted) are exposed through the single `" + kitProjectGatewayServerName + "` MCP server. Use the `" + kitProjectGatewayServerName + "` MCP tools (`list_servers`, `describe_server`, `describe_tools`, `execute_read_only_tool`, `execute_tool`, `connection_status`) to discover and invoke them. Run `list_servers` again when needed; access can change while the conversation is running.\n"
}

// kitProjectGatewayMCPJSON builds Kit's backend-agnostic mcp.json containing
// the single Project Gateway entry. Backends inject their own transport headers
// (for example a proxy-swapped bearer header) or auth block (for example GKE
// workload_identity) without duplicating the MCP configuration shape.
func kitProjectGatewayMCPJSON(gatewayURL *url.URL, headers map[string]string, auth map[string]any) ([]byte, error) {
	if gatewayURL == nil || gatewayURL.Scheme != "https" || gatewayURL.Host == "" || gatewayURL.User != nil || gatewayURL.RawQuery != "" || gatewayURL.Fragment != "" {
		return nil, errors.New("kit MCP requires an explicit HTTPS Project Gateway URL")
	}
	hdrs := map[string]string{}
	if headers != nil {
		maps.Copy(hdrs, headers)
	}
	entry := map[string]any{
		"type":    "streamable-http",
		"url":     gatewayURL.String(),
		"headers": hdrs,
	}
	if auth != nil {
		entry["auth"] = auth
	}
	data, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			kitProjectGatewayServerName: entry,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode kit Project Gateway MCP configuration: %w", err)
	}
	return data, nil
}

// kitWorkloadIdentityMCPAuth constructs the GKE/local workload_identity
// (jwtBearer) auth block for the Project Gateway entry.
func kitWorkloadIdentityMCPAuth(binding *KitWorkloadBinding, resource *url.URL) (map[string]any, error) {
	if binding == nil || binding.Audience == "" || resource == nil {
		return nil, errors.New("kit MCP requires workload binding and resource")
	}
	return map[string]any{
		"type":          "workload_identity",
		"tokenFile":     KitWorkloadAssertionPath,
		"endpointMode":  "explicit",
		"grant":         "jwtBearer",
		"tokenEndpoint": binding.Audience + "/token",
		"resource":      resource.String(),
		"scope":         "mcp:connect",
	}, nil
}

func kitCompositionMCP(gatewayURL *url.URL, binding *KitWorkloadBinding) ([]byte, error) {
	auth, err := kitWorkloadIdentityMCPAuth(binding, gatewayURL)
	if err != nil {
		return nil, err
	}
	return kitProjectGatewayMCPJSON(gatewayURL, nil, auth)
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
