// Consent interstitial for the OAuth proxy.
//
// The upstream callback parks the finished authorization in the cache and
// renders this page instead of handing the authorization code straight back
// to the MCP client. Consent is asked for on every authorization — no record
// of prior approvals is kept — so an upstream session that resumes silently
// (SSO, a live provider cookie) can never complete a dynamically registered
// client's authorization without a human in the loop.

package oauth

import (
	_ "embed"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

//go:embed consent_template.html
var consentPageTmplData string

// consentPageData is the field set consent_template.html renders against.
// It carries display copy plus the opaque ConsentID; every security-relevant
// value stays in the PendingConsent record behind that ID.
type consentPageData struct {
	ConsentID    string
	ClientName   string
	MCPSlug      string
	MCPURL       string
	RedirectURI  string
	ProviderSlug string
	Scopes       []string
}

// handleConsent finishes an OAuth proxy authorization once the user has acted
// on the consent screen. Mounted at `POST /oauth/{mcpSlug}/consent`.
//
// The pending record is consumed on the first POST regardless of the decision,
// so a consent screen cannot be replayed to mint a second redirect carrying
// the same authorization code.
func (s *Service) handleConsent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}

	r.Body = http.MaxBytesReader(w, r.Body, requestMaxBodyBytes)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "malformed consent submission").LogError(ctx, s.logger)
	}

	consentID := r.PostForm.Get("consent_id")
	if consentID == "" {
		return oops.E(oops.CodeBadRequest, nil, "consent_id is required").LogError(ctx, s.logger)
	}

	// Unknown, expired and already-consumed IDs are indistinguishable here,
	// and all equally unfinishable — the user has to restart the flow.
	pending, err := s.pendingConsentStorage.Get(ctx, PendingConsentCacheKey(consentID))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "this authorization request is no longer valid, please try again").LogError(ctx, s.logger)
	}

	// The parked record is authoritative for the slug; a mismatch means the
	// form was replayed against a different MCP server's consent route.
	if pending.MCPSlug != mcpSlug {
		return oops.E(oops.CodeBadRequest, nil, "consent does not match this mcp server").LogError(ctx, s.logger)
	}

	// Consume before acting so a resubmitted form cannot mint a second
	// redirect for the same code.
	if err := s.pendingConsentStorage.DeleteByKey(ctx, PendingConsentCacheKey(consentID)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "consume pending consent").LogError(ctx, s.logger)
	}

	approved := r.PostForm.Get("action") == "approve"

	logger := s.logger.With(
		attr.SlogOAuthClientID(pending.ClientID),
		attr.SlogToolsetMCPSlug(pending.MCPSlug),
	)

	redirectURL := pending.ApproveURL
	if !approved {
		redirectURL = pending.DenyURL

		// The grant was already minted during the upstream callback, so a
		// denial has to actively drop it rather than merely withhold the URL.
		if err := s.grantManager.RevokeGrant(ctx, pending.ToolsetID, pending.Code); err != nil {
			logger.ErrorContext(ctx, "failed to revoke grant after consent denial", attr.SlogError(err))
		}
	}

	logger.InfoContext(ctx, "oauth proxy consent decision recorded",
		attr.SlogOAuthStatus(conv.Ternary(approved, "approved", "denied")))

	if pending.UseResultPage {
		data := gramOAuthResultPageData{
			RedirectURL:      template.URL(redirectURL), // #nosec G203 // Built server-side from an already-validated redirect_uri
			ScriptHash:       s.oauthStatusPageScriptHash,
			ErrorCode:        "",
			ErrorDescription: "",
		}

		resultTmpl := s.successPageTmpl
		if !approved {
			data.ErrorCode = "access_denied"
			data.ErrorDescription = "You declined to authorize this application."
			resultTmpl = s.failurePageTmpl
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := resultTmpl.Execute(w, data); err != nil {
			return oops.E(oops.CodeUnexpected, err, "render oauth result page").LogError(ctx, logger)
		}

		return nil
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
	return nil
}
