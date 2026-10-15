package platformmcp

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RiskPolicyAudience describes positive grants, not exclusions or effective membership.
type RiskPolicyAudience struct {
	Type          string   `json:"type"`
	PrincipalURNs []string `json:"principal_urns"`
}

type riskPolicyAudienceReplacement struct {
	RiskPolicyAudience
	Confirm bool `json:"confirm"`
}

func parseRiskPolicyAudienceReplacement(raw json.RawMessage) (riskPolicyAudienceReplacement, []urn.Principal, error) {
	var value riskPolicyAudienceReplacement
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || !value.Confirm || value.PrincipalURNs == nil || len(value.PrincipalURNs) > 100 {
		return value, nil, invalidRiskPolicyRequest()
	}
	switch value.Type {
	case "everyone":
		if len(value.PrincipalURNs) != 0 {
			return value, nil, invalidRiskPolicyRequest()
		}
		return value, []urn.Principal{authz.AllUsersPrincipal()}, nil
	case "targeted":
		if len(value.PrincipalURNs) == 0 {
			return value, nil, invalidRiskPolicyRequest()
		}
	default:
		return value, nil, invalidRiskPolicyRequest()
	}
	principals := make([]urn.Principal, 0, len(value.PrincipalURNs))
	value.PrincipalURNs = canonicalStrings(value.PrincipalURNs)
	for _, raw := range value.PrincipalURNs {
		principal, err := urn.ParsePrincipal(raw)
		if err != nil || principal.String() != raw || (principal.Type != urn.PrincipalTypeUser && principal.Type != urn.PrincipalTypeRole) || principal == authz.AllUsersPrincipal() {
			return value, nil, invalidRiskPolicyRequest()
		}
		principals = append(principals, principal)
	}
	return value, principals, nil
}
