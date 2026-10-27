//nolint:exhaustruct // Optional MCP manifest and management payload fields use their zero values.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

const (
	// emaClientNameMaxLength matches the management API's client_name bound,
	// which counts characters.
	emaClientNameMaxLength = 255

	// emaIssuerCandidateLimit bounds the issuers listed when the caller names
	// none; organizations hold a handful.
	emaIssuerCandidateLimit = 50

	// emaIdempotencyKeyMaxLength matches the receipt executor's key bound.
	emaIdempotencyKeyMaxLength = 128

	operationRegisterEMAClient = "register_ema_client"
)

// EMAClientRegistrationManagement preserves the management API's org:admin
// check, organization-tier scoping, secret hashing, and atomic audit.
type EMAClientRegistrationManagement interface {
	ListIssuers(context.Context, *orggen.ListIssuersPayload) (*orggen.ListOrganizationUserSessionIssuersResult, error)
	GetIssuer(context.Context, *orggen.GetIssuerPayload) (*types.UserSessionIssuer, error)
	CreateJWTBearerClient(context.Context, *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error)
	PrepareJWTBearerClient(context.Context, *orggen.CreateJWTBearerClientPayload) (*usersessions.PreparedJWTBearerClient, error)
	RegisterJWTBearerClientTx(context.Context, pgx.Tx, *usersessions.PreparedJWTBearerClient) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error)
}

type RegisterEMAClientInput struct {
	IssuerID                string `json:"issuer_id,omitempty" jsonschema:"exact UUID of an organization-level sign-in issuer; omit to list the candidates instead of registering"`
	ClientName              string `json:"client_name" jsonschema:"display name for the client, such as the MCP client application being configured"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method" jsonschema:"none for a public client without a secret; client_secret_basic or client_secret_post for a confidential client whose secret is generated and returned once"`
	Confirmed               bool   `json:"confirmed" jsonschema:"false previews the exact issuer without registering; true registers, only after the user confirms the issuer, client name, and authentication method"`
	IdempotencyKey          string `json:"idempotency_key,omitempty" jsonschema:"optional stable key of at most 128 characters; retrying with the same key and input returns the first registration instead of creating another"`
}

type RegisterEMAClientOutput struct {
	Preview                           bool   `json:"preview" jsonschema:"true when nothing was registered because confirmed was false"`
	Replayed                          bool   `json:"replayed,omitempty" jsonschema:"true when the idempotency key matched an earlier registration; its secret is not shown again"`
	ClientID                          string `json:"client_id,omitempty" jsonschema:"the client ID the identity provider must put in the assertion's client_id claim and the MCP client must present"`
	ClientSecret                      string `json:"client_secret,omitempty" jsonschema:"generated client secret, returned only by the call that registered the client; it cannot be read again"`
	ClientName                        string `json:"client_name"`
	TokenEndpointAuthMethod           string `json:"token_endpoint_auth_method"`
	IssuerID                          string `json:"issuer_id"`
	IssuerSlug                        string `json:"issuer_slug"`
	AuthorizationServerMode           string `json:"authorization_server_mode" jsonschema:"shared when the issuer serves one authorization server, endpoint when each MCP server announces its own"`
	IssuerURL                         string `json:"issuer_url,omitempty" jsonschema:"assertion audience of a shared authorization server; absent when each MCP server announces its own"`
	TokenEndpoint                     string `json:"token_endpoint,omitempty" jsonschema:"token endpoint of a shared authorization server; absent when each MCP server announces its own"`
	TrustedIdentityProviderConfigured bool   `json:"trusted_identity_provider_configured" jsonschema:"whether the issuer trusts a live identity provider with signing keys; while false every assertion is rejected"`
	NextStep                          string `json:"next_step"`
}

type emaIssuerCandidate struct {
	IssuerID                          string `json:"issuer_id"`
	Slug                              string `json:"slug"`
	TrustedIdentityProviderConfigured bool   `json:"trusted_identity_provider_configured"`
}

type emaClientRefusal struct {
	Code      string               `json:"code"`
	Message   string               `json:"message"`
	Issuers   []emaIssuerCandidate `json:"issuers,omitempty"`
	Truncated bool                 `json:"truncated,omitempty"`
}

// emaIssuerRequiredError carries the candidates for a call that named no issuer.
type emaIssuerRequiredError struct {
	candidates []emaIssuerCandidate
	truncated  bool
}

func (e *emaIssuerRequiredError) Error() string { return "ema client issuer required" }

var (
	errEMAIdempotencyKeyReused    = errors.New("ema client idempotency key reused")
	errEMAIdempotencyNoReceiptKey = errors.New("ema client idempotency has no default project")
)

// emaTrustChecker reports the token path's trusted-issuer predicate.
type emaTrustChecker func(ctx context.Context, organizationID string, issuerID uuid.UUID) (bool, error)

type EMAClientRegistrationService struct {
	management     EMAClientRegistrationManagement
	trusted        emaTrustChecker
	db             *pgxpool.Pool
	now            func() time.Time
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)

	// encodeReceipt overrides the secret-free receipt encoding in tests.
	encodeReceipt func(RegisterEMAClientOutput) ([]byte, error)
}

func (r *PostgresReader) WithEMAClientRegistration(management EMAClientRegistrationManagement) *PostgresReader {
	if management == nil || r.db == nil {
		return r
	}
	db := r.db
	r.emaClientRegistration = &EMAClientRegistrationService{
		management: management,
		trusted: func(ctx context.Context, organizationID string, issuerID uuid.UUID) (bool, error) {
			trusted, err := usersessions.TrustedIdentityProviderConfigured(ctx, db, organizationID, issuerID)
			if err != nil {
				return false, fmt.Errorf("read ema issuer trust: %w", err)
			}
			return trusted, nil
		},
		db:             db,
		now:            time.Now,
		resolveProject: r.resolveInventoryProject,
	}
	return r
}

func (s *EMAClientRegistrationService) register(ctx context.Context, principal Principal, input RegisterEMAClientInput) (RegisterEMAClientOutput, error) {
	if s == nil || s.management == nil || s.trusted == nil {
		return RegisterEMAClientOutput{}, ErrUnavailable
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || principal.UserID == "" || principal.OrganizationID == "" || ac.UserID != principal.UserID || ac.ActiveOrganizationID != principal.OrganizationID {
		return RegisterEMAClientOutput{}, oops.C(oops.CodeUnauthorized)
	}

	if strings.TrimSpace(input.IssuerID) == "" {
		return RegisterEMAClientOutput{}, s.issuerCandidates(ctx, principal)
	}

	issuerID, err := uuid.Parse(strings.TrimSpace(input.IssuerID))
	name := strings.TrimSpace(input.ClientName)
	key := strings.TrimSpace(input.IdempotencyKey)
	if err != nil || issuerID == uuid.Nil || name == "" || utf8.RuneCountInString(name) > emaClientNameMaxLength ||
		!slices.Contains(usersessions.JWTBearerClientAuthMethods, input.TokenEndpointAuthMethod) || len(key) > emaIdempotencyKeyMaxLength {
		return RegisterEMAClientOutput{}, oops.C(oops.CodeBadRequest)
	}

	// Fresh read of the exact target; the create re-checks it under lock.
	issuer, err := s.management.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: issuerID.String()})
	if err != nil {
		return RegisterEMAClientOutput{}, fmt.Errorf("read ema client issuer: %w", err)
	}
	if issuer == nil || issuer.ProjectID != "" {
		return RegisterEMAClientOutput{}, oops.C(oops.CodeBadRequest)
	}
	trusted, err := s.trusted(ctx, principal.OrganizationID, issuerID)
	if err != nil {
		return RegisterEMAClientOutput{}, err
	}

	preview := emaIssuerOutput(issuer, trusted)
	preview.ClientName = name
	preview.TokenEndpointAuthMethod = input.TokenEndpointAuthMethod
	if !input.Confirmed {
		preview.Preview = true
		preview.NextStep = "Nothing was registered. Show the user this issuer, client name, and authentication method"
		if !trusted {
			preview.NextStep += ", and that the issuer does not trust an identity provider yet so sign-in will fail until one is configured"
		}
		preview.NextStep += ". After they confirm, call again with confirmed: true."
		return preview, nil
	}

	payload := &orggen.CreateJWTBearerClientPayload{UserSessionIssuerID: issuerID.String(), ClientName: name, TokenEndpointAuthMethod: input.TokenEndpointAuthMethod}
	if key == "" {
		created, err := s.create(ctx, payload)
		if err != nil {
			return RegisterEMAClientOutput{}, err
		}
		return created, nil
	}
	return s.createWithReceipt(ctx, principal, key, payload)
}

func (s *EMAClientRegistrationService) issuerCandidates(ctx context.Context, principal Principal) error {
	tier := "organization"
	limit := emaIssuerCandidateLimit
	listed, err := s.management.ListIssuers(ctx, &orggen.ListIssuersPayload{Tier: &tier, Limit: &limit})
	if err != nil {
		return fmt.Errorf("list ema client issuers: %w", err)
	}
	candidates := make([]emaIssuerCandidate, 0, len(listed.Items))
	for _, issuer := range listed.Items {
		if issuer == nil || issuer.ProjectID != "" {
			continue
		}
		id, err := uuid.Parse(issuer.ID)
		if err != nil {
			continue
		}
		trusted, err := s.trusted(ctx, principal.OrganizationID, id)
		if err != nil {
			return err
		}
		candidates = append(candidates, emaIssuerCandidate{IssuerID: issuer.ID, Slug: issuer.Slug, TrustedIdentityProviderConfigured: trusted})
	}
	truncated := listed.NextCursor != nil && *listed.NextCursor != ""
	return &emaIssuerRequiredError{candidates: candidates, truncated: truncated}
}

func (s *EMAClientRegistrationService) create(ctx context.Context, payload *orggen.CreateJWTBearerClientPayload) (RegisterEMAClientOutput, error) {
	created, err := s.management.CreateJWTBearerClient(ctx, payload)
	if err != nil {
		return RegisterEMAClientOutput{}, fmt.Errorf("register ema client: %w", err)
	}
	return emaCreatedOutput(created, payload.TokenEndpointAuthMethod)
}

func emaCreatedOutput(created *orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, authMethod string) (RegisterEMAClientOutput, error) {
	if created == nil || created.Client == nil || created.Issuer == nil {
		return RegisterEMAClientOutput{}, ErrUnavailable
	}
	out := emaIssuerOutput(created.Issuer, created.TrustedIdentityProviderConfigured)
	out.ClientID = created.Client.ClientID
	out.ClientName = created.Client.ClientName
	out.TokenEndpointAuthMethod = authMethod
	if created.ClientSecret != nil {
		out.ClientSecret = *created.ClientSecret
	}
	out.NextStep = emaClientNextStep(out)
	return out, nil
}

func encodeEMAClientReceipt(out RegisterEMAClientOutput) ([]byte, error) {
	out.ClientSecret = ""
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode ema client receipt: %w", err)
	}
	return encoded, nil
}

// createWithReceipt registers the client, its audit row, and the receipt in
// one transaction, so a retry with the same key can never register a second
// client. The receipt holds the registration's projection, never its secret,
// which is returned only after commit. Receipts are keyed by project, so the
// organization's default project carries this organization-level write.
func (s *EMAClientRegistrationService) createWithReceipt(ctx context.Context, principal Principal, key string, payload *orggen.CreateJWTBearerClientPayload) (RegisterEMAClientOutput, error) {
	if s.db == nil || s.now == nil || s.resolveProject == nil {
		return RegisterEMAClientOutput{}, ErrUnavailable
	}
	project, err := s.resolveProject(ctx, principal.OrganizationID, FindMCPInput{})
	if errors.Is(err, ErrForbidden) {
		return RegisterEMAClientOutput{}, errEMAIdempotencyNoReceiptKey
	}
	if err != nil {
		return RegisterEMAClientOutput{}, err
	}
	normalized, err := json.Marshal(struct {
		IssuerID   string `json:"issuer_id"`
		ClientName string `json:"client_name"`
		AuthMethod string `json:"token_endpoint_auth_method"`
	}{IssuerID: payload.UserSessionIssuerID, ClientName: payload.ClientName, AuthMethod: payload.TokenEndpointAuthMethod})
	if err != nil {
		return RegisterEMAClientOutput{}, fmt.Errorf("normalize ema client request: %w", err)
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-ema-client-v1\x00"), normalized...))

	encode := s.encodeReceipt
	if encode == nil {
		encode = encodeEMAClientReceipt
	}

	// A replay is answered before the bcrypt mint, which runs outside the receipt transaction.
	var prepared *usersessions.PreparedJWTBearerClient
	prepare := func(ctx context.Context) error {
		p, err := s.management.PrepareJWTBearerClient(ctx, payload)
		if err != nil {
			return fmt.Errorf("prepare ema client: %w", err)
		}
		prepared = p
		return nil
	}

	var secret string
	receipt, err := executeChargedMutationReceipt(ctx, prepare, mutationReceiptExecution[RegisterEMAClientOutput]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationRegisterEMAClient,
		IdempotencyKey: key, InputHash: hex.EncodeToString(digest[:]), Label: "ema client",
		Invalid:     func(error) error { return oops.C(oops.CodeUnauthorized) },
		Conflict:    func(string) error { return errEMAIdempotencyKeyReused },
		Unavailable: func(err error) error { return fmt.Errorf("%w: %w", ErrUnavailable, err) },
		ValidateReplay: func(payload []byte) bool {
			var stored RegisterEMAClientOutput
			return json.Unmarshal(payload, &stored) == nil && stored.ClientID != "" && stored.ClientSecret == ""
		},
		EncodeResult: encode,
		Mutate: func(ctx context.Context, tx pgx.Tx) (RegisterEMAClientOutput, error) {
			created, err := s.management.RegisterJWTBearerClientTx(ctx, tx, prepared)
			if err != nil {
				return RegisterEMAClientOutput{}, fmt.Errorf("register ema client: %w", err)
			}
			out, err := emaCreatedOutput(created, payload.TokenEndpointAuthMethod)
			secret = out.ClientSecret
			return out, err
		},
	})
	if err != nil {
		return RegisterEMAClientOutput{}, err
	}
	var out RegisterEMAClientOutput
	if err := json.Unmarshal(receipt.ResultPayload, &out); err != nil {
		return RegisterEMAClientOutput{}, fmt.Errorf("%w: decode ema client receipt: %w", ErrUnavailable, err)
	}
	if receipt.Replayed {
		out.Replayed = true
		out.NextStep = "This idempotency key already registered this client; nothing new was registered. Its secret was shown only by the first call and cannot be read again."
		return out, nil
	}
	out.ClientSecret = secret
	return out, nil
}

func emaIssuerOutput(issuer *types.UserSessionIssuer, trusted bool) RegisterEMAClientOutput {
	out := RegisterEMAClientOutput{
		IssuerID:                          issuer.ID,
		IssuerSlug:                        issuer.Slug,
		AuthorizationServerMode:           issuer.AuthorizationServerMode,
		TrustedIdentityProviderConfigured: trusted,
	}
	if issuer.IssuerURL != nil {
		out.IssuerURL = *issuer.IssuerURL
	}
	if issuer.TokenEndpoint != nil {
		out.TokenEndpoint = *issuer.TokenEndpoint
	}
	return out
}

func emaClientNextStep(out RegisterEMAClientOutput) string {
	step := "Configure the MCP client with this client ID"
	if out.ClientSecret != "" {
		step += " and client secret; the secret is shown only now, so the user must store it before continuing"
	}
	step += ". The identity provider must issue assertions for this client ID."
	if !out.TrustedIdentityProviderConfigured {
		step += " This issuer does not trust an identity provider yet, so every assertion is rejected until an organization administrator configures one."
	}
	return step
}

func registerEMAClientRegistrationTool(reg *Registrar, service *EMAClientRegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "register_ema_client",
		Title:       "Register an Enterprise-Managed Authorization Client",
		Description: "Register an MCP client, such as one that needs a client ID configured before it can use enterprise-managed authorization, on one organization-level sign-in issuer. The client redeems identity assertions from the organization's identity provider and has no redirect URIs. Call without issuer_id to list the organization's issuers. Call with confirmed: false to preview the exact issuer, show the user the issuer, client name, and authentication method, ask them to confirm, then call again with confirmed: true and a stable idempotency_key. A confidential method returns a generated client secret exactly once; give it to the user for their MCP client configuration and never repeat it. Without an idempotency_key each call registers a new client. Registering does not by itself make sign-in work: the issuer must also trust the identity provider issuing the assertions. Requires org:admin.",
		Annotations: writeAnnotations(false, false, false),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterEMAClientInput) (*mcp.CallToolResult, RegisterEMAClientOutput, error) {
		return principalToolCall(ctx, emaClientToolResult, func(p Principal) (RegisterEMAClientOutput, error) { return service.register(ctx, p, input) })
	})
}

func emaClientToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	refusal := emaClientRefusal{Code: unavailableCode, Message: "Registering an enterprise-managed authorization client is temporarily unavailable. With an idempotency_key nothing was registered, so retrying with the same key is safe; without one, check the issuer's clients before retrying."}
	if required, ok := errors.AsType[*emaIssuerRequiredError](err); ok {
		refusal = emaClientRefusal{Code: "issuer_required", Message: "Nothing was registered. Ask the user which organization-level issuer the client belongs to, then call again with its exact issuer_id.", Issuers: required.candidates, Truncated: required.truncated}
		if required.truncated {
			refusal.Message += fmt.Sprintf(" Only the first %d issuers are listed; if the one the user wants is missing, ask them for its exact ID.", emaIssuerCandidateLimit)
		}
	}
	switch {
	case errors.Is(err, errEMAIdempotencyKeyReused):
		refusal.Code, refusal.Message = "idempotency_key_reused", "Nothing was registered. This idempotency key was already used for a different registration or one that did not finish. To retry the original request, repeat it exactly; for a new registration, use a new key."
	case errors.Is(err, errEMAIdempotencyNoReceiptKey):
		refusal.Code, refusal.Message = "idempotency_unavailable", "Nothing was registered. Safe retries need the organization's default project, which does not exist. Ask the user before registering without an idempotency_key."
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			refusal.Code, refusal.Message = "invalid_request", "Nothing was registered. Provide an exact organization-level issuer UUID, a client name of at most 255 characters, one of none, client_secret_basic, or client_secret_post, and an idempotency_key of at most 128 characters. Project-level issuers are not supported."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			refusal.Code, refusal.Message = "permission_denied", "Registering a client requires a signed-in organization administrator (org:admin)."
		case oops.CodeNotFound:
			refusal.Code, refusal.Message = "not_found", "That issuer is not available to you. Call again without issuer_id to list the organization's issuers."
		default:
			// Unknown failures keep the bounded unavailable response.
		}
	}
	payload, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}, true
}
