package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	defaultSlackAPIBaseURL = "https://slack.com/api"
	//nolint:gosec // environment variable name, not a credential
	slackBotTokenEnvVar  = "SLACK_BOT_TOKEN"
	slackUserTokenEnvVar = "SLACK_USER_TOKEN"
	slackTokenEnvVar     = "SLACK_TOKEN"
	sourceSlack          = "slack"

	toolNameReadChannelMessages    = "platform_slack_read_channel_messages"
	toolNameReadThreadMessages     = "platform_slack_read_thread_messages"
	toolNameReadUserProfile        = "platform_slack_read_user_profile"
	toolNameSearchChannels         = "platform_slack_search_channels"
	toolNameSearchMessagesAndFiles = "platform_slack_search_messages_and_files"
	toolNameSearchUsers            = "platform_slack_search_users"
	toolNameScheduleMessage        = "platform_slack_schedule_message"
	toolNameSendMessage            = "platform_slack_send_message"
)

var defaultSearchChannelTypes = []string{"public_channel"}

type slackTokenKind int

const (
	tokenPreferBot slackTokenKind = iota
	tokenRequireUser
)

type apiClient struct {
	baseURL    string
	httpClient *guardian.HTTPClient
}

type slackTool struct {
	descriptor core.ToolDescriptor
	client     *apiClient
	callFn     func(context.Context, *apiClient, toolconfig.ToolCallEnv, io.Reader, io.Writer) error
}

type slackResponseEnvelope struct {
	Ok               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	Warning          string `json:"warning,omitempty"`
	ResponseMetadata *struct {
		Messages []string `json:"messages,omitempty"`
	} `json:"response_metadata,omitempty"`
}

type readChannelMessagesInput struct {
	ChannelID          string  `json:"channel_id" jsonschema:"Slack conversation ID to read."`
	Cursor             *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Latest             *string `json:"latest,omitempty" jsonschema:"Only messages before this Slack timestamp are returned."`
	Oldest             *string `json:"oldest,omitempty" jsonschema:"Only messages after this Slack timestamp are returned."`
	Inclusive          *bool   `json:"inclusive,omitempty" jsonschema:"Include messages matching oldest or latest timestamps."`
	Limit              *int    `json:"limit,omitempty" jsonschema:"Maximum number of messages to return. Slack allows up to 1000."`
	IncludeAllMetadata *bool   `json:"include_all_metadata,omitempty" jsonschema:"Include all message metadata in the response."`
}

type readThreadMessagesInput struct {
	ChannelID          string  `json:"channel_id" jsonschema:"Slack conversation ID containing the thread."`
	ThreadTS           string  `json:"thread_ts" jsonschema:"Slack timestamp for the parent message or any reply in the thread."`
	Cursor             *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Latest             *string `json:"latest,omitempty" jsonschema:"Only messages before this Slack timestamp are returned."`
	Oldest             *string `json:"oldest,omitempty" jsonschema:"Only messages after this Slack timestamp are returned."`
	Inclusive          *bool   `json:"inclusive,omitempty" jsonschema:"Include messages matching oldest or latest timestamps."`
	Limit              *int    `json:"limit,omitempty" jsonschema:"Maximum number of messages to return. Slack allows up to 1000."`
	IncludeAllMetadata *bool   `json:"include_all_metadata,omitempty" jsonschema:"Include all message metadata in the response."`
}

type readUserProfileInput struct {
	UserID        string `json:"user_id" jsonschema:"Slack user ID to inspect."`
	IncludeLocale *bool  `json:"include_locale,omitempty" jsonschema:"Include locale in the returned profile data when available."`
}

type searchChannelsInput struct {
	Query           *string  `json:"query,omitempty" jsonschema:"Optional case-insensitive substring filter applied to channel names."`
	ChannelTypes    []string `json:"channel_types,omitempty" jsonschema:"Conversation types to include. Defaults to public_channel."`
	Cursor          *string  `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Limit           *int     `json:"limit,omitempty" jsonschema:"Maximum number of channels to fetch per page. Slack allows up to 1000."`
	ExcludeArchived *bool    `json:"exclude_archived,omitempty" jsonschema:"Exclude archived channels from the response."`
}

type searchMessagesAndFilesInput struct {
	Query     string  `json:"query" jsonschema:"Search query. Supports Slack modifiers like in:#channel, from:@user, before:2024-01-01, has:link."`
	Page      *int    `json:"page,omitempty" jsonschema:"1-indexed page number to fetch. Slack returns paging metadata in the response."`
	Limit     *int    `json:"limit,omitempty" jsonschema:"Maximum number of results per page. Slack allows up to 100."`
	Highlight *bool   `json:"highlight,omitempty" jsonschema:"Highlight matching text in results."`
	Sort      *string `json:"sort,omitempty" jsonschema:"Sort field. Slack accepts score or timestamp."`
	SortDir   *string `json:"sort_dir,omitempty" jsonschema:"Sort direction. Slack accepts asc or desc."`
}

type searchUsersInput struct {
	Query         *string `json:"query,omitempty" jsonschema:"Optional case-insensitive substring filter applied to user name, display name, real name, and email."`
	Cursor        *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Limit         *int    `json:"limit,omitempty" jsonschema:"Maximum number of users to fetch per page. Slack allows up to 1000."`
	IncludeLocale *bool   `json:"include_locale,omitempty" jsonschema:"Include locale in the returned user data when available."`
}

type scheduleMessageInput struct {
	ChannelID string  `json:"channel_id" jsonschema:"Slack conversation ID to post into."`
	Text      string  `json:"text" jsonschema:"Message text to schedule."`
	PostAt    int64   `json:"post_at" jsonschema:"UNIX timestamp when Slack should send the message."`
	ThreadTS  *string `json:"thread_ts,omitempty" jsonschema:"Optional thread timestamp to schedule a threaded reply."`
}

type sendMessageInput struct {
	ChannelID      string  `json:"channel_id" jsonschema:"Slack conversation ID to post into."`
	Text           string  `json:"text" jsonschema:"Message text to send."`
	ThreadTS       *string `json:"thread_ts,omitempty" jsonschema:"Optional thread timestamp to reply in an existing thread."`
	ReplyBroadcast *bool   `json:"reply_broadcast,omitempty" jsonschema:"Broadcast a threaded reply to the channel."`
	UnfurlLinks    *bool   `json:"unfurl_links,omitempty" jsonschema:"Control Slack link unfurling for the message."`
	UnfurlMedia    *bool   `json:"unfurl_media,omitempty" jsonschema:"Control Slack media unfurling for the message."`
}

func NewReadChannelMessagesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "read_channel_messages",
			Name:        toolNameReadChannelMessages,
			Description: "Read messages from a Slack conversation using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[readChannelMessagesInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callReadChannelMessages,
	}
}

func NewReadThreadMessagesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "read_thread_messages",
			Name:        toolNameReadThreadMessages,
			Description: "Read a Slack thread using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[readThreadMessagesInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callReadThreadMessages,
	}
}

func NewReadUserProfileTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "read_user_profile",
			Name:        toolNameReadUserProfile,
			Description: "Read a Slack user profile using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[readUserProfileInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callReadUserProfile,
	}
}

func NewSearchChannelsTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "search_channels",
			Name:        toolNameSearchChannels,
			Description: "List Slack conversations via conversations.list using the server's bot or user token. Optionally filters channels client-side by a name substring.",
			InputSchema: core.BuildInputSchema[searchChannelsInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSearchChannels,
	}
}

func NewSearchMessagesAndFilesTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "search_messages_and_files",
			Name:        toolNameSearchMessagesAndFiles,
			Description: "Search Slack messages and files via search.all. Requires a user token with search:read (SLACK_USER_TOKEN or SLACK_TOKEN).",
			InputSchema: core.BuildInputSchema[searchMessagesAndFilesInput](
				core.WithPropertyNumberRange("limit", 1, 100),
				core.WithPropertyEnum("sort", "score", "timestamp"),
				core.WithPropertyEnum("sort_dir", "asc", "desc"),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSearchMessagesAndFiles,
	}
}

func NewSearchUsersTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "search_users",
			Name:        toolNameSearchUsers,
			Description: "List Slack workspace users via users.list using the server's bot or user token. Optionally filters results client-side by a name substring.",
			InputSchema: core.BuildInputSchema[searchUsersInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSearchUsers,
	}
}

func NewScheduleMessageTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "schedule_message",
			Name:        toolNameScheduleMessage,
			Description: "Schedule a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[scheduleMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callScheduleMessage,
	}
}

func NewSendMessageTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "send_message",
			Name:        toolNameSendMessage,
			Description: "Send a Slack message using the server's Slack token from SLACK_BOT_TOKEN or SLACK_TOKEN.",
			InputSchema: core.BuildInputSchema[sendMessageInput](),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSendMessage,
	}
}

func (t *slackTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *slackTool) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.client == nil {
		return fmt.Errorf("slack client not configured")
	}
	return t.callFn(ctx, t.client, env, payload, wr)
}

func newAPIClient(baseURL string, httpClient *guardian.HTTPClient) *apiClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultSlackAPIBaseURL
	}
	return &apiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *apiClient) call(ctx context.Context, method string, payload map[string]any, kind slackTokenKind, env toolconfig.ToolCallEnv) ([]byte, error) {
	token, err := c.token(kind, env)
	if err != nil {
		return nil, err
	}
	if c.httpClient == nil {
		return nil, fmt.Errorf("slack HTTP client not configured")
	}

	form, err := encodeFormPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("encode slack payload for %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build slack request for %s: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call slack %s: %w", method, err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read slack response for %s: %w", method, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack %s returned %d: %s", method, resp.StatusCode, string(bodyBytes))
	}

	var envelope slackResponseEnvelope
	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		return nil, fmt.Errorf("decode slack response for %s: %w", method, err)
	}
	if !envelope.Ok {
		return nil, fmt.Errorf("slack %s: %s", method, slackErrorDetails(envelope))
	}

	return bodyBytes, nil
}

func (c *apiClient) token(kind slackTokenKind, env toolconfig.ToolCallEnv) (string, error) {
	var candidates []string
	switch kind {
	case tokenRequireUser:
		candidates = []string{slackUserTokenEnvVar, slackTokenEnvVar}
	default:
		candidates = []string{slackBotTokenEnvVar, slackUserTokenEnvVar, slackTokenEnvVar}
	}
	merged := env.Merged()
	for _, key := range candidates {
		if value := strings.TrimSpace(merged.Get(key)); value != "" {
			return value, nil
		}
	}
	if kind == tokenRequireUser {
		return "", fmt.Errorf("slack user token not configured: expected %s or %s with search:read scope", slackUserTokenEnvVar, slackTokenEnvVar)
	}
	return "", fmt.Errorf("slack token not configured: expected %s, %s, or %s", slackBotTokenEnvVar, slackUserTokenEnvVar, slackTokenEnvVar)
}

func slackErrorDetails(resp slackResponseEnvelope) string {
	parts := make([]string, 0, 3)
	if resp.Error != "" {
		parts = append(parts, resp.Error)
	}
	if resp.Warning != "" {
		parts = append(parts, "warning="+resp.Warning)
	}
	if resp.ResponseMetadata != nil && len(resp.ResponseMetadata.Messages) > 0 {
		parts = append(parts, strings.Join(resp.ResponseMetadata.Messages, "; "))
	}
	if len(parts) == 0 {
		return "request failed"
	}
	return strings.Join(parts, " | ")
}

func decodePayload(payload io.Reader, target any) error {
	bodyBytes, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(bodyBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(bodyBytes, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func writeResponse(wr io.Writer, body []byte) error {
	if _, err := wr.Write(body); err != nil {
		return fmt.Errorf("write response body: %w", err)
	}
	return nil
}

func requireString(name string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}

func defaultChannelTypes(channelTypes []string) []string {
	if len(channelTypes) == 0 {
		return append([]string(nil), defaultSearchChannelTypes...)
	}
	return append([]string(nil), channelTypes...)
}

func callReadChannelMessages(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input readChannelMessagesInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"channel": channelID,
	}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalString(request, "latest", input.Latest)
	setOptionalString(request, "oldest", input.Oldest)
	setOptionalBool(request, "inclusive", input.Inclusive)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "include_all_metadata", input.IncludeAllMetadata)

	body, err := client.call(ctx, "conversations.history", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func callReadThreadMessages(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input readThreadMessagesInput
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

	request := map[string]any{
		"channel": channelID,
		"ts":      threadTS,
	}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalString(request, "latest", input.Latest)
	setOptionalString(request, "oldest", input.Oldest)
	setOptionalBool(request, "inclusive", input.Inclusive)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "include_all_metadata", input.IncludeAllMetadata)

	body, err := client.call(ctx, "conversations.replies", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func callReadUserProfile(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input readUserProfileInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	userID, err := requireString("user_id", input.UserID)
	if err != nil {
		return err
	}

	request := map[string]any{
		"user": userID,
	}
	setOptionalBool(request, "include_locale", input.IncludeLocale)

	body, err := client.call(ctx, "users.info", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func callSearchChannels(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input searchChannelsInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{
		"types": strings.Join(defaultChannelTypes(input.ChannelTypes), ","),
	}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "exclude_archived", input.ExcludeArchived)

	body, err := client.call(ctx, "conversations.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}

	filtered, err := filterListResponse(body, "channels", channelMatchesQuery(input.Query))
	if err != nil {
		return err
	}
	return writeResponse(wr, filtered)
}

func callSearchMessagesAndFiles(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input searchMessagesAndFilesInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	query, err := requireString("query", input.Query)
	if err != nil {
		return err
	}

	request := map[string]any{
		"query": query,
	}
	setOptionalInt(request, "page", input.Page)
	setOptionalInt(request, "count", input.Limit)
	setOptionalBool(request, "highlight", input.Highlight)
	setOptionalString(request, "sort", input.Sort)
	setOptionalString(request, "sort_dir", input.SortDir)

	body, err := client.call(ctx, "search.all", request, tokenRequireUser, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func callSearchUsers(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input searchUsersInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "include_locale", input.IncludeLocale)

	body, err := client.call(ctx, "users.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}

	filtered, err := filterListResponse(body, "members", userMatchesQuery(input.Query))
	if err != nil {
		return err
	}
	return writeResponse(wr, filtered)
}

func callScheduleMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input scheduleMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	channelID, err := requireString("channel_id", input.ChannelID)
	if err != nil {
		return err
	}
	text, err := requireString("text", input.Text)
	if err != nil {
		return err
	}
	if input.PostAt <= 0 {
		return fmt.Errorf("post_at is required")
	}

	request := map[string]any{
		"channel": channelID,
		"text":    text,
		"post_at": input.PostAt,
	}
	setOptionalString(request, "thread_ts", input.ThreadTS)

	body, err := client.call(ctx, "chat.scheduleMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func callSendMessage(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input sendMessageInput
	if err := decodePayload(payload, &input); err != nil {
		return err
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

	body, err := client.call(ctx, "chat.postMessage", request, tokenPreferBot, env)
	if err != nil {
		return err
	}
	return writeResponse(wr, body)
}

func filterListResponse(body []byte, field string, predicate func(map[string]any) bool) ([]byte, error) {
	if predicate == nil {
		return body, nil
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode slack list response: %w", err)
	}
	raw, ok := response[field]
	if !ok {
		return body, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return body, nil
	}
	filtered := make([]any, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if predicate(entry) {
			filtered = append(filtered, entry)
		}
	}
	response[field] = filtered
	out, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode filtered slack list response: %w", err)
	}
	return out, nil
}

func channelMatchesQuery(query *string) func(map[string]any) bool {
	needle := strings.ToLower(strings.TrimSpace(derefString(query)))
	if needle == "" {
		return nil
	}
	return func(entry map[string]any) bool {
		return stringFieldContains(entry, needle, "name", "name_normalized")
	}
}

func userMatchesQuery(query *string) func(map[string]any) bool {
	needle := strings.ToLower(strings.TrimSpace(derefString(query)))
	if needle == "" {
		return nil
	}
	return func(entry map[string]any) bool {
		if stringFieldContains(entry, needle, "name", "real_name") {
			return true
		}
		profile, ok := entry["profile"].(map[string]any)
		if !ok {
			return false
		}
		return stringFieldContains(profile, needle, "display_name", "display_name_normalized", "real_name", "real_name_normalized", "email")
	}
}

func stringFieldContains(entry map[string]any, needle string, keys ...string) bool {
	for _, key := range keys {
		if value, ok := entry[key].(string); ok && value != "" {
			if strings.Contains(strings.ToLower(value), needle) {
				return true
			}
		}
	}
	return false
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func encodeFormPayload(payload map[string]any) (url.Values, error) {
	form := url.Values{}
	for key, value := range payload {
		encoded, err := encodeFormValue(value)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", key, err)
		}
		form.Set(key, encoded)
	}
	return form, nil
}

func encodeFormValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case []string:
		return strings.Join(v, ","), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("marshal value: %w", err)
		}
		return string(data), nil
	}
}

func setOptionalString(target map[string]any, key string, value *string) {
	if value != nil && strings.TrimSpace(*value) != "" {
		target[key] = strings.TrimSpace(*value)
	}
}

func setOptionalBool(target map[string]any, key string, value *bool) {
	if value != nil {
		target[key] = *value
	}
}

func setOptionalInt(target map[string]any, key string, value *int) {
	if value != nil {
		target[key] = *value
	}
}

func slackToolAnnotations(readOnly, destructive, idempotent, openWorld bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
