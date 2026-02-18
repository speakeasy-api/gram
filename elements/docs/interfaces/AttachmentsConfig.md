[**@gram-ai/elements v1.27.1**](../README.md)

***

[@gram-ai/elements](../globals.md) / AttachmentsConfig

# Interface: AttachmentsConfig

AttachmentsConfig provides fine-grained control over file attachments.

Note: not yet implemented. Attachments are not supported yet.

## Properties

### accept?

> `optional` **accept**: `string`[]

Accepted file types. Can be MIME types or file extensions.

#### Example

```ts
['image/*', '.pdf', '.docx']
```

***

### maxCount?

> `optional` **maxCount**: `number`

Maximum number of files that can be attached at once.

#### Default

```ts
10
```

***

### maxSize?

> `optional` **maxSize**: `number`

Maximum file size in bytes.

#### Default

```ts
104857600 (100MB)
```
