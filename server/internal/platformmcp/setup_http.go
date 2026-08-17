package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const providerSetupStartPath = "/platform-mcp/provider-setup"

type DashboardSetupStarter interface {
	StartDashboardSetup(ctx context.Context, userID, organizationID, handoff string) (ProviderSetupResult, error)
}

type DashboardSetupService struct {
	store       *RegistrationStore
	gate        CatalogRegistrationGateChecker
	authorizer  Authorizer
	adapters    *ProviderAdapters
	setupBudget OperationBudget
}

func NewDashboardSetupService(store *RegistrationStore, gate CatalogRegistrationGateChecker, authorizer Authorizer, adapters *ProviderAdapters, setupBudget OperationBudget) *DashboardSetupService {
	return &DashboardSetupService{store: store, gate: gate, authorizer: authorizer, adapters: adapters, setupBudget: setupBudget}
}

func (s *DashboardSetupService) StartDashboardSetup(ctx context.Context, userID, organizationID, handoff string) (ProviderSetupResult, error) {
	if s == nil || s.store == nil || s.store.db == nil || s.gate == nil || s.authorizer == nil || s.adapters == nil || !s.setupBudget.valid() || userID == "" || organizationID == "" || handoff == "" {
		return ProviderSetupResult{}, ErrUnavailable
	}

	principal := Principal{
		Surface:        SurfaceDashboard,
		UserID:         userID,
		OrganizationID: organizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "",
	}
	if err := s.authorizer.RequireLiveOrgAdmin(ctx, principal); err != nil {
		if isAuthorizationDenied(err) {
			return ProviderSetupResult{}, ErrForbidden
		}
		return ProviderSetupResult{}, fmt.Errorf("authorize platform mcp dashboard setup: %w: %w", ErrUnavailable, err)
	}
	row, err := platformrepo.New(s.store.db).GetPlatformMCPSetupHandoffForDashboardStart(ctx, platformrepo.GetPlatformMCPSetupHandoffForDashboardStartParams{
		HandoffHash:    setupHandoffHash(handoff),
		OrganizationID: organizationID,
		SubjectUrn:     userSubjectURN(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderSetupResult{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return ProviderSetupResult{}, fmt.Errorf("load platform mcp setup handoff for dashboard start: %w", err)
	}
	principal.ConnectionID = row.ConnectionID.UUID.String()
	principal.Generation = row.ConnectionGeneration.UUID.String()
	if err := s.setupBudget.Allow(ctx, principal); err != nil {
		return ProviderSetupResult{}, err
	}
	enabled, err := s.gate.Enabled(ctx, organizationID, row.ProjectSlug)
	if err != nil {
		return ProviderSetupResult{}, fmt.Errorf("check platform mcp dashboard setup gate: %w: %w", ErrUnavailable, err)
	}
	if !enabled {
		return ProviderSetupResult{}, ErrRegistrationUnavailable
	}
	eligible, err := s.store.EligibleCatalogRegistrationTarget(ctx, organizationID, ResolvedProject{ID: row.ProjectID, Name: "", Slug: row.ProjectSlug})
	if err != nil {
		return ProviderSetupResult{}, fmt.Errorf("check platform mcp dashboard setup target eligibility: %w", err)
	}
	if !eligible {
		return ProviderSetupResult{}, ErrTargetIneligible
	}
	return s.store.BeginProviderSetup(ctx, principal, SetupHandoffBinding{
		ProjectID:        row.ProjectID,
		RegistrationID:   row.RegistrationID,
		ProviderKey:      row.ProviderKey,
		CatalogReference: row.CatalogReference,
		Intent:           row.Intent,
	}, handoff, s.adapters)
}

type DashboardSessionAuthenticator interface {
	AuthenticateWithCookie(ctx context.Context) (context.Context, error)
}

type DashboardSetupHTTP struct {
	starter  DashboardSetupStarter
	sessions DashboardSessionAuthenticator
}

func NewDashboardSetupHTTP(starter DashboardSetupStarter, sessionManager DashboardSessionAuthenticator) *DashboardSetupHTTP {
	return &DashboardSetupHTTP{starter: starter, sessions: sessionManager}
}

func (s *DashboardSetupHTTP) Attach(mux interface {
	Handle(string, string, http.HandlerFunc)
}) {
	mux.Handle(http.MethodPost, providerSetupStartPath, handlerFunc(s.Handler()))
}

func (s *DashboardSetupHTTP) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if s == nil || s.starter == nil || s.sessions == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, err := s.sessions.AuthenticateWithCookie(r.Context())
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		auth, ok := contextvalues.GetAuthContext(ctx)
		if !ok || auth == nil || auth.UserID == "" || auth.ActiveOrganizationID == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if !requireJSON(w, r, 16<<10) {
			return
		}
		var request struct {
			Handoff string `json:"handoff"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Handoff == "" {
			http.Error(w, "invalid setup handoff", http.StatusBadRequest)
			return
		}
		result, err := s.starter.StartDashboardSetup(ctx, auth.UserID, auth.ActiveOrganizationID, request.Handoff)
		if errors.Is(err, ErrOperationRateLimited) || errors.Is(err, ErrReadinessRateLimited) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"code": "rate_limited"})
			return
		}
		if errors.Is(err, ErrOperationBudgetUnavailable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": unavailableCode})
			return
		}
		if errors.Is(err, ErrSetupHandoffReissueRequired) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "setup_handoff_reissue_required"})
			return
		}
		if errors.Is(err, ErrSetupHandoffInvalid) || errors.Is(err, ErrRegistrationUnavailable) || errors.Is(err, ErrTargetIneligible) {
			http.Error(w, "setup unavailable", http.StatusForbidden)
			return
		}
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrUnauthorized) || isAuthorizationDenied(err) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"authorization_url": result.AuthorizationURL})
	})
}
