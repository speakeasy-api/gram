package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSendMessage = "platform_slack_send_message"

const (
	// routeRepliesSelf delivers replies to the new message to the calling
	// assistant's current conversation.
	routeRepliesSelf = "self"

	// routeRepliesNew delivers replies to the new message as a conversation of
	// its own.
	routeRepliesNew = "new"
)

// ThreadRouter routes replies in a Slack thread to an assistant conversation.
type ThreadRouter interface {
	RouteSlackThreadToAssistant(ctx context.Context, thread bgtriggers.AssistantThread, channelID, threadTS string) error
}

type sendMessageInput struct {
	ChannelID      string       `json:"channel_id" jsonschema:"Slack conversation ID to post into."`
	Text           string       `json:"text" jsonschema:"Message text. When 'blocks' is set, this is the accessibility fallback Slack shows in notifications and clients that cannot render Block Kit."`
	ThreadTS       *string      `json:"thread_ts,omitempty" jsonschema:"Optional thread timestamp to reply in an existing thread."`
	ReplyBroadcast *bool        `json:"reply_broadcast,omitempty" jsonschema:"Broadcast a threaded reply to the channel."`
	UnfurlLinks    *bool        `json:"unfurl_links,omitempty" jsonschema:"Control Slack link unfurling for the message."`
	UnfurlMedia    *bool        `json:"unfurl_media,omitempty" jsonschema:"Control Slack media unfurling for the message."`
	Blocks         []slackBlock `json:"blocks,omitempty" jsonschema:"Optional Block Kit blocks (https://docs.slack.dev/reference/block-kit/blocks). Buttons inside an actions block deliver block_actions interactions back to this assistant on the originating thread; pick a stable action_id and a value to recognise the click."`
	RouteReplies   *string      `json:"route_replies,omitempty" jsonschema:"Only for new top-level messages; not allowed with thread_ts. 'self' delivers replies in the new message's thread to your current conversation and subscribes you to that thread. 'new' (the default) starts a separate conversation when someone replies."`
}

type postMessageResponse struct {
	Channel string `json:"channel"`
	Ts      string `json:"ts"`
}

// NewSendMessageTool returns the Slack send-message tool. router may be nil,
// in which case route_replies "self" is rejected.
func NewSendMessageTool(httpClient *guardian.HTTPClient, router ThreadRouter) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "send_message",
			Name:        toolNameSendMessage,
			Description: "Send a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN. Supports plain text and Block Kit (sections, actions, context, divider, buttons) via the optional 'blocks' parameter.",
			InputSchema: core.BuildInputSchema[sendMessageInput](
				core.WithPropertyEnum("route_replies", routeRepliesSelf, routeRepliesNew),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: sendMessageCall(router),
	}
}

func sendMessageCall(router ThreadRouter) func(context.Context, *apiClient, toolconfig.ToolCallEnv, io.Reader, io.Writer) error {
	return func(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
		return callSendMessage(ctx, client, router, env, payload, wr)
	}
}

func callSendMessage(ctx context.Context, client *apiClient, router ThreadRouter, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input sendMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	routeReplies := routeRepliesNew
	if input.RouteReplies != nil {
		if input.ThreadTS != nil && *input.ThreadTS != "" {
			return fmt.Errorf("route_replies applies only to new top-level messages; omit it when thread_ts is set")
		}
		routeReplies = *input.RouteReplies
	}
	var routeTo *bgtriggers.AssistantThread
	switch routeReplies {
	case routeRepliesNew:
	case routeRepliesSelf:
		thread, err := callerAssistantThread(ctx, router, env)
		if err != nil {
			return err
		}
		routeTo = &thread
	default:
		return fmt.Errorf("route_replies must be %q or %q", routeRepliesSelf, routeRepliesNew)
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	text, err := requireString("text", input.Text)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
		"text":    text,
	}
	setOptionalString(request, "thread_ts", input.ThreadTS)
	setOptionalBool(request, "reply_broadcast", input.ReplyBroadcast)
	setOptionalBool(request, "unfurl_links", input.UnfurlLinks)
	setOptionalBool(request, "unfurl_media", input.UnfurlMedia)
	if len(input.Blocks) > 0 {
		request["blocks"] = input.Blocks
	}

	body, err := client.Call(ctx, "chat.postMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}

	if routeTo != nil {
		// The message is already posted, so a routing failure is reported
		// alongside Slack's response instead of failing the call, which a
		// retry would turn into a duplicate message.
		body = routeRepliesToAssistant(ctx, router, *routeTo, body)
	}
	return writeResponse(wr, body)
}

// routeRepliesToAssistant routes replies to a posted message to the calling
// assistant's conversation and returns the Slack response, with a
// route_replies_error field when routing failed.
func routeRepliesToAssistant(ctx context.Context, router ThreadRouter, thread bgtriggers.AssistantThread, body []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body
	}
	var posted postMessageResponse
	err := json.Unmarshal(body, &posted)
	if err == nil {
		err = router.RouteSlackThreadToAssistant(ctx, thread, posted.Channel, posted.Ts)
	}
	if err == nil {
		return body
	}
	message, encodeErr := json.Marshal("message posted, but replies will start a separate conversation: " + err.Error())
	if encodeErr != nil {
		return body
	}
	fields["route_replies_error"] = message
	out, encodeErr := json.Marshal(fields)
	if encodeErr != nil {
		return body
	}
	return out
}

func callerAssistantThread(ctx context.Context, router ThreadRouter, env toolconfig.ToolCallEnv) (bgtriggers.AssistantThread, error) {
	if router == nil {
		return bgtriggers.AssistantThread{}, fmt.Errorf("route_replies %q is not available here", routeRepliesSelf)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return bgtriggers.AssistantThread{}, fmt.Errorf("route_replies %q requires a project", routeRepliesSelf)
	}
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return bgtriggers.AssistantThread{}, fmt.Errorf("route_replies %q is only available to assistants", routeRepliesSelf)
	}
	return bgtriggers.AssistantThread{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: principal.AssistantID,
		ThreadID:    principal.ThreadID,
		ChatID:      env.GramChatID,
	}, nil
}
