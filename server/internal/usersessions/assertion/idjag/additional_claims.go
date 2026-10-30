package idjag

import (
	"encoding/json"
	"errors"
)

type additionalClaims struct {
	// Resource is raw so an absent claim is told apart from an empty, null, or non-string one.
	Resource json.RawMessage `json:"resource,omitempty"`
	ClientID string          `json:"client_id"`
	Email    string          `json:"email"`
	Scope    string          `json:"scope"`
}

// errResourceClaimInvalid is a present resource claim that is not a non-empty string.
var errResourceClaimInvalid = errors.New("resource claim must be a non-empty string")

// resource returns the resource claim and whether the assertion carries one.
func (c additionalClaims) resource() (string, bool, error) {
	if len(c.Resource) == 0 {
		return "", false, nil
	}
	var value *string
	if err := json.Unmarshal(c.Resource, &value); err != nil || value == nil || *value == "" {
		return "", true, errResourceClaimInvalid
	}
	return *value, true, nil
}
