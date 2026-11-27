package assistanttokens

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Claims describes the authenticated thread binding returned by AuthorizeRuntime.
type Claims struct {
	// OrgID is the organization owning the assistant thread.
	OrgID string

	// ProjectID is the project owning the assistant thread.
	ProjectID string

	// UserID is the turn's authorizing user, or empty for workload turns.
	UserID string

	// AssistantID is the assistant bound to the runtime credential.
	AssistantID string

	// ThreadID is the assistant thread bound to the runtime credential.
	ThreadID string
}

const mcpAuthFlowIssuer = "gram-assistants-mcp-auth-flow"

type MCPAuthFlowInput struct {
	OrgID       string
	ProjectID   uuid.UUID
	UserID      string
	AssistantID uuid.UUID
	ThreadID    uuid.UUID
	// AttemptID uniquely identifies one authorization attempt.
	AttemptID string
	// FlowID binds the token to the stable callback path.
	FlowID            string
	ServerID          string
	McpURL            string
	ClientID          string
	ClientSecret      string
	RedirectURI       string
	CodeVerifier      string
	TokenEndpoint     string
	OAuthServerIssuer string
	TTL               time.Duration
}

type MCPAuthFlowClaims struct {
	OrgID             string `json:"org_id"`
	ProjectID         string `json:"project_id"`
	UserID            string `json:"user_id"`
	AssistantID       string `json:"assistant_id"`
	ThreadID          string `json:"thread_id"`
	FlowID            string `json:"flow_id"`
	ServerID          string `json:"server_id"`
	McpURL            string `json:"mcp_url"`
	ClientID          string `json:"client_id"`
	ClientSecret      string `json:"client_secret,omitempty"`
	RedirectURI       string `json:"redirect_uri"`
	CodeVerifier      string `json:"code_verifier"`
	TokenEndpoint     string `json:"token_endpoint"`
	OAuthServerIssuer string `json:"oauth_server_issuer,omitempty"`
	jwt.RegisteredClaims
}

type Manager struct {
	jwtSecret string
	db        *pgxpool.Pool
	tokens    *tokenrepo.Queries
	authz     *authz.Engine

	// credentials mints and authenticates the principal credentials assistant
	// turns run with; runtimeBindings records which assistant thread each one
	// was minted for.
	credentials     *principalcredential.Issuer
	runtimeBindings cache.Cache
}

func New(jwtSecret string, db *pgxpool.Pool, authzEngine *authz.Engine, credentials *principalcredential.Issuer, runtimeBindings cache.Cache) *Manager {
	return &Manager{
		credentials:     credentials,
		runtimeBindings: runtimeBindings,
		db:              db,
		jwtSecret:       jwtSecret,
		tokens:          tokenrepo.New(db),
		authz:           authzEngine,
	}
}

func (m *Manager) GenerateMCPAuthFlow(input MCPAuthFlowInput) (string, error) {
	now := time.Now()
	ttl := input.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	attemptID := input.AttemptID
	if attemptID == "" {
		attemptID = input.FlowID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, MCPAuthFlowClaims{
		OrgID:             input.OrgID,
		ProjectID:         input.ProjectID.String(),
		UserID:            input.UserID,
		AssistantID:       input.AssistantID.String(),
		ThreadID:          input.ThreadID.String(),
		FlowID:            input.FlowID,
		ServerID:          input.ServerID,
		McpURL:            input.McpURL,
		ClientID:          input.ClientID,
		ClientSecret:      input.ClientSecret,
		RedirectURI:       input.RedirectURI,
		CodeVerifier:      input.CodeVerifier,
		TokenEndpoint:     input.TokenEndpoint,
		OAuthServerIssuer: input.OAuthServerIssuer,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    mcpAuthFlowIssuer,
			Subject:   input.AssistantID.String(),
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: nil,
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        attemptID,
		},
	})

	signed, err := token.SignedString([]byte(m.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("sign mcp auth flow token: %w", err)
	}
	return signed, nil
}

func (m *Manager) ValidateMCPAuthFlow(tokenString string) (*MCPAuthFlowClaims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	token, err := jwt.ParseWithClaims(tokenString, &MCPAuthFlowClaims{
		OrgID:             "",
		ProjectID:         "",
		UserID:            "",
		AssistantID:       "",
		ThreadID:          "",
		FlowID:            "",
		ServerID:          "",
		McpURL:            "",
		ClientID:          "",
		ClientSecret:      "",
		RedirectURI:       "",
		CodeVerifier:      "",
		TokenEndpoint:     "",
		OAuthServerIssuer: "",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			ExpiresAt: nil,
			NotBefore: nil,
			IssuedAt:  nil,
			ID:        "",
		},
	}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(m.jwtSecret), nil
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "invalid mcp auth flow token")
	}

	claims, ok := token.Claims.(*MCPAuthFlowClaims)
	if !ok || !token.Valid {
		return nil, oops.E(oops.CodeUnauthorized, nil, "invalid mcp auth flow token")
	}
	if claims.Issuer != mcpAuthFlowIssuer {
		return nil, oops.E(oops.CodeUnauthorized, nil, "invalid mcp auth flow token issuer")
	}
	return claims, nil
}
