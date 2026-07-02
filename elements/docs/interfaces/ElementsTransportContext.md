[**@gram-ai/elements v1.40.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / ElementsTransportContext

# Interface: ElementsTransportContext

Live chat context handed to a [ElementsTransportFactory](../type-aliases/ElementsTransportFactory.md).

## Properties

### getChatId()

> **getChatId**: () => `string` \| `null`

The active conversation's persisted chat id, or null when the current
thread has no server-side chat yet (a brand-new, not-yet-sent thread).
Sourced from the thread-list runtime, so it stays current as the user
switches conversations.

#### Returns

`string` \| `null`

***

### adoptChatId()

> **adoptChatId**: () => (`chatId`) => `void`

Adopt a chat id assigned out-of-band (e.g. a server-minted id a consumer
transport receives on the first send). Call at the START of an async send
to capture the active conversation, then invoke the returned function with
the server's id once it's known. The closure binds to the conversation the
send originated from, so a thread switch or a parallel send on another
thread during the round-trip can't mis-associate the id.

#### Returns

> (`chatId`): `void`

##### Parameters

###### chatId

`string`

##### Returns

`void`
