package channels

func toolDefinitions() []any {
	str := func(description string) any { return map[string]any{"type": "string", "description": description} }
	boolean := map[string]any{"type": "boolean"}
	// Message sequences start at 1, so a cursor is never negative.
	sequence := map[string]any{"type": "integer", "minimum": 0}
	array := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	common := map[string]any{
		"instanceId":   str("Active owned instance ID"),
		"leaseSeconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 3600},
		"note":         str("Optional presence note"),
	}
	tools := []any{}
	add := func(name, description string, properties map[string]any, required []string) {
		if required == nil {
			required = []string{}
		}
		tools = append(tools, map[string]any{
			"name":        name,
			"description": description,
			"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
		})
	}
	add("channels_connect", "Create a durable client instance, or resume an owned instance using its secret. Instances expire without lease renewal. No broadcast is sent on resume.", map[string]any{"instanceId": common["instanceId"], "resumeToken": str("Secret returned by original connect"), "label": str("Human-readable instance label"), "note": common["note"], "leaseSeconds": common["leaseSeconds"]}, nil)
	add("channels_presence", "Inspect current presence or renew your owned instance lease and note.", common, nil)
	add("channels_dm", "Open the private conversation addressed to a recipient instance, reusing the existing one for that pair of endpoints. Only the two instance owners and authorized read-all principals can read it.", map[string]any{"instanceId": common["instanceId"], "recipientInstanceId": str("Active recipient instance ID from presence")}, []string{"instanceId", "recipientInstanceId"})
	add("channels_create_thread", "Create a public organization thread or a child inheriting its parent's private participants.", map[string]any{"instanceId": common["instanceId"], "title": str("Thread title"), "parentId": str("Optional existing parent thread ID")}, []string{"instanceId", "title"})
	add("channels_post", "Post to an accessible thread. Required idempotency key makes retries safe; retry the same key after a rate-limit refusal.", map[string]any{"instanceId": common["instanceId"], "threadId": str("Destination thread ID"), "text": str("Message text, max 32768 bytes"), "idempotencyKey": str("Stable unique key for this logical message")}, []string{"instanceId", "threadId", "text", "idempotencyKey"})
	add("channels_conversations", "Page through the organization's conversations, most recently active first. The presence snapshot carries only the first page; pass its threadsNextAfter (or this operation's nextAfter) as `after` to reach the rest.", map[string]any{"after": str("Opaque cursor from a previous page"), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}, nil)
	add("channels_read", "Read accessible messages after the last cursor for durable catchup. Persist the returned cursor. hasMore means more is already waiting; stale means retention removed history below your cursor and you should start again from the latest.", map[string]any{"threadId": str("Optional thread filter"), "cursor": sequence, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}, nil)
	add("channels_history", "Page backwards through one conversation's older messages. Pass the lowest message id you hold as the integer `before` to get the page before it. Messages come back oldest first; nextBefore is the cursor for the page before this one.", map[string]any{"threadId": str("Conversation to page"), "before": sequence, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}, []string{"threadId"})
	add("channels_follow", "Persist follow and mute settings for an accessible thread. Following a public thread is REQUIRED to be notified about it; subscribing to channels://messages alone is not enough. Muting only suppresses notifications and never hides messages from channels_read or channels_history.", map[string]any{"threadId": str("Thread ID"), "follow": boolean, "muted": boolean}, []string{"threadId"})
	add("channels_preferences", "Read or replace durable per-principal followed and muted thread lists.", map[string]any{"followed": array, "muted": array}, nil)
	add("channels_admin", "Enable or disable channels, or set a subject's connect/read-all grants. Requires channel administration permission; explicit grants override native Channels permissions, not organization membership.", map[string]any{"enabled": boolean, "subjectId": str("Exact subject to restrict"), "canConnect": boolean, "canReadAll": boolean, "remove": boolean}, nil)
	return tools
}
