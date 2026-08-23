package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// mintTimeout bounds the two Tailscale API calls a mint performs.
const mintTimeout = 30 * time.Second

// mintAuthKey exchanges the ingress's OAuth client for a fresh single-use,
// non-ephemeral, preauthorized auth key carrying the ingress's tags. Only
// called for a device with no stored identity; resumes never mint.
func (n *node) mintAuthKey(ctx context.Context) (string, error) {
	apiBase := n.provider.APIBase
	if apiBase == "" {
		apiBase = "https://api.tailscale.com"
	}

	ctx, cancel := context.WithTimeout(ctx, mintTimeout)
	defer cancel()

	form := url.Values{}
	form.Set("client_id", n.cfg.Credential.OAuthClientID)
	form.Set("client_secret", n.cfg.Credential.OAuthClientSecret)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/api/v2/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build oauth token request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("oauth token request: %w", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	tokenBody, err := io.ReadAll(io.LimitReader(tokenResp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read oauth token response: %w", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oauth token request failed: status %d", tokenResp.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tokenBody, &token); err != nil {
		return "", fmt.Errorf("decode oauth token response: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("oauth token response carried no access token")
	}

	keyPayload := map[string]any{
		"capabilities": map[string]any{
			"devices": map[string]any{
				"create": map[string]any{
					"reusable":      false,
					"ephemeral":     false,
					"preauthorized": true,
					"tags":          n.cfg.Tags,
				},
			},
		},
		// The key only needs to survive until this join completes.
		"expirySeconds": 3600,
		"description":   "gram network ingress " + n.cfg.ID.String(),
	}
	body, err := json.Marshal(keyPayload)
	if err != nil {
		return "", fmt.Errorf("encode key request: %w", err)
	}

	keyReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/api/v2/tailnet/-/keys", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build key request: %w", err)
	}
	keyReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	keyReq.Header.Set("Content-Type", "application/json")

	keyResp, err := http.DefaultClient.Do(keyReq)
	if err != nil {
		return "", fmt.Errorf("key request: %w", err)
	}
	defer func() { _ = keyResp.Body.Close() }()
	keyBody, err := io.ReadAll(io.LimitReader(keyResp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read key response: %w", err)
	}
	if keyResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("key request failed: status %d", keyResp.StatusCode)
	}
	var key struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(keyBody, &key); err != nil {
		return "", fmt.Errorf("decode key response: %w", err)
	}
	if key.Key == "" {
		return "", fmt.Errorf("key response carried no key")
	}
	return key.Key, nil
}
