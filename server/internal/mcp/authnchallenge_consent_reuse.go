package mcp

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// consentEvidenceHash versions fingerprints so an incompatible comparison
// never silently accepts evidence written by an older application.
func consentEvidenceHash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return "v1:" + sha256Hex(string(data))
}

func bindConsentBrowser(w http.ResponseWriter, state *AuthnChallengeState) error {
	secret, err := generateOpaqueToken()
	if err != nil {
		return fmt.Errorf("generate consent browser secret: %w", err)
	}
	state.ConsentBrowserHash = sha256Hex(secret)
	http.SetCookie(w, &http.Cookie{
		Name: "__Host-speakeasy_consent_" + state.FlowID, Value: secret, Path: "/",
		Quoted: false, Domain: "", Expires: time.Time{}, RawExpires: "", Partitioned: false, Raw: "", Unparsed: nil,
		MaxAge: int(state.TTL().Seconds()), HttpOnly: true,
		Secure: true, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func consentBrowserMatches(r *http.Request, state AuthnChallengeState, now time.Time) bool {
	if state.ConsentBrowserHash == "" || state.FlowID == "" || state.CreatedAt.IsZero() || state.CreatedAt.After(now) || now.Sub(state.CreatedAt) > state.TTL() {
		return false
	}
	cookie, err := r.Cookie("__Host-speakeasy_consent_" + state.FlowID)
	return err == nil && subtle.ConstantTimeCompare([]byte(sha256Hex(cookie.Value)), []byte(state.ConsentBrowserHash)) == 1
}

func consentReuseEligible(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) bool {
	return endpoint.servingSharedAuthorizationServer() != nil && !state.FirstParty && state.Federation == nil &&
		!endpoint.MetaMcpServerID.Valid && state.Subject != nil && state.Subject.Kind == urn.SessionSubjectKindUser &&
		state.Subject.ID == state.AuthorizerUserID && state.AuthorizerImpersonated != nil && !*state.AuthorizerImpersonated &&
		endpointToolSelectionResource(endpoint) != ""
}

// A complete fingerprint requires definitions from every tool. Snapshots
// written before definition capture can still serve explicit consent, but
// cannot establish reusable approval.
func consentInventoryFingerprint(inventory *consentToolInventory) string {
	if inventory == nil || !inventory.Complete || inventory.Resource == "" || inventory.BackendHash == "" {
		return ""
	}
	tools := slices.Clone(inventory.Tools)
	for i := range tools {
		if tools[i].DefinitionHash == "" {
			return ""
		}
		tools[i].Annotations = normalizeKnownAnnotations(tools[i].Annotations)
	}
	slices.SortFunc(tools, func(a, b consentInventoryTool) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return consentEvidenceHash(struct {
		Resource string
		Backend  string
		Tools    []consentInventoryTool
	}{Resource: inventory.Resource, Backend: inventory.BackendHash, Tools: tools})
}

// Missing, expired, unroutable, under-scoped, or not-yet-verified chained
// connections need the full page. A partial explicit approval is valid but
// must not silently authorize a subsequent request.
func consentRemoteFingerprint(endpoint *ResolvedMcpEndpoint, cards []remoteSessionCard) string {
	permissions := make([]string, 0, len(cards))
	for _, card := range cards {
		if !card.consentReuseReady || card.consentPermissionsHash == "" {
			return ""
		}
		permissions = append(permissions, card.consentPermissionsHash)
	}
	slices.Sort(permissions)
	return consentEvidenceHash(struct {
		Issuer      uuid.UUID
		Resource    string
		Permissions []string
	}{Issuer: endpoint.UserSessionIssuerID, Resource: endpoint.UpstreamResource, Permissions: permissions})
}

func consentResourceParams(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, clientID uuid.UUID) usersessions_repo.GetUserSessionConsentResourceParams {
	return usersessions_repo.GetUserSessionConsentResourceParams{
		ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID,
		UserSessionClientID: clientID, SubjectUrn: *state.Subject, Resource: endpointToolSelectionResource(endpoint),
	}
}

// consentApprovalTransaction holds the client lock until the authorization
// code is stored. Withdrawal uses the same lock, including across resource
// and remote-set variants. Inventory discovery happens before this lock.
type consentApprovalTransaction struct {
	tx        pgx.Tx
	selection *toolfilter.SessionSelection
	duration  int
}

// beginConsentApproval returns nil for a reuse miss. Explicit approval always
// supersedes older evidence, even if this flow cannot capture a new snapshot.
func (s *Service) beginConsentApproval(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, clientID uuid.UUID, inventory *consentToolInventory, selection *toolfilter.SessionSelection, duration int, reuse bool) (_ *consentApprovalTransaction, err error) {
	inventoryHash := consentInventoryFingerprint(inventory)
	remoteHash := ""
	if inventoryHash != "" {
		cards, cardErr := s.buildRemoteSessionCards(ctx, endpoint, state, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
		if cardErr == nil {
			remoteHash = consentRemoteFingerprint(endpoint, cards)
		}
	}
	if inventory != nil && inventory.Resource != endpointToolSelectionResource(endpoint) {
		inventoryHash = ""
	}
	if reuse && (inventoryHash == "" || remoteHash == "") {
		return nil, nil
	}
	// Resolve the default only when saving reusable evidence. Other explicit
	// grants keep zero so token exchange applies its normal duration default.
	if duration == 0 && !reuse && inventoryHash != "" && remoteHash != "" {
		issuer, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
			ID: endpoint.UserSessionIssuerID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID,
		})
		if err != nil {
			return nil, fmt.Errorf("read consent duration: %w", err)
		}
		for _, option := range buildSessionDurationOptions(issuer) {
			if option.Selected {
				duration = option.Hours
			}
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin consent approval: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
		}
	}()
	repo := usersessions_repo.New(tx)
	if _, err := repo.LockUserSessionConsentClient(ctx, usersessions_repo.LockUserSessionConsentClientParams{
		UserSessionClientID: clientID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID,
	}); err != nil {
		return nil, fmt.Errorf("lock consent client: %w", err)
	}
	if reuse {
		saved, err := repo.GetUserSessionConsentResource(ctx, consentResourceParams(endpoint, state, clientID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read resource approval: %w", err)
		}
		if saved.InventoryHash != inventoryHash || saved.RemoteSetHash != remoteHash || saved.SessionDurationHours <= 0 {
			return nil, nil
		}
		selection, err = toolfilter.ParseSessionSelection(saved.ToolSelection)
		if err != nil {
			return nil, nil
		}
		if selection != nil {
			if selection.Resource != endpointToolSelectionResource(endpoint) {
				return nil, nil
			}
			selection, err = toolfilter.NewSessionSelection(selection.Resource, uuid.New(), selection.Allow)
			if err != nil {
				return nil, fmt.Errorf("restore consent policy: %w", err)
			}
		}
		duration = int(saved.SessionDurationHours)
	} else {
		consent, err := repo.RecordUserSessionConsent(ctx, usersessions_repo.RecordUserSessionConsentParams{
			SubjectUrn: *state.Subject, RemoteSetHash: remoteSetHashEmpty, UserSessionClientID: clientID,
			ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID,
		})
		if err != nil {
			return nil, fmt.Errorf("record consent: %w", err)
		}
		if err := repo.SupersedeUserSessionConsentResource(ctx, usersessions_repo.SupersedeUserSessionConsentResourceParams{
			ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID, UserSessionClientID: clientID,
			SubjectUrn: *state.Subject, Resource: endpointToolSelectionResource(endpoint),
		}); err != nil {
			return nil, fmt.Errorf("supersede resource approval: %w", err)
		}
		if inventoryHash != "" && remoteHash != "" && duration > 0 {
			var policy []byte
			if selection != nil {
				policy, err = json.Marshal(selection)
				if err != nil {
					return nil, fmt.Errorf("encode consent policy: %w", err)
				}
			}
			hours := conv.SafeInt32(duration)
			if _, err := repo.CreateUserSessionConsentResource(ctx, usersessions_repo.CreateUserSessionConsentResourceParams{
				ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID, UserSessionConsentID: consent.ID,
				Resource: endpointToolSelectionResource(endpoint), InventoryHash: inventoryHash,
				RemoteSetHash: remoteHash, ToolSelection: policy, SessionDurationHours: hours,
			}); err != nil {
				return nil, fmt.Errorf("record resource approval: %w", err)
			}
		}
	}
	keep = true
	return &consentApprovalTransaction{tx: tx, selection: selection, duration: duration}, nil
}

func consentFullPageURL(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) string {
	return endpoint.consentPath() + "?" + url.Values{"state": {state.ID}, "consent": {"required"}}.Encode()
}

//go:embed consent_reuse_template.html
var consentReuseTemplateHTML string

var consentReuseTemplate = template.Must(
	template.Must(template.New("reuse").Parse(consentReuseTemplateHTML)).Parse(consentLogoHTML),
)

func renderConsentReuse(w http.ResponseWriter, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	if err := consentReuseTemplate.Execute(w, struct {
		Styles   template.CSS
		Fallback string
		Path     string
		ToolsURL string
		State    string
		CSRF     string
		Script   string
	}{Styles: consentPageStyles, Fallback: consentFullPageURL(endpoint, state), Path: endpoint.consentPath(), ToolsURL: endpoint.consentPath() + "/mcp", State: state.ID, CSRF: state.CSRFToken, Script: consentToolsScriptURL}); err != nil {
		return fmt.Errorf("render saved consent check: %w", err)
	}
	return nil
}

func (s *Service) consentReuseInventory(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, rawAttempt string) (*consentToolInventory, error) {
	if !s.consentToolFilteringEnabled(ctx, s.logger, endpoint.OrganizationID) {
		return nil, nil
	}
	eligible, err := s.consentToolPickerEligible(ctx, endpoint)
	if err != nil || !eligible {
		return nil, err
	}
	lockedDown, err := s.customDomainLockdownApplies(ctx, s.logger, endpoint.ProjectID)
	if err != nil || lockedDown {
		return nil, err
	}
	attempt, err := consentAttemptID(rawAttempt)
	if err != nil {
		return nil, err
	}
	inventory, found, err := s.getCompletedConsentInventory(ctx, state.ID, attempt)
	if err != nil || !found {
		return nil, err
	}
	if inventory.CompletedAt.IsZero() || time.Since(inventory.CompletedAt) > time.Minute {
		return nil, nil
	}
	backendHash, err := s.consentBackendFingerprint(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if inventory.BackendHash != backendHash {
		return nil, nil
	}
	if endpoint.ToolsetID.Valid {
		tools, _, err := s.authorizedToolsetConsentInventory(ctx, endpoint, state)
		if err != nil {
			return nil, err
		}
		current := inventory
		current.Tools = tools
		if consentInventoryFingerprint(&current) != consentInventoryFingerprint(&inventory) {
			return nil, nil
		}
	}
	return &inventory, nil
}

// consentBackendFingerprint binds discovery to access-relevant configuration.
// Display names and update timestamps do not change the authorization surface.
func (s *Service) consentBackendFingerprint(ctx context.Context, endpoint *ResolvedMcpEndpoint) (string, error) {
	evidence := map[string]any{"resource": endpoint.UpstreamResource, "toolset": endpoint.ToolsetID}
	if !endpoint.McpServerID.Valid {
		return consentEvidenceHash(evidence), nil
	}
	server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID: endpoint.McpServerID.UUID, ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		return "", fmt.Errorf("read consent backend: %w", err)
	}
	evidence["server"] = server.ID
	evidence["remote"] = server.RemoteMcpServerID
	evidence["tunnel"] = server.TunneledMcpServerID
	evidence["toolset"] = server.ToolsetID
	evidence["variation"] = server.ToolVariationsGroupID
	evidence["issuer"] = server.RemoteSessionIssuerID
	evidence["visibility"] = server.Visibility
	evidence["network"] = server.NetworkAccessMode
	if server.RemoteMcpServerID.Valid {
		remote, err := remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{
			ID: server.RemoteMcpServerID.UUID, ProjectID: endpoint.ProjectID,
		})
		if err != nil {
			return "", fmt.Errorf("read consent remote backend: %w", err)
		}
		headers, err := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, remote.ID, false)
		if err != nil {
			return "", fmt.Errorf("read consent backend headers: %w", err)
		}
		permissions := make([]string, 0, len(headers))
		for _, header := range headers {
			permissions = append(permissions, consentEvidenceHash([]any{header.Name, header.Value, header.ValueFromRequestHeader, header.IsRequired}))
		}
		slices.Sort(permissions)
		evidence["url"] = remote.Url
		evidence["headers"] = permissions
	}
	return consentEvidenceHash(evidence), nil
}
