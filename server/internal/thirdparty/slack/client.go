package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/hashicorp/go-cleanhttp"
)

const slackServer = "https://slack.com/api"

type SlackClient struct {
	clientID     string
	clientSecret string
	client       *http.Client
}

func NewSlackClient(clientID, clientSecret string) *SlackClient {
	return &SlackClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		client:       cleanhttp.DefaultPooledClient(),
	}
}

type slackOAuthResponse struct {
	Ok          bool   `json:"ok"`
	AccessToken string `json:"access_token"`
	Team        struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"team"`
	Error string `json:"error"`
}

func (s *SlackClient) OAuthV2Access(ctx context.Context, code, initialRedirectUI string) (*slackOAuthResponse, error) {
	tokenURL := slackServer + "/oauth.v2.access"
	data := url.Values{}
	data.Set("client_id", s.clientID)
	data.Set("client_secret", s.clientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", initialRedirectUI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.URL.RawQuery = data.Encode()

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to complete slack oauth request: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			// Log or handle the error as appropriate. For now, just print to stderr.
			fmt.Printf("failed to close response body: %v\n", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 response in slack oauth: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var oauthResponse slackOAuthResponse

	if err := json.Unmarshal(body, &oauthResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if oauthResponse.Error != "" {
		return nil, fmt.Errorf("slack oauth failed with: %s", oauthResponse.Error)
	}

	return &oauthResponse, nil
}
