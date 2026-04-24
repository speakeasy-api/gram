package assistanttokens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const issuer = "gram-assistants"

// revocationCacheTTL is how long a successful or failed revocation lookup is
// trusted before Authorize hits the DB again. Keeps the per-turn lookup
// burst (1 completions + N MCP calls on the same thread) cheap while
// bounding revocation latency.
const revocationCacheTTL = 5 * time.Second

type Claims struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	// UserID is the assistant owner at mint time. It outlives an ownership
	// transfer by at most the token TTL — after that the next /configure or
	// /turn mints a fresh token against the new owner.
	UserID      string `json:"user_id"`
	AssistantID string `json:"assistant_id"`
	ThreadID    string `json:"thread_id"`
	jwt.RegisteredClaims
}

type GenerateInput struct {
	OrgID       string
	ProjectID   uuid.UUID
	UserID      string
	AssistantID uuid.UUID
	ThreadID    uuid.UUID
	TTL         time.Duration
}

type Manager struct {
	jwtSecret  string
	db         *pgxpool.Pool
	orgs       *organizationsrepo.Queries
	projects   *projectsrepo.Queries
	authz      *authz.Engine
	revocation *revocationCache
}

func New(jwtSecret string, db *pgxpool.Pool, authzEngine *authz.Engine) *Manager {
	return &Manager{
		jwtSecret:  jwtSecret,
		db:         db,
		orgs:       organizationsrepo.New(db),
		projects:   projectsrepo.New(db),
		authz:      authzEngine,
		revocation: newRevocationCache(revocationCacheTTL),
	}
}

func (m *Manager) Generate(input GenerateInput) (string, error) {
	now := time.Now()
	ttl := input.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		OrgID:       input.OrgID,
		ProjectID:   input.ProjectID.String(),
		UserID:      input.UserID,
		AssistantID: input.AssistantID.String(),
		ThreadID:    input.ThreadID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   input.AssistantID.String(),
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: nil,
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "",
		},
	})

	signed, err := token.SignedString([]byte(m.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("sign assistant token: %w", err)
	}
	return signed, nil
}

func (m *Manager) Validate(tokenString string) (*Claims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if strings.HasPrefix(strings.ToLower(tokenString), "bearer ") {
		tokenString = strings.TrimSpace(tokenString[7:])
	}
	if tokenString == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{
		OrgID:       "",
		ProjectID:   "",
		UserID:      "",
		AssistantID: "",
		ThreadID:    "",
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
		return nil, oops.E(oops.CodeUnauthorized, err, "invalid assistant token")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, oops.E(oops.CodeUnauthorized, nil, "invalid assistant token")
	}
	if claims.Issuer != issuer {
		return nil, oops.E(oops.CodeUnauthorized, nil, "invalid assistant token issuer")
	}

	return claims, nil
}

func (m *Manager) Authorize(ctx context.Context, tokenString string) (context.Context, *Claims, error) {
	claims, err := m.Validate(tokenString)
	if err != nil {
		return ctx, nil, err
	}

	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid assistant token project")
	}

	threadID, err := uuid.Parse(claims.ThreadID)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid assistant token thread")
	}

	assistantID, err := uuid.Parse(claims.AssistantID)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid assistant token assistant")
	}

	project, err := m.projects.GetProjectByID(ctx, projectID)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "unable to load assistant project")
	}
	if project.OrganizationID != claims.OrgID {
		return ctx, nil, oops.E(oops.CodeUnauthorized, nil, "assistant token organization mismatch")
	}

	org, err := m.orgs.GetOrganizationMetadata(ctx, claims.OrgID)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "unable to load assistant organization")
	}

	if err := m.checkRevocation(ctx, threadID, assistantID); err != nil {
		return ctx, nil, err
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: claims.OrgID,
		// UserID carries the assistant owner captured at mint time. It may
		// outlive an ownership transfer by up to the token TTL; the next
		// runtime /configure or /turn mints a fresh token against the new
		// owner.
		UserID:                claims.UserID,
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             &project.ID,
		OrganizationSlug:      org.Slug,
		Email:                 nil,
		ProjectSlug:           &project.Slug,
		AccountType:           org.GramAccountType,
		Whitelisted:           org.Whitelisted,
		HasActiveSubscription: false,
		APIKeyScopes:          []string{auth.APIKeyScopeChat.String(), auth.APIKeyScopeConsumer.String()},
		IsAdmin:               false,
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: assistantID,
		ThreadID:    threadID,
	})

	if m.authz != nil {
		ctx, err = m.authz.PrepareContext(ctx)
		if err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "load assistant owner grants")
		}
	}

	return ctx, claims, nil
}

// checkRevocation rejects tokens for deleted threads, deleted assistants, or
// non-active assistants. Result is memoized for revocationCacheTTL so the
// per-turn burst of authorized calls (1× /chat/completions + N× MCP)
// collapses to a single DB hit.
func (m *Manager) checkRevocation(ctx context.Context, threadID, assistantID uuid.UUID) error {
	if m.db == nil {
		return nil
	}
	if allowed, ok := m.revocation.get(threadID); ok {
		if allowed {
			return nil
		}
		return oops.E(oops.CodeUnauthorized, nil, "assistant token has been revoked")
	}

	var (
		threadDeleted    bool
		assistantDeleted bool
		assistantStatus  string
	)
	err := m.db.QueryRow(ctx, `
SELECT t.deleted, a.deleted, a.status
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id
WHERE t.id = $1 AND t.assistant_id = $2
`, threadID, assistantID).Scan(&threadDeleted, &assistantDeleted, &assistantStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			m.revocation.put(threadID, false)
			return oops.E(oops.CodeUnauthorized, nil, "assistant token thread not found")
		}
		return oops.E(oops.CodeUnauthorized, err, "unable to load assistant thread")
	}

	allowed := !threadDeleted && !assistantDeleted && assistantStatus == "active"
	m.revocation.put(threadID, allowed)
	if !allowed {
		return oops.E(oops.CodeUnauthorized, nil, "assistant token has been revoked")
	}
	return nil
}

type revocationCache struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[uuid.UUID]revocationEntry
}

type revocationEntry struct {
	allowed bool
	expires time.Time
}

func newRevocationCache(ttl time.Duration) *revocationCache {
	return &revocationCache{
		ttl: ttl,
		mu:  sync.Mutex{},
		m:   map[uuid.UUID]revocationEntry{},
	}
}

func (c *revocationCache) get(id uuid.UUID) (bool, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok {
		return false, false
	}
	if now.After(e.expires) {
		delete(c.m, id)
		return false, false
	}
	return e.allowed, true
}

func (c *revocationCache) put(id uuid.UUID, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[id] = revocationEntry{allowed: allowed, expires: time.Now().Add(c.ttl)}
}
