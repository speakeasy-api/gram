// Package provider implements the hosted Google Chat connection protocol.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
)

// Config contains deployment-owned app configuration, never user input.
type Config struct {
	ClientID           string
	ClientSecret       string
	RedirectURL        string
	ServiceAccountJSON string
	Topic              string
	PushServiceAccount string
	ServerURL          string
}

// Client calls only fixed Google endpoints and never follows redirects.
type Client struct {
	Config Config
	HTTP   *guardian.HTTPClient
}

// Identity is the verified account that authorized the connection.
type Identity struct {
	Subject  string `json:"sub"`
	Email    string `json:"email"`
	Verified bool   `json:"email_verified"`
}

// Space is an accessible Chat space.
type Space struct {
	Name                string `json:"name"`
	DisplayName         string `json:"displayName"`
	ExternalUserAllowed bool   `json:"externalUserAllowed"`
}

// Member is a provider-reported space member.
type Member struct {
	Member struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Type        string `json:"type"`
		Email       string `json:"email"`
		Anonymous   bool   `json:"isAnonymous"`
	} `json:"member"`
	State string `json:"state"`
}

// Subscription contains the lease returned by Google.
type Subscription struct {
	Name           string    `json:"name"`
	ExpireTime     time.Time `json:"expireTime"`
	TargetResource string    `json:"targetResource"`
	State          string    `json:"state"`
	EventTypes     []string  `json:"eventTypes"`
	PayloadOptions struct {
		IncludeResource bool `json:"includeResource"`
	} `json:"payloadOptions"`
	Etag                 string `json:"etag"`
	NotificationEndpoint struct {
		PubsubTopic string `json:"pubsubTopic"`
	} `json:"notificationEndpoint"`
}

var scopes = []string{"openid", "email", "https://www.googleapis.com/auth/chat.spaces.create", "https://www.googleapis.com/auth/chat.spaces.readonly", "https://www.googleapis.com/auth/chat.memberships", "https://www.googleapis.com/auth/chat.memberships.app"}

func (c *Client) oauth() *oauth2.Config {
	return &oauth2.Config{ClientID: c.Config.ClientID, ClientSecret: c.Config.ClientSecret, Endpoint: google.Endpoint, RedirectURL: c.Config.RedirectURL, Scopes: scopes}
}
func (c *Client) Ready() bool {
	return c != nil && c.HTTP != nil && c.Config.ClientID != "" && c.Config.ClientSecret != "" && c.Config.ServiceAccountJSON != "" && c.Config.Topic != "" && c.Config.PushServiceAccount != ""
}
func (c *Client) AuthorizationURL(state, challenge string) string {
	return c.oauth().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"))
}
func (c *Client) context(ctx context.Context) context.Context {
	h := *c.HTTP
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return context.WithValue(ctx, oauth2.HTTPClient, &h)
}
func (c *Client) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, Identity, error) {
	token, err := c.oauth().Exchange(c.context(ctx), code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, Identity{}, errors.New("google authorization exchange failed")
	}
	if token.RefreshToken == "" {
		return nil, Identity{}, errors.New("google did not grant offline access; reconnect and approve access")
	}
	granted, _ := token.Extra("scope").(string)
	for _, scope := range scopes[2:] {
		if !strings.Contains(" "+granted+" ", " "+scope+" ") {
			return nil, Identity{}, errors.New("google did not grant all required Chat permissions")
		}
	}
	var identity Identity
	if err := c.Call(ctx, token.AccessToken, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil, &identity); err != nil {
		return nil, Identity{}, err
	}
	if identity.Subject == "" || identity.Email == "" || !identity.Verified {
		return nil, Identity{}, errors.New("google account identity is not verified")
	}
	return token, identity, nil
}
func (c *Client) Refresh(ctx context.Context, token *oauth2.Token) (*oauth2.Token, error) {
	value, err := c.oauth().TokenSource(c.context(ctx), token).Token()
	if err != nil {
		return nil, errors.New("google authorization expired or was revoked; reconnect")
	}
	return value, nil
}
func (c *Client) AppToken(ctx context.Context, cloud bool) (string, error) {
	scope := "https://www.googleapis.com/auth/chat.bot"
	if cloud {
		scope = "https://www.googleapis.com/auth/cloud-platform"
	}
	return c.appToken(ctx, scope)
}

// AppMessageToken obtains app authority for message events and history.
func (c *Client) AppMessageToken(ctx context.Context) (string, error) {
	return c.appToken(ctx, "https://www.googleapis.com/auth/chat.app.messages.readonly", "https://www.googleapis.com/auth/chat.bot")
}
func (c *Client) appToken(ctx context.Context, requestedScopes ...string) (string, error) {
	var raw struct {
		Email string `json:"client_email"`
		Key   string `json:"private_key"`
		Type  string `json:"type"`
	}
	if json.Unmarshal([]byte(c.Config.ServiceAccountJSON), &raw) != nil || raw.Type != "service_account" || raw.Key == "" {
		return "", errors.New("hosted Google Chat app credentials are not configured")
	}
	cfg := jwt.Config{Email: raw.Email, PrivateKey: []byte(raw.Key), Scopes: requestedScopes, TokenURL: google.Endpoint.TokenURL, PrivateKeyID: "", Subject: "", Expires: 0, Audience: "", PrivateClaims: nil, UseIDToken: false}
	token, err := cfg.TokenSource(c.context(ctx)).Token()
	if err != nil {
		return "", errors.New("hosted Google Chat app authentication failed")
	}
	return token.AccessToken, nil
}
func (c *Client) Call(ctx context.Context, token, method, endpoint string, body, result any) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return errors.New("invalid Google endpoint")
	}
	switch u.Host {
	case "chat.googleapis.com", "workspaceevents.googleapis.com", "pubsub.googleapis.com", "openidconnect.googleapis.com", "oauth2.googleapis.com":
	default:
		return errors.New("invalid Google endpoint")
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("encode Google request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("build Google request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	h := *c.HTTP
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(req)
	if err != nil {
		return errors.New("google request failed")
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode}
	}
	if result == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(result); err != nil {
		return errors.New("decode Google response")
	}
	return nil
}
func (c *Client) Spaces(ctx context.Context, token string) ([]Space, error) {
	result := []Space{}
	page := ""
	for range 100 {
		var v struct {
			Spaces []Space `json:"spaces"`
			Next   string  `json:"nextPageToken"`
		}
		err := c.Call(ctx, token, http.MethodGet, "https://chat.googleapis.com/v1/spaces?pageSize=100&filter="+url.QueryEscape(`spaceType = "SPACE"`)+"&pageToken="+url.QueryEscape(page), nil, &v)
		if err != nil {
			return nil, err
		}
		result = append(result, v.Spaces...)
		if v.Next == "" {
			return result, nil
		}
		page = v.Next
	}
	return nil, errors.New("too many Google Chat spaces")
}
func (c *Client) Members(ctx context.Context, token, space string) ([]Member, error) {
	result := []Member{}
	page := ""
	for range 100 {
		var v struct {
			Members []Member `json:"memberships"`
			Next    string   `json:"nextPageToken"`
		}
		err := c.Call(ctx, token, http.MethodGet, "https://chat.googleapis.com/v1/"+space+"/members?pageSize=100&pageToken="+url.QueryEscape(page), nil, &v)
		if err != nil {
			return nil, err
		}
		result = append(result, v.Members...)
		if v.Next == "" {
			return result, nil
		}
		page = v.Next
	}
	return nil, errors.New("too many Google Chat members")
}

// APIError contains only an HTTP status, never provider content or credentials.
type APIError struct{ Status int }

func (e *APIError) Error() string { return fmt.Sprintf("google API returned HTTP %d", e.Status) }
