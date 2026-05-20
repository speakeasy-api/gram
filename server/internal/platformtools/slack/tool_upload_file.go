package slack

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameUploadFile = "platform_slack_upload_file"

type uploadFileInput struct {
	Filename       string  `json:"filename" jsonschema:"Name of the file being uploaded (used by Slack as the displayed filename)."`
	ContentBase64  string  `json:"content_base64" jsonschema:"Base64-encoded file bytes. The decoded length is sent to Slack as the file size."`
	Title          *string `json:"title,omitempty" jsonschema:"Optional human-friendly title for the file."`
	AltText        *string `json:"alt_text,omitempty" jsonschema:"Optional screen-reader description for image uploads. Sent to Slack as alt_txt."`
	SnippetType    *string `json:"snippet_type,omitempty" jsonschema:"Syntax type for code snippet uploads (for example: python, go, json)."`
	ChannelID      *string `json:"channel_id,omitempty" jsonschema:"Optional channel ID to share the file into. Omit to keep the file private to the uploader. To share into multiple channels, invoke the tool once per channel."`
	InitialComment *string `json:"initial_comment,omitempty" jsonschema:"Optional message text to post alongside the shared file."`
	ThreadTS       *string `json:"thread_ts,omitempty" jsonschema:"Optional thread timestamp to share the file as a reply in an existing thread."`
}

type getUploadURLExternalResponse struct {
	slackResponseEnvelope
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

type completeUploadFileEntry struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

func NewUploadFileTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "upload_file",
			Name:        toolNameUploadFile,
			Description: "Upload a file to Slack using the modern external upload flow (files.getUploadURLExternal + binary upload + files.completeUploadExternal). File bytes are passed as base64. Optionally shares the file into a single channel with an initial comment or thread reply; call the tool multiple times to share into more channels.",
			InputSchema: core.BuildInputSchema[uploadFileInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callUploadFile,
	}
}

func callUploadFile(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input uploadFileInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	filename, err := requireString("filename", input.Filename)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.ContentBase64) == "" {
		return fmt.Errorf("content_base64 is required")
	}
	fileBytes, err := base64.StdEncoding.DecodeString(input.ContentBase64)
	if err != nil {
		return fmt.Errorf("decode content_base64: %w", err)
	}
	if len(fileBytes) == 0 {
		return fmt.Errorf("content_base64 decoded to zero bytes")
	}

	startRequest := map[string]any{
		"filename": filename,
		"length":   len(fileBytes),
	}
	setOptionalString(startRequest, "alt_txt", input.AltText)
	setOptionalString(startRequest, "snippet_type", input.SnippetType)

	startBody, err := client.call(ctx, "files.getUploadURLExternal", startRequest, tokenPreferBot, env)
	if err != nil {
		return err
	}

	var start getUploadURLExternalResponse
	if err := json.Unmarshal(startBody, &start); err != nil {
		return fmt.Errorf("decode files.getUploadURLExternal response: %w", err)
	}
	if strings.TrimSpace(start.UploadURL) == "" || strings.TrimSpace(start.FileID) == "" {
		return fmt.Errorf("files.getUploadURLExternal returned empty upload_url or file_id")
	}

	if err := postBinary(ctx, client.httpClient, start.UploadURL, fileBytes); err != nil {
		return err
	}

	entry := completeUploadFileEntry{ID: start.FileID, Title: strings.TrimSpace(derefString(input.Title))}
	completeRequest := map[string]any{
		"files": []completeUploadFileEntry{entry},
	}
	setOptionalString(completeRequest, "channel_id", input.ChannelID)
	setOptionalString(completeRequest, "initial_comment", input.InitialComment)
	setOptionalString(completeRequest, "thread_ts", input.ThreadTS)

	body, err := client.call(ctx, "files.completeUploadExternal", completeRequest, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func postBinary(ctx context.Context, httpClient *guardian.HTTPClient, uploadURL string, data []byte) error {
	if httpClient == nil {
		return fmt.Errorf("slack HTTP client not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build slack upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload file bytes to slack: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack upload URL returned %d: %s", resp.StatusCode, string(body))
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("drain slack upload response: %w", err)
	}
	return nil
}
