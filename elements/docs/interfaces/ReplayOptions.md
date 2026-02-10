[**@gram-ai/elements v1.26.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / ReplayOptions

# Interface: ReplayOptions

## Properties

### typingSpeed?

> `optional` **typingSpeed**: `number`

Milliseconds per character when streaming text.

#### Default

```ts
15
```

***

### userMessageDelay?

> `optional` **userMessageDelay**: `number`

Milliseconds to wait before showing each user message.

#### Default

```ts
800
```

***

### assistantStartDelay?

> `optional` **assistantStartDelay**: `number`

Milliseconds to wait before the assistant starts "typing".

#### Default

```ts
400
```

***

### onComplete()?

> `optional` **onComplete**: () => `void`

Called when the full replay sequence finishes.

#### Returns

`void`
