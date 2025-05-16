package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack/repo"
)

const slackServer = "https://slack.com/api"

type SlackClient struct {
	clientID     string
	clientSecret string
	client       *http.Client
	enc          *encryption.Encryption
	repo         *repo.Queries
}

func NewSlackClient(clientID, clientSecret string, db *pgxpool.Pool, enc *encryption.Encryption) *SlackClient {
	return &SlackClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		client:       cleanhttp.DefaultPooledClient(),
		enc:          enc,
		repo:         repo.New(db),
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

type SlackAppAuthInfoResponse struct {
	OrganizationID     string
	ProjectID          uuid.UUID
	AccessToken        string
	TeamName           string
	TeamID             string
	DefaultToolsetSlug *string
}

func (s *SlackClient) GetAppAuthInfo(ctx context.Context, slackTeamID string) (*SlackAppAuthInfoResponse, error) {
	conn, err := s.repo.GetSlackAppConnectionByTeamID(ctx, slackTeamID)
	if err != nil {
		return nil, fmt.Errorf("failed to get slack app connection: %w", err)
	}

	decryptedAccessToken, err := s.enc.Decrypt(conn.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}

	return &SlackAppAuthInfoResponse{
		AccessToken:        decryptedAccessToken,
		OrganizationID:     conn.OrganizationID,
		ProjectID:          conn.ProjectID,
		TeamName:           conn.SlackTeamName,
		TeamID:             conn.SlackTeamID,
		DefaultToolsetSlug: conv.FromPGText[string](conn.DefaultToolsetSlug),
	}, nil
}

type SlackPostMessageInput struct {
	ChannelID string
	Message   string
	ThreadTS  *string
}

func (s *SlackClient) PostMessage(ctx context.Context, accessToken string, input SlackPostMessageInput) error {
	urlStr := slackServer + "/chat.postMessage"

	// Build form body
	form := url.Values{}
	form.Set("channel", input.ChannelID)
	form.Set("text", input.Message)
	if input.ThreadTS != nil {
		form.Set("thread_ts", *input.ThreadTS)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to Slack: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			fmt.Printf("failed to close response body: %v\n", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("received non-200 response from Slack: %d, and failed to read body: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("received non-200 response from Slack: %d, body: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if ok, exists := result["ok"].(bool); !exists || !ok {
		errMsg, _ := result["error"].(string)
		return fmt.Errorf("slack postMessage failed: %s", errMsg)
	}

	return nil
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
