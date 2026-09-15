package mv

import (
	"encoding/json"
	"fmt"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
)

type storedIdentityProviderVerification struct {
	Outcome       string                           `json:"outcome"`
	Detail        string                           `json:"detail"`
	Capabilities  []string                         `json:"capabilities"`
	GrantedScopes []string                         `json:"granted_scopes"`
	CheckedAt     string                           `json:"checked_at"`
	Reads         []identityProviderCapabilityRead `json:"reads"`
}

type identityProviderCapabilityRead struct {
	Capability string  `json:"capability"`
	Resource   string  `json:"resource"`
	OK         bool    `json:"ok"`
	Count      *int    `json:"count,omitempty"`
	Detail     *string `json:"detail,omitempty"`
}

// BuildIdentityProviderConnectionView converts a connection row into its API
// shape without exposing provider credentials or private signing material.
func BuildIdentityProviderConnectionView(row repo.GetIdentityProviderConnectionByOrganizationRow, jwksURL string) (*gen.IdentityProviderConnection, error) {
	verification, err := BuildIdentityProviderVerifyResultView(row)
	if err != nil {
		return nil, err
	}
	var verifyEvidence *gen.IdentityProviderVerifyEvidence
	if verification != nil {
		verifyEvidence = verification.Evidence
	}

	return &gen.IdentityProviderConnection{
		ID:                   row.ID.String(),
		Kind:                 row.Kind,
		TenantIdentifier:     row.TenantIdentifier,
		DisplayName:          conv.FromPGText[string](row.DisplayName),
		ClientID:             conv.FromPGText[string](row.ClientID),
		Status:               row.Status,
		StatusDetail:         conv.FromPGText[string](row.StatusDetail),
		Capabilities:         row.Capabilities,
		GrantedScopes:        row.GrantedScopes,
		JwksURL:              jwksURL,
		SigningKeyKid:        row.SigningKeyKid,
		LastVerifiedAt:       conv.PtrEmpty(conv.FromPGTimestamptz(row.LastVerifiedAt)),
		VerifyEvidence:       verifyEvidence,
		SignInState:          conv.FromPGText[string](row.SignInState),
		SignInConnectionID:   conv.FromPGText[string](row.WorkosConnectionID),
		GroupsSource:         conv.FromPGText[string](row.GroupsSource),
		GroupsClaimConfirmed: &row.GroupsClaimConfirmed,
		CreatedAt:            conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:            conv.FromPGTimestamptz(row.UpdatedAt),
	}, nil
}

// BuildIdentityProviderVerifyResultView reconstructs the last persisted verification result.
func BuildIdentityProviderVerifyResultView(row repo.GetIdentityProviderConnectionByOrganizationRow) (*gen.IdentityProviderVerifyResult, error) {
	if len(row.VerifyEvidence) == 0 {
		return nil, nil
	}

	var stored storedIdentityProviderVerification
	if err := json.Unmarshal(row.VerifyEvidence, &stored); err != nil {
		return nil, fmt.Errorf("decode identity provider verification evidence: %w", err)
	}
	if !validIdentityProviderVerifyOutcome(stored.Outcome) {
		return nil, nil
	}
	if _, err := time.Parse(time.RFC3339, stored.CheckedAt); err != nil {
		return nil, nil
	}
	reads := make([]*gen.IdentityProviderCapabilityRead, 0, len(stored.Reads))
	for _, read := range stored.Reads {
		if !validIdentityProviderCapabilityResource(read.Resource) {
			continue
		}
		reads = append(reads, &gen.IdentityProviderCapabilityRead{
			Capability: read.Capability,
			Resource:   read.Resource,
			OK:         read.OK,
			Count:      read.Count,
			Detail:     read.Detail,
		})
	}
	return &gen.IdentityProviderVerifyResult{
		Outcome:       stored.Outcome,
		Detail:        stored.Detail,
		Capabilities:  stored.Capabilities,
		GrantedScopes: stored.GrantedScopes,
		Evidence: &gen.IdentityProviderVerifyEvidence{
			CheckedAt: stored.CheckedAt,
			Reads:     reads,
		},
	}, nil
}

func validIdentityProviderVerifyOutcome(outcome string) bool {
	switch outcome {
	case "passed", "unreachable", "refused", "mismatched_value", "capability_missing":
		return true
	default:
		return false
	}
}

func validIdentityProviderCapabilityResource(resource string) bool {
	switch resource {
	case "groups", "users", "apps", "sign_in_application", "sign_in_connection":
		return true
	default:
		return false
	}
}
