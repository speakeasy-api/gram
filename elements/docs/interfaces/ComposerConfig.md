[**@gram-ai/elements v1.20.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / ComposerConfig

# Interface: ComposerConfig

## Properties

### placeholder?

> `optional` **placeholder**: `string`

The placeholder text for the composer input.

#### Default

```ts
'Send a message...'
```

***

### attachments?

> `optional` **attachments**: `boolean` \| [`AttachmentsConfig`](AttachmentsConfig.md)

Configuration for file attachments in the composer.
Set to `false` to disable attachments entirely.
Set to `true` for default attachment behavior.
Or provide an object for fine-grained control.

#### Default

```ts
true
```
