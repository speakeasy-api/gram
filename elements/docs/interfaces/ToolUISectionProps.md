[**@gram-ai/elements v1.40.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / ToolUISectionProps

# Interface: ToolUISectionProps

## Properties

### title

> **title**: `string`

Section title

***

### content

> **content**: `string` \| `Record`\<`string`, `unknown`\> \| \{ `content`: [`ContentItem`](../type-aliases/ContentItem.md)[]; \}

Content to display - string or object (will be JSON stringified)

***

### defaultExpanded?

> `optional` **defaultExpanded**: `boolean`

Whether section starts expanded

***

### highlightSyntax?

> `optional` **highlightSyntax**: `boolean`

Enable syntax highlighting

***

### language?

> `optional` **language**: `BundledLanguage`

Language hint for syntax highlighting

***

### highlight?

> `optional` **highlight**: [`SectionHighlight`](SectionHighlight.md)

Flagged substrings — renders a navigable highlighted view + header icon.
