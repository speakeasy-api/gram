package gram

import (
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections"
	googleprovider "github.com/speakeasy-api/gram/server/internal/googlechatconnections/provider"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/urfave/cli/v2"
)

// googleChatFlags is shared by the HTTP server and renewal worker.
func googleChatFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "google-chat-client-id", EnvVars: []string{"GOOGLE_CHAT_CLIENT_ID"}, Usage: "OAuth client ID for the hosted Speakeasy Google Chat app"},
		&cli.StringFlag{Name: "google-chat-client-secret", EnvVars: []string{"GOOGLE_CHAT_CLIENT_SECRET"}, Usage: "OAuth client secret for Google Chat account connections"},
		&cli.StringFlag{Name: "hosted-google-chat-service-account-json", EnvVars: []string{"HOSTED_GOOGLE_CHAT_SERVICE_ACCOUNT_JSON"}, Usage: "Service account JSON for the hosted Chat app and its event delivery"},
		&cli.StringFlag{Name: "google-chat-topic", EnvVars: []string{"GOOGLE_CHAT_TOPIC"}, Usage: "Full Pub/Sub topic resource name for hosted Chat events"},
		&cli.StringFlag{Name: "google-chat-push-service-account", EnvVars: []string{"GOOGLE_CHAT_PUSH_SERVICE_ACCOUNT"}, Usage: "Service account email used to authenticate Chat event pushes"},
	}
}

// newHostedGoogleChat constructs the same provider and runtime for both processes.
func newHostedGoogleChat(c *cli.Context, db *pgxpool.Pool, http *guardian.HTTPClient, serverURL *url.URL) (*googleprovider.Client, *googleprovider.Runtime) {
	provider := &googleprovider.Client{HTTP: http, Config: googleprovider.Config{ClientID: c.String("google-chat-client-id"), ClientSecret: c.String("google-chat-client-secret"), RedirectURL: serverURL.JoinPath(googlechatconnections.CallbackPath).String(), ServiceAccountJSON: c.String("hosted-google-chat-service-account-json"), Topic: c.String("google-chat-topic"), PushServiceAccount: c.String("google-chat-push-service-account"), ServerURL: serverURL.String()}}
	return provider, &googleprovider.Runtime{RefreshMembers: nil, DB: db, Client: provider}
}
