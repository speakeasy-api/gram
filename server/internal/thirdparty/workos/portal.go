package workos

import (
	"context"
	"encoding/json"
	"fmt"
)

// PortalIntent identifies a WorkOS Admin Portal flow.
type PortalIntent string

const (
	PortalIntentDSync              PortalIntent = "dsync"
	PortalIntentSSO                PortalIntent = "sso"
	PortalIntentAuditLogs          PortalIntent = "audit_logs"
	PortalIntentDomainVerification PortalIntent = "domain_verification"
	PortalIntentLogStreams         PortalIntent = "log_streams"
)

// GenerateAdminPortalLink mints a WorkOS Admin Portal link for the given org and intent.
// Pass an empty returnURL to let WorkOS use its default post-flow behavior.
func (wc *Client) GenerateAdminPortalLink(ctx context.Context, workosOrgID string, intent PortalIntent, returnURL string) (string, error) {
	body := struct {
		Organization string `json:"organization"`
		Intent       string `json:"intent"`
		ReturnURL    string `json:"return_url,omitempty"`
	}{
		Organization: workosOrgID,
		Intent:       string(intent),
		ReturnURL:    returnURL,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal portal link request: %w", err)
	}

	var out struct {
		Link string `json:"link"`
	}
	if err := wc.do(ctx, "POST", "/portal/generate_link", payload, &out); err != nil {
		return "", fmt.Errorf("generate portal link: %w", err)
	}

	return out.Link, nil
}
