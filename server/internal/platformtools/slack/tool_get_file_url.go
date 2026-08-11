package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameGetFileURL = "platform_slack_get_file_url"

// fileURLTTL bounds how long a minted download URL stays fetchable. The
// runtime calls inspect_asset immediately after this tool returns, so the
// window only needs to cover one tool round trip.
const fileURLTTL = 5 * time.Minute

// sealedFileFetch is the encrypted payload behind a minted download URL. It
// carries the Slack token so the proxy endpoint can perform the download
// without re-resolving trigger environments; the blob is AES-GCM sealed, so
// the token never appears in cleartext outside the server.
type sealedFileFetch struct {
	FileID string `json:"file_id"`
	Token  string `json:"token"`
	// ExpiresAt is a unix timestamp; the proxy rejects the URL after it.
	ExpiresAt int64 `json:"exp"`
}

type getFileURLInput struct {
	FileID string `json:"file_id" jsonschema:"Slack file ID of the image to get a download URL for."`
}

// NewGetFileURLTool mints short-lived download URLs for Slack image files.
// The returned URL points at the server's Slack file proxy, needs no
// credentials, and is meant to be passed to the runtime's inspect_asset tool
// so the assistant can see the image.
func NewGetFileURLTool(httpClient *guardian.HTTPClient, enc *encryption.Client, serverURL *url.URL) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "get_file_url",
			Name:        toolNameGetFileURL,
			Description: "Get a short-lived download URL for a Slack image file (png, jpeg, gif, or webp, up to 10 MiB) by file ID. Pass the returned download_url to the inspect_asset tool to view the image. Requires the files:read scope on the server's Slack token (SLACK_BOT_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[getFileURLInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: func(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
			return callGetFileURL(ctx, client, enc, serverURL, env, payload, wr)
		},
	}
}

func callGetFileURL(ctx context.Context, client *apiClient, enc *encryption.Client, serverURL *url.URL, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if enc == nil || serverURL == nil {
		return fmt.Errorf("slack file url minting is not configured")
	}

	var input getFileURLInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	fileID, err := requireString("file_id", input.FileID)
	if err != nil {
		return err
	}

	token, err := client.Token(tokenPreferBot, env)
	if err != nil {
		return fmt.Errorf("resolve slack token: %w", err)
	}

	ref, err := client.ResolveImageFile(ctx, fileID, token)
	if err != nil {
		return fmt.Errorf("resolve slack image file: %w", err)
	}

	expiresAt := time.Now().Add(fileURLTTL)
	sealed, err := json.Marshal(sealedFileFetch{
		FileID:    ref.FileID,
		Token:     token,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return fmt.Errorf("encode sealed file fetch: %w", err)
	}
	blob, err := enc.Encrypt(sealed)
	if err != nil {
		return fmt.Errorf("seal file fetch payload: %w", err)
	}

	downloadURL := serverURL.JoinPath("platform", "slack", "files")
	q := url.Values{}
	q.Set("t", blob)
	downloadURL.RawQuery = q.Encode()

	body, err := json.Marshal(map[string]any{
		"file": map[string]any{
			"id":       ref.FileID,
			"name":     ref.Name,
			"title":    ref.Title,
			"mimetype": ref.MimeType,
			"size":     ref.Size,
		},
		"download_url": downloadURL.String(),
		"expires_at":   expiresAt.UTC().Format(time.RFC3339),
		"note":         "pass download_url to the inspect_asset tool to view the image",
	})
	if err != nil {
		return fmt.Errorf("encode get file url response: %w", err)
	}
	return writeResponse(wr, body)
}
