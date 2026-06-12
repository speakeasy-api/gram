package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSetThreadStatus = "platform_slack_set_thread_status"

type setThreadStatusInput struct {
	ChannelID       string   `json:"channel_id" jsonschema:"Channel containing the thread."`
	ThreadTS        string   `json:"thread_ts" jsonschema:"Timestamp of the thread's parent message."`
	Status          string   `json:"status" jsonschema:"The overarching task being worked on. Slack renders it mid-sentence after the app's name ('<App Name> <status>'), so phrase it like 'is ordering pizza...'. Clears automatically when the app posts a reply, or after a two-minute timeout. An empty string clears the indicator immediately (optional — the timeout is the backstop)."`
	LoadingMessages []string `json:"loading_messages,omitempty" jsonschema:"A single message describing the current step, e.g. ['Calling DoorDash...']. Always pass exactly one — Slack rotates through multiple, which is distracting. Defaults to the status text when omitted."`
}

type setThreadStatusOutput struct {
	Ok bool `json:"ok"`
}

func NewSetThreadStatusTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "set_thread_status",
			Name:        toolNameSetThreadStatus,
			Description: "Show a native AI loading indicator on a Slack thread via assistant.threads.setStatus, using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN. Slack clears the status automatically once the app posts its reply (or after a two-minute timeout). Accepts the chat:write scope.\n\nAGENTS: Use this tool to communicate progress while working on a request. Set 'status' to the overarching task; Slack renders it mid-sentence after the app's name, so phrase it like 'is ordering pizza...'. Set 'loading_messages' to a single message describing the current step, e.g. ['Calling DoorDash...'] — never more than one, rotation is distracting. When calling other tools, pair them with a call to this tool that updates 'loading_messages' to reflect the new step, whenever it makes sense. An empty 'status' clears the indicator immediately; this is optional — Slack clears stale indicators on its own after two minutes — and only worth doing if you set a status earlier in the turn and are ending it without posting a reply.",
			InputSchema: core.BuildInputSchema[setThreadStatusInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSetThreadStatus,
	}
}

func callSetThreadStatus(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input setThreadStatusInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	threadTS, err := requireString("thread_ts", input.ThreadTS)
	if err != nil {
		return err
	}
	// An empty status is a deliberate clear (Slack removes the indicator),
	// used when the assistant ends a turn without posting a reply.
	request := map[string]any{
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"status":     input.Status,
	}
	if input.Status != "" {
		// The indicator must stay a single static phrase: clamp extra messages
		// (Slack rotates through multiples) and pin it to the status text when
		// none are given (Slack rotates its own defaults otherwise).
		loadingMessages := input.LoadingMessages
		if len(loadingMessages) > 1 {
			loadingMessages = loadingMessages[:1]
		}
		if len(loadingMessages) == 0 {
			loadingMessages = []string{input.Status}
		}
		// Slack expects the array param as a JSON-encoded string in a
		// form-encoded request; pass it pre-marshaled so encodeFormValue
		// doesn't comma-join it.
		encodedMessages, err := json.Marshal(loadingMessages)
		if err != nil {
			return fmt.Errorf("encode loading_messages: %w", err)
		}
		request["loading_messages"] = string(encodedMessages)
	}

	body, err := client.Call(ctx, "assistant.threads.setStatus", request, tokenPreferBot, env)
	if err != nil {
		return err
	}

	var output setThreadStatusOutput
	if err := json.Unmarshal(body, &output); err != nil {
		return fmt.Errorf("decode set_thread_status response: %w", err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("encode set_thread_status response: %w", err)
	}
	return writeResponse(wr, encoded)
}
