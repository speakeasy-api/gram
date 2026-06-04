[**@gram-ai/elements v1.33.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / convertGramMessagesToExported

# Function: convertGramMessagesToExported()

> **convertGramMessagesToExported**(`messages`): `ExportedMessageRepository`

Converts an array of Gram ChatMessages to an ExportedMessageRepository.
Creates parent-child relationships based on message order.

Note: system, developer, and tool messages are filtered out. assistant-ui's
exported format only models user/assistant turns; system/developer rows are
pre-prompt instructions the UI doesn't render, and tool rows are folded into
the preceding assistant message as `tool-call` parts via `tool_calls`.

## Parameters

### messages

[`GramChatMessage`](../interfaces/GramChatMessage.md)[]

## Returns

`ExportedMessageRepository`
