[**@gram-ai/elements v1.30.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / MessageContent

# Variable: MessageContent

> `const` **MessageContent**: `FC`\<[`MessageContentProps`](../interfaces/MessageContentProps.md)\>

Standalone renderer for stored chat message content. Recognises the same
`chart` and `ui` fenced code blocks that the live `<Chat />` component
renders as widgets, but works without an `ElementsProvider`, MCP client,
auth session, or assistant-ui runtime.

Use in static viewers (agent session detail panel, replay, share) so a
stored bar chart appears as a chart instead of as raw JSON. Plain markdown
formatting is intentionally not applied — text segments render as
preformatted text.
