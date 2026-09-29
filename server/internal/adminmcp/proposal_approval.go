package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const proposalApprovalPrefix = "adminMCPApproval:"

type proposalApprovalChallenge struct {
	ID          string    `json:"id"`
	ProposalID  uuid.UUID `json:"proposal_id"`
	Digest      string    `json:"digest"`
	SessionHash string    `json:"session_hash"`
	CSRFHash    string    `json:"csrf_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

func (c proposalApprovalChallenge) CacheKey() string   { return proposalApprovalPrefix + c.ID }
func (c proposalApprovalChallenge) TTL() time.Duration { return proposalLifetime }

// StaffProposalApproval is mounted only alongside the staff OAuth routes on
// the private admin ingress. Browser approval never accepts operation arguments.
type StaffProposalApproval struct {
	store   *proposalStore
	session *StaffOAuthAuthorization
	cache   cache.TypedCacheObject[proposalApprovalChallenge]
	writes  WriteConfig
	// A write is not approvable until its operation supplies an exact-state
	// validator. The production registry is populated alongside write tools.
	revalidate map[WriteOperation]ProposalRevalidator
}

func newStaffProposalApproval(store *proposalStore, session *StaffOAuthAuthorization, challengeCache cache.Cache, writes WriteConfig) *StaffProposalApproval {
	return &StaffProposalApproval{store: store, session: session, cache: cache.NewTypedObjectCache[proposalApprovalChallenge](nil, challengeCache, cache.SuffixNone), writes: writes, revalidate: nil}
}

var proposalApprovalPage = template.Must(template.New("admin-mcp-proposal").Parse(`<!doctype html><html><head><title>Review staff change</title></head><body><h1>Review staff change</h1><p>Operation: {{.Operation}}</p><p>Proposal expires: {{.Expiry}}</p><p>Exact proposed change:</p><pre>{{.Preview}}</pre><p>Only approve if the target and change are correct. This approval is separate from the admin:write connection consent.</p><form method="post" action="/admin-mcp/proposals/{{.ID}}"><input type="hidden" name="challenge" value="{{.Challenge}}"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><input type="hidden" name="digest" value="{{.Digest}}"><button type="submit" name="action" value="approve">Approve this change</button><button type="submit" name="action" value="reject">Reject</button></form></body></html>`))

func (s *StaffProposalApproval) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		if s == nil || s.store == nil || s.session == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.show(w, r, id)
		case http.MethodPost:
			s.decide(w, r, id)
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (s *StaffProposalApproval) browser(r *http.Request, p Proposal) (string, error) {
	staff, sessionID, err := s.session.staffSession(r)
	if err != nil || staff == nil || urn.NewUserSubject(staff.OIDCSubject).String() != p.SubjectURN || !s.writes.OperationEnabled(p.Operation) {
		return "", ErrWriteIdentity
	}
	return sessionID, nil
}

// verifyConnection compares the active connection's encrypted browser session
// and granted scope under its existing row lock in proposalStore.Approve.
func (s *StaffProposalApproval) verifyConnection() ProposalRevalidator {
	return func(ctx context.Context, tx pgx.Tx, p Proposal) error {
		return s.checkConnection(ctx, tx, p)
	}
}

func (s *StaffProposalApproval) checkConnection(ctx context.Context, db repo.DBTX, p Proposal) error {
	connection, err := repo.New(db).GetLinkedWriteConnection(ctx, repo.GetLinkedWriteConnectionParams{
		ConnectionID:  p.ConnectionID,
		OauthClientID: p.OAuthClientID,
		Generation:    p.Generation,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConnectionChanged
	}
	if err != nil {
		return fmt.Errorf("read linked staff connection: %w", err)
	}
	linked, err := s.session.cipher.Decrypt(connection.AdminSessionIDEnc)
	if err != nil || linked == "" || !slices.Contains(connection.Scopes, ScopeWrite) {
		return ErrWriteIdentity
	}
	verified, err := s.session.verifier.Authorize(ctx, linked, &security.APIKeyScheme{Name: constants.AdminAuthSecurityScheme}) //nolint:exhaustruct // Only the scheme name is used.
	if err != nil {
		return ErrWriteIdentity
	}
	staff, ok := contextvalues.GetAdminAuthContext(verified)
	if !ok || staff == nil || staff.SessionID != linked || urn.NewUserSubject(staff.OIDCSubject).String() != p.SubjectURN || staff.Email == "" {
		return ErrWriteIdentity
	}
	return nil
}

func (s *StaffProposalApproval) show(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	staff, sessionID, err := s.session.staffSession(r)
	if err != nil {
		returnTo := Path + "/proposals/" + id.String()
		http.Redirect(w, r, "/admin/auth.login?return_to="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	p, err := s.store.GetForSubject(r.Context(), id, urn.NewUserSubject(staff.OIDCSubject).String())
	if err != nil || p.Status != ProposalPendingApproval || !time.Now().Before(p.ExpiresAt) {
		http.NotFound(w, r)
		return
	}
	if _, err := s.browser(r, p); err != nil || s.revalidate[p.Operation] == nil {
		http.Error(w, "write operation unavailable", http.StatusForbidden)
		return
	}
	if err := s.checkConnection(r.Context(), s.store.db, p); err != nil {
		http.Error(w, "staff connection unavailable", http.StatusForbidden)
		return
	}
	csrf, err := staffOpaqueToken()
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	challenge := proposalApprovalChallenge{ID: uuid.NewString(), ProposalID: id, Digest: p.ProposalDigest, SessionHash: staffTokenHash(sessionID), CSRFHash: staffTokenHash(csrf), CreatedAt: time.Now()}
	if err := s.cache.Store(r.Context(), challenge); err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	var page strings.Builder
	if err := proposalApprovalPage.Execute(&page, struct{ ID, Operation, Expiry, Preview, Challenge, CSRF, Digest string }{
		ID: id.String(), Operation: string(p.Operation), Expiry: p.ExpiresAt.UTC().Format(time.RFC3339), Preview: string(p.Preview), Challenge: challenge.ID, CSRF: csrf, Digest: p.ProposalDigest,
	}); err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page.String()))
}

func (s *StaffProposalApproval) decide(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid approval", http.StatusBadRequest)
		return
	}
	challenge, err := s.cache.GetAndDelete(r.Context(), proposalApprovalPrefix+r.PostForm.Get("challenge"))
	if err != nil || challenge.ProposalID != id || time.Since(challenge.CreatedAt) >= proposalLifetime || !digestsEqual(challenge.CSRFHash, staffTokenHash(r.PostForm.Get("csrf_token"))) || !digestsEqual(challenge.Digest, r.PostForm.Get("digest")) {
		http.Error(w, "approval expired or changed", http.StatusForbidden)
		return
	}
	staff, sessionID, err := s.session.staffSession(r)
	if err != nil || staff == nil || !digestsEqual(challenge.SessionHash, staffTokenHash(sessionID)) {
		http.Error(w, "staff session changed", http.StatusForbidden)
		return
	}
	subject := urn.NewUserSubject(staff.OIDCSubject).String()
	p, err := s.store.GetForSubject(r.Context(), id, subject)
	if err != nil || !digestsEqual(p.ProposalDigest, challenge.Digest) || !s.writes.OperationEnabled(p.Operation) || s.revalidate[p.Operation] == nil {
		http.Error(w, "proposal unavailable", http.StatusForbidden)
		return
	}
	if r.PostForm.Get("action") == "reject" {
		if err = s.checkConnection(r.Context(), s.store.db, p); err == nil {
			_, err = s.store.Reject(r.Context(), id, subject)
		}
	} else if r.PostForm.Get("action") == "approve" {
		_, err = s.store.Approve(r.Context(), id, subject, challenge.Digest, time.Now(), s.verifyConnection(), s.revalidate[p.Operation])
	} else {
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "proposal unavailable or changed", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("Decision recorded. Return to your MCP client to check the proposal status."))
}
