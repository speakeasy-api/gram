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
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

// kitProjectGatewayServerName is the single MCP server name configured in
// Kit's mcp.json. Both project MCP servers and built-in Speakeasy toolsets are
// served through this Project Gateway entry according to the agent's grants.
const kitProjectGatewayServerName = "project-gateway"

// kitATEEgressAuthPlaceholder is a fixed non-secret bearer placeholder sent by
// Kit on ATE actors so ATE's egress gateway sees an Authorization header and
// replaces it with the minted actor JWT for Speakeasy endpoints.
const kitATEEgressAuthPlaceholder = "ate-egress-injected-placeholder"

// KitLocalCompositionConfig composes runtime-owned workload authentication.
// Inference and MCP never borrow a human token or an API key.
type KitLocalCompositionConfig struct {
	// Set only by ATE composition after selecting an exact thread actor.
	ateIdentity           *kitATEWorkloadIdentity
	bootstrapGatewayToken string
	bootstrapWorkload     *KitWorkloadBinding

	OrgHosts                                                  *orghost.Resolver
	Environment, Image, ExtraCACertFile, Network              string
	Origin                                                    *url.URL
	AllowInsecureInference                                    bool
	MCPUserSessionIssuerID                                    uuid.UUID
	WorkloadIssuerURL, WorkloadSubject, WorkloadAssertionFile string
	WorkloadJWKSURL                                           string
	WorkloadIssuerBaseURL                                     string
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

// InstallKitLocalRuntime composes the local-only per-thread Docker Kit backend
// speaking native Kit ACP (/acp) via kit-ate-init + kit serve --remote-acp.
// Outbound auth uses the local-only operator-projected workload assertion and
// RFC 7523 token exchange; deployed Kit runtimes use ATE egress injection.
func (s *ServiceCore) InstallKitLocalRuntime(c KitLocalCompositionConfig) error {
	if err := validateKitLocalComposition(c); err != nil {
		return err
	}
	if s.guardianPolicy == nil || s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil {
		return errors.New("kit requires guarded HTTP, transcript, inference and encrypted ACP credential services")
	}
	env := map[string]string{"SPEAKEASY_ORIGIN": c.Origin.String()}
	loopbackClient := s.guardianPolicy.PooledClient(guardian.WithAllowedCIDRBlocks(localRuntimeLoopbackCIDRs...))
	backend, err := NewKitLocalRuntimeBackend(KitLocalRuntimeConfig{
		Environment:             c.Environment,
		Image:                   c.Image,
		Env:                     env,
		InitPort:                ateInitPort,
		GuestPort:               ateACPPort,
		ServerURL:               s.serverURL,
		ExtraCACertFile:         c.ExtraCACertFile,
		WorkloadAssertionFile:   c.WorkloadAssertionFile,
		Network:                 c.Network,
		CompatibilityGeneration: "kit-local-acp-v3",
		HomeOwnedByRuntimeUser:  true,
		HelperClient:            loopbackClient,
		ProvisionWorkload: func(ctx context.Context, r assistantRuntimeRecord) (*KitWorkloadBinding, error) {
			tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
			if err != nil {
				return nil, err
			}
			defer release()
			r.LifecycleTx = tx
			root, err := s.ensureKitRootTokenAlreadyLocked(ctx, r)
			if err != nil {
				return nil, err
			}
			local := c
			local.bootstrapGatewayToken = root
			bootstrap, err := s.kitCompositionBootstrap(ctx, r, local)
			if err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit local kit workload provisioning: %w", err)
			}
			return bootstrap.Workload, nil
		},
		Bootstrap: func(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID, workload *KitWorkloadBinding) (KitLocalBootstrap, error) {
			tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
			if err != nil {
				return KitLocalBootstrap{}, err
			}
			defer release()
			r.LifecycleTx = tx
			token, err := s.kitATEToken(ctx, r, threadID)
			if err != nil {
				return KitLocalBootstrap{}, err
			}
			local := c
			local.bootstrapGatewayToken = token
			local.bootstrapWorkload = workload
			bootstrap, err := s.kitCompositionBootstrap(ctx, r, local)
			if err != nil {
				return KitLocalBootstrap{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return KitLocalBootstrap{}, fmt.Errorf("commit local kit thread bootstrap: %w", err)
			}
			return bootstrap, nil
		},
		Token: func(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (string, error) {
			if r.LifecycleTx != nil {
				return s.kitATEToken(ctx, r, threadID)
			}
			tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
			if err != nil {
				return "", err
			}
			defer release()
			r.LifecycleTx = tx
			token, err := s.kitATEToken(ctx, r, threadID)
			if err != nil {
				return "", err
			}
			if err := tx.Commit(ctx); err != nil {
				return "", fmt.Errorf("commit local kit token read: %w", err)
			}
			return token, nil
		},
		Fence: func(ctx context.Context, r assistantRuntimeRecord) error {
			if r.LifecycleTx != nil {
				return s.revokeKitWorkload(ctx, r.LifecycleTx, r)
			}
			tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
			if err != nil {
				return err
			}
			defer release()
			if err := s.revokeKitWorkload(ctx, tx, r); err != nil {
				return err
			}
			return tx.Commit(ctx)
		},
		Protocol:           s.kitLocalProtocolFactory(c, loopbackClient),
		Now:                nil,
		HelperPollInterval: 0,
	})
	if err != nil {
		return err
	}
	if err := s.installKitRuntime(backend); err != nil {
		return err
	}
	// Projects without a served Project Gateway use the Default runtime.
	s.newAssistantRuntimeEligible = func(ctx context.Context, tx pgx.Tx, assistant assistantRecord) (bool, error) {
		return s.kitAssistantEligible(ctx, tx, assistant, c.OrgHosts)
	}
	return nil
}

func (s *ServiceCore) kitLocalProtocolFactory(c KitLocalCompositionConfig, client *guardian.HTTPClient) func(context.Context, assistantRuntimeRecord, uuid.UUID, *url.URL, string) (runtimeProtocol, error) {
	projector := NewKitTranscriptProjector(s.db, s.chatWriter, c.TurnStream, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		bound, err := s.ValidateKitRuntimeSessionInTx(ctx, tx, kitRuntimeSessionKey{
			ProjectID:   b.ProjectID,
			AssistantID: b.AssistantID,
			RuntimeID:   b.RuntimeID,
			ThreadID:    b.ThreadID,
		}, b.SessionID)
		if err != nil {
			return err
		}
		if bound.ChatID != b.ChatID {
			return ErrKitRuntimeBinding
		}
		return nil
	})
	return func(_ context.Context, _ assistantRuntimeRecord, threadID uuid.UUID, endpoint *url.URL, token string) (runtimeProtocol, error) {
		if threadID == uuid.Nil || endpoint == nil || endpoint.Path != "/acp" || client == nil || token == "" {
			return nil, ErrKitRuntimeBinding
		}
		p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
			Core:         s,
			Endpoint:     endpoint,
			HTTPClient:   client,
			Projector:    projector,
			CWD:          KitWorkspacePath,
			GatewayToken: token,
		})
		if err != nil {
			return nil, err
		}
		return &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: p}, nil
	}
}

func (s *ServiceCore) ensureKitRootTokenAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) (string, error) {
	root, err := s.loadKitGatewayTokenAlreadyLocked(ctx, r)
	if !errors.Is(err, ErrKitGatewayTokenNotFound) {
		return root, err
	}
	if r.LifecycleTx == nil {
		return s.ensureKitATEGatewayRootToken(ctx, r)
	}
	if s.encryptionClient == nil {
		return "", errKitGatewayCredential
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate kit root gateway secret: %w", err)
	}
	plain, err := json.Marshal(kitGatewayTokenEnvelope{
		ProjectID:   r.ProjectID,
		AssistantID: r.AssistantID,
		RuntimeID:   r.ID,
		Token:       base64.RawURLEncoding.EncodeToString(secret),
	})
	if err != nil {
		return "", fmt.Errorf("encode kit root gateway secret: %w", err)
	}
	encrypted, err := s.encryptionClient.Encrypt(plain)
	if err != nil {
		return "", errKitGatewayCredential
	}
	if _, err := assistantrepo.New(r.LifecycleTx).StoreKitGatewayCredentialIfAbsent(ctx, assistantrepo.StoreKitGatewayCredentialIfAbsentParams{
		ProjectID:       r.ProjectID,
		AssistantID:     r.AssistantID,
		RuntimeID:       r.ID,
		TokenCiphertext: encrypted,
	}); err != nil {
		return "", fmt.Errorf("store kit root gateway credential: %w", err)
	}
	return s.loadKitGatewayTokenAlreadyLocked(ctx, r)
}

var errKitMCPConfigurationUnverified = fmt.Errorf("%w: kit runtime MCP configuration is unverified", errKitRuntimeUnrecoverable)

// The verified filesystem config owns the single Project Gateway connection.
// Do not shadow it with per-turn server overlays or management credentials.
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
		// Runtimes without verified Project Gateway metadata fail closed so
		// the turn worker revokes their credentials and replaces them cleanly.
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
	tx, err := r.LifecycleTx.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin kit bootstrap savepoint: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	r.LifecycleTx = tx
	gateway := c.bootstrapGatewayToken
	if gateway == "" {
		gateway, err = s.ensureKitRootTokenAlreadyLocked(ctx, r)
		if err != nil {
			return zero, err
		}
	}
	workload := c.bootstrapWorkload
	if workload != nil {
		if c.ateIdentity != nil {
			if err := s.validateKitATEBootstrapWorkload(ctx, r, c, workload, identity, gateway); err != nil {
				return zero, err
			}
		} else {
			if c.Environment != "local" || !kitWorkloadIdentityMatches(r, workload.Identity) || workload.Identity != identity || workload.IssuerID == uuid.Nil || workload.Subject == "" {
				return zero, ErrKitRuntimeBinding
			}
		}
		recovered := *workload
		workload = &recovered
	} else if c.ateIdentity != nil {
		workload, err = s.provisionKitATEWorkload(ctx, r, c, identity, gateway)
		if err != nil {
			return zero, err
		}
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
		mcpJSON, err = kitProjectGatewayMCPJSON(
			gatewayURL,
			map[string]string{"Authorization": "Bearer " + kitATEEgressAuthPlaceholder},
			nil,
		)
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
	inferenceEndpoint := ""
	if egressInjected {
		assertionFile = ""
		if s.serverURL == nil {
			return zero, errors.New("ATE inference requires deployment server URL")
		}
		inferenceEndpoint = strings.TrimRight(s.serverURL.String(), "/") + "/chat/completions"
	} else {
		inferenceEndpoint = c.Origin.String() + "/chat/completions"
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
		InferenceEndpoint:  inferenceEndpoint,
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
// (for example a proxy-swapped bearer header) or auth block (for example local
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

// kitWorkloadIdentityMCPAuth constructs the local-only workload_identity
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
		return "", fmt.Errorf("load Kit ACP root credential: %w", err)
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
// owns the assistant admission fence.
func (s *ServiceCore) kitLifecycleDB(r assistantRuntimeRecord) assistantrepo.DBTX {
	if r.LifecycleTx != nil {
		return r.LifecycleTx
	}
	return s.db
}

// kitAssistantEligible reports whether the project's Project Gateway is served
// for machine access, retaining the legacy default otherwise. This predicate
// itself does not read tool assignments or backing-agent grants (CreateAssistant
// keeps creates with initial legacy attachments on Default before calling it).
func (s *ServiceCore) kitAssistantEligible(ctx context.Context, tx pgx.Tx, assistant assistantRecord, hosts *orghost.Resolver) (bool, error) {
	_, _, err := s.kitProjectGateway(ctx, tx, assistant, hosts)
	if errors.Is(err, errKitMCPIneligible) {
		return false, nil
	}
	return err == nil, err
}

var errKitMCPIneligible = errors.New("assistant project gateway is unavailable")

// kitProjectGateway resolves the Project Gateway URL and shared organization
// user-session issuer for a Kit assistant through the shared orggateway
// availability predicate.
func (s *ServiceCore) kitProjectGateway(ctx context.Context, db assistantrepo.DBTX, assistant assistantRecord, hosts *orghost.Resolver) (*url.URL, uuid.UUID, error) {
	availability, err := orggateway.ResolveProjectGateway(ctx, db, orggateway.ResolveProjectGatewayParams{
		OrganizationID:          assistant.OrganizationID,
		ProjectID:               assistant.ProjectID,
		ServerURL:               s.serverURL,
		OrgHosts:                hosts,
		Features:                s.featureFlags,
		RequireSharedIssuerMode: true,
	})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("resolve Kit project gateway: %w", err)
	}
	if !availability.Available {
		return nil, uuid.Nil, fmt.Errorf("%w: %s", errKitMCPIneligible, availability.Reason)
	}
	return availability.GatewayURL, availability.IssuerID, nil
}
