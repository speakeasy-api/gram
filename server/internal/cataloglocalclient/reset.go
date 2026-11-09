package cataloglocalclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ValidateReset keeps reset separate from refresh and rejects implicit confirmation.
func ValidateReset(action, planID, confirmation string) error {
	if action == "bootstrap" {
		return fmt.Errorf("reset cannot bootstrap; preview and explicitly confirm a reset plan")
	}
	if err := Validate(action, planID); err != nil {
		return err
	}
	if action == "apply" && confirmation == "" {
		return fmt.Errorf("reset apply requires --confirmation with the exact server preview value")
	}
	if action != "apply" && confirmation != "" {
		return fmt.Errorf("--confirmation is only valid for reset apply")
	}
	return nil
}

// RunReset uses the same ephemeral staff login and backend as the Admin UI.
// Apply sends exactly one request, never obtains a replacement plan or retries.
func RunReset(ctx context.Context, adminURL, providerURL, email, action, planID, confirmation string) (any, error) {
	if err := ValidateReset(action, planID, confirmation); err != nil {
		return nil, err
	}
	return authenticated(ctx, adminURL, providerURL, email, func(client *http.Client, admin *url.URL) (any, error) {
		body, err := json.Marshal(struct {
			Action       string `json:"action"`
			PlanID       string `json:"planId,omitempty"`
			Confirmation string `json:"confirmation,omitempty"`
		}{action, planID, confirmation})
		if err != nil {
			return nil, fmt.Errorf("encode reset request: %w", err)
		}
		var result json.RawMessage
		if err := post(ctx, client, admin, "catalog.reset", string(body), &result); err != nil {
			return nil, err
		}
		if action == "apply" {
			var receipt struct {
				State string `json:"state"`
			}
			if err := json.Unmarshal(result, &receipt); err != nil {
				return nil, fmt.Errorf("decode reset receipt: %w", err)
			}
			if receipt.State != "completed" {
				return result, fmt.Errorf("destination reset incomplete (%s); inspect reset status and preview unresolved structural conflicts", receipt.State)
			}
		}
		return result, nil
	})
}
