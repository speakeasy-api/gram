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

// SSOIntentOptions configures the SSO flow in the Admin Portal.
type SSOIntentOptions struct {
	BookmarkSlug string `json:"bookmark_slug,omitempty"`
	ProviderType string `json:"provider_type,omitempty"`
}

// DomainVerificationIntentOptions configures the domain verification flow in the Admin Portal.
type DomainVerificationIntentOptions struct {
	DomainName string `json:"domain_name,omitempty"`
}

// IntentOptions groups per-intent configuration for the Admin Portal.
type IntentOptions struct {
	SSO                *SSOIntentOptions                `json:"sso,omitempty"`
	DomainVerification *DomainVerificationIntentOptions `json:"domain_verification,omitempty"`
}

// GenerateAdminPortalLinkOpts holds all optional parameters for generating an Admin Portal link.
type GenerateAdminPortalLinkOpts struct {
	ReturnURL       string         `json:"return_url,omitempty"`
	SuccessURL      string         `json:"success_url,omitempty"`
	ITContactEmails []string       `json:"it_contact_emails,omitempty"`
	IntentOptions   *IntentOptions `json:"intent_options,omitempty"`
}

// GenerateAdminPortalLink mints a WorkOS Admin Portal link for the given org and intent.
func (wc *Client) GenerateAdminPortalLink(ctx context.Context, workosOrgID string, intent PortalIntent, opts GenerateAdminPortalLinkOpts) (string, error) {
	body := struct {
		Organization    string         `json:"organization"`
		Intent          string         `json:"intent"`
		ReturnURL       string         `json:"return_url,omitempty"`
		SuccessURL      string         `json:"success_url,omitempty"`
		ITContactEmails []string       `json:"it_contact_emails,omitempty"`
		IntentOptions   *IntentOptions `json:"intent_options,omitempty"`
	}{
		Organization:    workosOrgID,
		Intent:          string(intent),
		ReturnURL:       opts.ReturnURL,
		SuccessURL:      opts.SuccessURL,
		ITContactEmails: opts.ITContactEmails,
		IntentOptions:   opts.IntentOptions,
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
