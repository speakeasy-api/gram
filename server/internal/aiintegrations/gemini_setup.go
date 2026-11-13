package aiintegrations

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
)

const (
	// geminiMatterName is the Vault matter Speakeasy finds or creates for its
	// exports. Reusing it by name keeps reconnects on one matter, and with it
	// the matter-scoped chat identities that deduplicate re-imported turns.
	geminiMatterName        = "Speakeasy Gemini import"
	geminiMatterDescription = "Created by Speakeasy to export Gemini app conversations for the AI Integrations import. Closing or deleting it stops the import."

	// Setup is an interactive save that can list matters, create one, and
	// list organizational units.
	geminiSetupTimeout = 30 * time.Second
)

// geminiSetupClient is the part of the Vault client setup needs.
type geminiSetupClient interface {
	EnsureMatter(ctx context.Context, name, description string) (string, error)
	GetMatter(ctx context.Context) error
	ListOrgUnits(ctx context.Context) ([]geminivault.OrgUnit, error)
}

// geminiSetupRequest is one save's Gemini inputs. Nil pointers keep the saved
// value; an empty org unit clears the selection and holds syncing.
type geminiSetupRequest struct {
	APIKey         string
	APIKeySupplied bool
	DelegatedUser  *string
	OrgUnitID      *string
	MatterID       *string
	Before         *Config
	Reenabled      bool
}

type geminiSetup struct {
	Settings ProviderSettings
	MatterID string
}

// prepareGemini checks a Gemini save against Google before it is written: the
// key can act as the delegated admin, the matter is usable (found or created
// when none is configured), and a selected org unit is one the admin can read.
// Unchanged settings skip the network entirely.
func (s *Service) prepareGemini(ctx context.Context, req geminiSetupRequest) (geminiSetup, error) {
	settings := ProviderSettings{DelegatedUser: "", OrgUnitID: "", OrgUnitPath: ""}
	beforeMatter := ""
	if req.Before != nil {
		settings = req.Before.Settings
		beforeMatter = conv.PtrValOr(req.Before.ExternalOrganizationID, "")
	}
	if req.DelegatedUser != nil {
		settings.DelegatedUser = strings.TrimSpace(*req.DelegatedUser)
	}
	requestedOrgUnit := settings.OrgUnitID
	if req.OrgUnitID != nil {
		requestedOrgUnit = strings.TrimSpace(*req.OrgUnitID)
	}
	matterID := beforeMatter
	if req.MatterID != nil && strings.TrimSpace(*req.MatterID) != "" {
		matterID = strings.TrimSpace(*req.MatterID)
		if _, err := uuid.Parse(matterID); err != nil {
			return geminiSetup{}, oops.E(oops.CodeInvalid, nil, "external_organization_id must be a Google Vault matter UUID")
		}
	}

	if err := geminivault.ParseServiceAccount(req.APIKey); err != nil {
		return geminiSetup{}, oops.E(oops.CodeInvalid, nil, "api_key must be a valid Google service account JSON key file with an RSA private key of at least 2048 bits and a Google token endpoint")
	}
	if err := geminivault.ValidateDelegatedUser(settings.DelegatedUser); err != nil {
		return geminiSetup{}, oops.E(oops.CodeInvalid, nil, "delegated_user must be the email address of a Google Workspace admin")
	}

	credentialsChanged := req.Before == nil || req.APIKeySupplied || settings.DelegatedUser != req.Before.Settings.DelegatedUser
	orgUnitChanged := req.Before == nil || requestedOrgUnit != req.Before.Settings.OrgUnitID
	checkMatter := matterID == "" || matterID != beforeMatter || credentialsChanged || req.Reenabled
	checkOrgUnit := requestedOrgUnit != "" && (orgUnitChanged || credentialsChanged)
	if requestedOrgUnit == "" {
		settings.OrgUnitID, settings.OrgUnitPath = "", ""
	}
	if !checkMatter && !checkOrgUnit {
		return geminiSetup{Settings: settings, MatterID: matterID}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, geminiSetupTimeout)
	defer cancel()
	client, err := s.newGeminiClient(geminivault.Config{
		ServiceAccountJSON: req.APIKey,
		DelegatedUser:      settings.DelegatedUser,
		MatterID:           matterID,
		OrgUnitID:          "",
	})
	if err != nil {
		return geminiSetup{}, oops.E(oops.CodeInvalid, err, "the Gemini service account key or delegated admin is invalid")
	}
	if checkMatter {
		if matterID == "" {
			matterID, err = client.EnsureMatter(ctx, geminiMatterName, geminiMatterDescription)
		} else {
			err = client.GetMatter(ctx)
		}
		if err != nil {
			return geminiSetup{}, geminiSetupError(geminiStepVault, err)
		}
	}
	if checkOrgUnit {
		units, err := client.ListOrgUnits(ctx)
		if err != nil {
			return geminiSetup{}, geminiSetupError(geminiStepDirectory, err)
		}
		found := false
		for _, unit := range units {
			if unit.ID == requestedOrgUnit {
				settings.OrgUnitID, settings.OrgUnitPath = unit.ID, unit.Path
				found = true
				break
			}
		}
		if !found {
			return geminiSetup{}, oops.E(oops.CodeInvalid, nil, "organizational unit not found; choose one from the list for this Workspace")
		}
	}
	return geminiSetup{Settings: settings, MatterID: matterID}, nil
}

// ListOrgUnits lists the org units a saved Gemini connection can export, so
// the dashboard can offer them after the key and delegated admin are saved.
func (s *Service) ListOrgUnits(ctx context.Context, payload *gen.ListOrgUnitsPayload) (*gen.ListAIIntegrationOrgUnitsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	provider, err := normalizeProvider(payload.Provider)
	if err != nil {
		return nil, err
	}
	if provider != ProviderGeminiCompliance {
		return nil, oops.E(oops.CodeInvalid, nil, "listOrgUnits supports only %s", ProviderGeminiCompliance)
	}
	cfg, row, err := s.store.loadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return nil, err
	}
	if row == nil || cfg.APIKey == "" || cfg.Settings.DelegatedUser == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "save the Gemini service account key and delegated admin first")
	}
	client, err := s.newGeminiClient(geminivault.Config{
		ServiceAccountJSON: cfg.APIKey,
		DelegatedUser:      cfg.Settings.DelegatedUser,
		MatterID:           "",
		OrgUnitID:          "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "the saved Gemini credentials are invalid; save them again")
	}
	ctx, cancel := context.WithTimeout(ctx, geminiSetupTimeout)
	defer cancel()
	units, err := client.ListOrgUnits(ctx)
	if err != nil {
		return nil, geminiSetupError(geminiStepDirectory, err)
	}
	result := &gen.ListAIIntegrationOrgUnitsResult{OrgUnits: make([]*gen.AIIntegrationOrgUnit, 0, len(units))}
	for _, unit := range units {
		result.OrgUnits = append(result.OrgUnits, &gen.AIIntegrationOrgUnit{
			ID:          unit.ID,
			Path:        unit.Path,
			Name:        unit.Name,
			Description: conv.PtrEmpty(unit.Description),
		})
	}
	return result, nil
}

type geminiSetupStep int

const (
	geminiStepVault geminiSetupStep = iota
	geminiStepDirectory
)

// geminiSetupError turns a Google answer into a save error that says what to
// fix. Upstream bodies never reach it: the client keeps only status codes.
func geminiSetupError(step geminiSetupStep, err error) error {
	if errors.Is(err, geminivault.ErrMatterNotOpen) {
		return oops.E(oops.CodeInvalid, err, "the Google Vault matter is not open; reopen it in Google Vault")
	}
	if httpErr, ok := errors.AsType[*geminivault.HTTPError](err); ok {
		if httpErr.CredentialsRejected() {
			if step == geminiStepDirectory {
				return oops.E(oops.CodeUnauthorized, err, "Google refused a directory token; add https://www.googleapis.com/auth/admin.directory.orgunit.readonly to the service account's domain-wide delegation")
			}
			return oops.E(oops.CodeUnauthorized, err, "Google rejected the service account credentials; check the key, its domain-wide delegation scopes, and the delegated admin")
		}
		switch status := httpErr.StatusCode; {
		case status == http.StatusUnauthorized || status == http.StatusForbidden || (step == geminiStepDirectory && status == http.StatusNotFound):
			if step == geminiStepDirectory {
				return oops.E(oops.CodeForbidden, err, "Google denied the organizational unit list; the delegated admin needs permission to read organizational units")
			}
			return oops.E(oops.CodeForbidden, err, "Google Vault denied access; the delegated admin needs a Vault license, the Manage Matters, Manage Searches and Manage Exports privileges, and access to the matter")
		case status == http.StatusNotFound:
			return oops.E(oops.CodeInvalid, err, "Google Vault matter not found; check the matter ID")
		case status == http.StatusTooManyRequests || status >= 500:
			return oops.E(oops.CodeUnavailable, err, "Google is temporarily unavailable (HTTP %d); try saving again", status)
		default:
			return oops.E(oops.CodeInvalid, err, "Google rejected the setup request (HTTP %d)", status)
		}
	}
	return oops.E(oops.CodeUnavailable, err, "could not complete the Google setup request; try saving again")
}
