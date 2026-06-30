[**@gram-ai/elements v1.39.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / ActiveChatTitle

# Function: ActiveChatTitle()

> **ActiveChatTitle**(`__namedParameters`): `Element`

Inline-editable title for the active conversation, intended for a chat
header. Reads the active thread's title from the assistant-ui runtime and
saves edits through `threadListItem().rename`, which optimistically updates
the runtime and calls the Gram thread-list adapter (→ chat.generateTitle).

Renaming requires a persisted thread (a remote id). A brand-new conversation
only has a local id until its first message, so the title renders as a
read-only "New Chat" until then. Clearing the title (saving empty) resets it
to automatic, session-context naming.

Must be rendered inside an Elements runtime provider.

## Parameters

### \_\_namedParameters

`ActiveChatTitleProps`

## Returns

`Element`
