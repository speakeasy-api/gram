package googlechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// Resource segments deliberately exclude path escapes, query delimiters, and
// filter syntax. Message IDs may include dots and custom client- IDs.
var spacePattern = regexp.MustCompile(`^spaces/[A-Za-z0-9_-]+$`)
var messagePattern = regexp.MustCompile(`^spaces/[A-Za-z0-9_-]+/messages/[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)
var threadPattern = regexp.MustCompile(`^spaces/[A-Za-z0-9_-]+/threads/[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

type sendInput struct {
	Space     string `json:"space" jsonschema:"Space resource name: spaces/{space}. The app must be a member."`
	Text      string `json:"text" jsonschema:"Message text (maximum 30000 UTF-8 bytes)."`
	RequestID string `json:"request_id" jsonschema:"Required UUID idempotency key. Generate once per intended message and reuse unchanged on retries; never reuse for another message."`
}
type replyInput struct {
	Thread    string `json:"thread" jsonschema:"Existing thread in a named space: spaces/{space}/threads/{thread}. For direct messages use send_message instead. Fails rather than creating a new thread when not found."`
	Text      string `json:"text" jsonschema:"Reply text (maximum 30000 UTF-8 bytes)."`
	RequestID string `json:"request_id" jsonschema:"Required UUID idempotency key. Generate once per intended reply and reuse unchanged on retries."`
}
type messageInput struct {
	Name string `json:"name" jsonschema:"Message resource name: spaces/{space}/messages/{message}."`
}
type updateInput struct {
	Name string `json:"name" jsonschema:"Resource name of a message created by this Chat app: spaces/{space}/messages/{message}."`
	Text string `json:"text" jsonschema:"Replacement message text (maximum 30000 UTF-8 bytes). Only text is updated."`
}
type paginationInput struct {
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Page size from 1 to 100; defaults to 100. One page is returned per call."`
	PageToken string `json:"page_token,omitempty" jsonschema:"Opaque nextPageToken from the previous response. Keep all other parameters unchanged."`
}
type listMessagesInput struct {
	Space     string `json:"space" jsonschema:"Space resource name: spaces/{space}. The app must be a member."`
	Thread    string `json:"thread,omitempty" jsonschema:"Optional thread resource name within the same space, to limit results to one thread."`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Page size from 1 to 100; defaults to 100. One page is returned per call."`
	PageToken string `json:"page_token,omitempty" jsonschema:"Opaque nextPageToken from the previous response; retain the same space and thread."`
}

type chatTool struct {
	descriptor core.ToolDescriptor
	client     *client
}

func (t *chatTool) Descriptor() core.ToolDescriptor { return t.descriptor }
func newTool(h *guardian.HTTPClient, operation, description string, schema []byte, readOnly, destructive bool) core.PlatformToolExecutor {
	idempotent, openWorld := true, true
	return &chatTool{client: newClient(h), descriptor: core.ToolDescriptor{
		SourceSlug: "googlechat", HandlerName: operation, Name: "platform_googlechat_" + operation,
		Description: description + " Uses the customer-managed Chat app service account in GOOGLE_CHAT_SERVICE_ACCOUNT_JSON.", InputSchema: schema, Managed: true, Variables: nil, OwnerKind: nil, OwnerID: nil,
		Annotations: &types.ToolAnnotations{Title: nil, ReadOnlyHint: &readOnly, DestructiveHint: &destructive, IdempotentHint: &idempotent, OpenWorldHint: &openWorld},
	}}
}
func NewSendMessageTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "send_message", "Send a new Google Chat message with chat.bot app auth. Supply a stable request_id for retry safety.", core.BuildInputSchema[sendInput](core.WithPropertyFormat("request_id", "uuid")), false, false)
}
func NewReplyToThreadTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "reply_to_thread", "Reply to an existing Google Chat thread in a named space with chat.bot app auth. For direct messages use send_message instead. Never falls back to creating a new thread. Supply a stable request_id.", core.BuildInputSchema[replyInput](core.WithPropertyFormat("request_id", "uuid")), false, false)
}
func NewUpdateMessageTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "update_message", "Replace the text of a message created by this Chat app using chat.bot. Cannot edit human or other apps' messages; Google enforces ownership. Does not create missing messages.", core.BuildInputSchema[updateInput](), false, true)
}
func NewDeleteMessageTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "delete_message", "Delete a message created by this Chat app using chat.bot. Cannot delete human or other apps' messages. Does not force-delete threaded replies (user-auth only); Google enforces ownership and deletion restrictions. A repeated delete can return 404.", core.BuildInputSchema[messageInput](), false, true)
}
func NewListSpacesTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "list_spaces", "List one page of spaces the Chat app belongs to using chat.bot. Follow nextPageToken for more results.", core.BuildInputSchema[paginationInput](core.WithPropertyNumberRange("page_size", 1, 100)), true, false)
}
func NewGetMessageTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "get_message", "Read a public Google Chat message, including human messages, using chat.app.messages.readonly. Requires one-time Workspace administrator approval for that app scope. Does not expose private messages.", core.BuildInputSchema[messageInput](), true, false)
}
func NewListMessagesTool(h *guardian.HTTPClient) core.PlatformToolExecutor {
	return newTool(h, "list_messages", "Read one page of public Google Chat messages, including human messages, optionally within one thread. Uses chat.app.messages.readonly with one-time Workspace administrator approval. Private and deleted messages are excluded. Follow nextPageToken for more results.", core.BuildInputSchema[listMessagesInput](core.WithPropertyNumberRange("page_size", 1, 100)), true, false)
}

func decode(payload io.Reader, target any) error {
	d := json.NewDecoder(io.LimitReader(payload, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return errors.New("invalid Google Chat tool input")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("invalid Google Chat tool input")
	}
	return nil
}
func validText(text string) bool { return strings.TrimSpace(text) != "" && len(text) <= 30000 }
func pagination(q url.Values, size int, token string) error {
	if size == 0 {
		size = 100
	}
	if size < 1 || size > 100 || len(token) > 8192 {
		return errors.New("page_size must be 1..100 and page_token at most 8192 bytes")
	}
	q.Set("pageSize", fmt.Sprint(size))
	if token != "" {
		q.Set("pageToken", token)
	}
	return nil
}
func (t *chatTool) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	operation := t.descriptor.HandlerName
	method, scope, resource := http.MethodGet, botScope, ""
	q := url.Values{}
	var body any
	switch operation {
	case "send_message", "reply_to_thread":
		var space, thread, text, requestID string
		if operation == "send_message" {
			var in sendInput
			if err := decode(payload, &in); err != nil {
				return err
			}
			space, text, requestID = in.Space, in.Text, in.RequestID
			if !spacePattern.MatchString(space) {
				return errors.New("invalid space resource name")
			}
		} else {
			var in replyInput
			if err := decode(payload, &in); err != nil {
				return err
			}
			thread, text, requestID = in.Thread, in.Text, in.RequestID
			if !threadPattern.MatchString(thread) {
				return errors.New("invalid thread resource name")
			}
			space = strings.Split(thread, "/threads/")[0]
		}
		if !validText(text) {
			return errors.New("text must contain 1..30000 UTF-8 bytes")
		}
		if _, err := uuid.Parse(requestID); err != nil {
			return errors.New("request_id must be a UUID reused unchanged for retries")
		}
		message := map[string]any{"text": text}
		if thread != "" {
			message["thread"] = map[string]string{"name": thread}
			q.Set("messageReplyOption", "REPLY_MESSAGE_OR_FAIL")
		}
		q.Set("requestId", requestID)
		method, resource, body = http.MethodPost, space+"/messages", message
	case "update_message":
		var in updateInput
		if err := decode(payload, &in); err != nil {
			return err
		}
		if !messagePattern.MatchString(in.Name) {
			return errors.New("invalid message resource name")
		}
		if !validText(in.Text) {
			return errors.New("text must contain 1..30000 UTF-8 bytes")
		}
		method, resource, body = http.MethodPatch, in.Name, map[string]string{"name": in.Name, "text": in.Text}
		q.Set("updateMask", "text")
	case "get_message", "delete_message":
		var in messageInput
		if err := decode(payload, &in); err != nil {
			return err
		}
		if !messagePattern.MatchString(in.Name) {
			return errors.New("invalid message resource name")
		}
		resource = in.Name
		if operation == "delete_message" {
			method = http.MethodDelete
		} else {
			scope = historyScope
		}
	case "list_spaces":
		var in paginationInput
		if err := decode(payload, &in); err != nil {
			return err
		}
		if err := pagination(q, in.PageSize, in.PageToken); err != nil {
			return err
		}
		resource = "spaces"
	case "list_messages":
		var in listMessagesInput
		if err := decode(payload, &in); err != nil {
			return err
		}
		if !spacePattern.MatchString(in.Space) {
			return errors.New("invalid space resource name")
		}
		if in.Thread != "" {
			if !threadPattern.MatchString(in.Thread) || !strings.HasPrefix(in.Thread, in.Space+"/threads/") {
				return errors.New("thread must be a resource in the requested space")
			}
			q.Set("filter", "thread.name = "+in.Thread)
		}
		if err := pagination(q, in.PageSize, in.PageToken); err != nil {
			return err
		}
		resource, scope = in.Space+"/messages", historyScope
	default:
		return errors.New("unknown Google Chat tool")
	}
	data, err := t.client.call(ctx, env, scope, method, resource, q, body)
	if err != nil {
		return err
	}
	if _, err := wr.Write(data); err != nil {
		return fmt.Errorf("write Google Chat response: %w", err)
	}
	return nil
}
