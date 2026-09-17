package identityproviderconnections

import (
	"slices"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
)

// connectionRows is a connection with its Okta subtype and managed client.
type connectionRows struct {
	Connection repo.IdentityProviderConnection
	Okta       repo.OktaIdentityProviderConnection
	Managed    *ManagedClient
}

func (r connectionRows) clientIDSubmitted() bool {
	return r.Managed.ClientID != PlaceholderClientID(r.Connection.Provider, r.Connection.ID)
}

func (r connectionRows) reasons() []string {
	reasons, _ := parseLastError(r.Connection.LastError.String)
	return reasons
}

func (r connectionRows) lastError() *string {
	_, failure := parseLastError(r.Connection.LastError.String)
	return conv.PtrEmpty(failure)
}

// checked reports whether a verification has ever completed against Okta.
func (r connectionRows) checked() bool {
	return r.Connection.Status != StatusPending
}

func missingScopes(granted []string) []string {
	missing := make([]string, 0, len(RequiredOktaScopes))
	for _, scope := range RequiredOktaScopes {
		if !slices.Contains(granted, scope) {
			missing = append(missing, scope)
		}
	}
	return missing
}

func buildConnectionView(r connectionRows) *gen.OktaIdentityProviderConnection {
	var clientID *string
	if r.clientIDSubmitted() {
		clientID = conv.PtrEmpty(r.Managed.ClientID)
	}

	var activeKey *gen.IdentityProviderConnectionActiveKey
	if r.Managed.ActiveKeyID.Valid {
		activeKey = &gen.IdentityProviderConnectionActiveKey{
			ID:          r.Managed.ActiveKeyID.UUID.String(),
			Kid:         r.Managed.ActiveKid,
			ActivatedAt: r.Managed.ActivatedAt.UTC().Format(time.RFC3339),
		}
	}

	granted := r.Okta.GrantedScopes
	if granted == nil {
		granted = []string{}
	}
	missing := []string{}
	if r.checked() {
		missing = missingScopes(granted)
	}

	checklist := OktaChecklist(r.Okta.ListingMode, r.Managed.JSONWebKeySetURL)
	items := make([]*gen.IdentityProviderConnectionChecklistItem, 0, len(checklist))
	for _, item := range checklist {
		items = append(items, &gen.IdentityProviderConnectionChecklistItem{Key: item.Key, Title: item.Title, Description: item.Description})
	}

	return &gen.OktaIdentityProviderConnection{
		ID:                  r.Connection.ID.String(),
		OrganizationID:      r.Connection.OrganizationID,
		Provider:            r.Connection.Provider,
		Status:              r.Connection.Status,
		OrgURL:              r.Okta.OrgUrl,
		IssuerURL:           r.Okta.IssuerUrl,
		ListingMode:         r.Okta.ListingMode,
		JwksURL:             r.Managed.JSONWebKeySetURL,
		ClientID:            clientID,
		ClientIDSubmitted:   r.clientIDSubmitted(),
		DpopRequired:        r.Okta.DpopRequired,
		RequiredScopes:      slices.Clone(RequiredOktaScopes),
		GrantedScopes:       slices.Clone(granted),
		MissingScopes:       missing,
		VerificationReasons: r.reasons(),
		LastVerifiedAt:      conv.PtrEmpty(conv.FromPGTimestamptz(r.Connection.LastVerifiedAt)),
		LastError:           r.lastError(),
		AgentID:             conv.FromPGText[string](r.Okta.AgentID),
		AgentAppID:          conv.FromPGText[string](r.Okta.AgentAppID),
		ActiveKey:           activeKey,
		Checklist:           items,
		CreatedAt:           conv.FromPGTimestamptz(r.Connection.CreatedAt),
		UpdatedAt:           conv.FromPGTimestamptz(r.Connection.UpdatedAt),
	}
}

func snapshot(r connectionRows) *audit.IdentityProviderConnectionSnapshot {
	clientID := ""
	if r.clientIDSubmitted() {
		clientID = r.Managed.ClientID
	}
	granted := r.Okta.GrantedScopes
	if granted == nil {
		granted = []string{}
	}
	return &audit.IdentityProviderConnectionSnapshot{
		Provider:      r.Connection.Provider,
		Status:        r.Connection.Status,
		OrgURL:        r.Okta.OrgUrl,
		ListingMode:   r.Okta.ListingMode,
		ClientID:      clientID,
		DPoPRequired:  r.Okta.DpopRequired,
		GrantedScopes: slices.Clone(granted),
		Reasons:       r.reasons(),
		LastError:     conv.PtrValOr(r.lastError(), ""),
		AgentID:       r.Okta.AgentID.String,
		AgentAppID:    r.Okta.AgentAppID.String,
	}
}
