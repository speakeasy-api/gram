[**@gram-ai/elements v1.23.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / Plugin

# Interface: Plugin

A plugin enables addition of custom rendering capabilities to the Elements library.
For example, a plugin could provide a custom renderer for a specific language such as
D3.js or Mermaid.

The general flow of a plugin is:
1. Plugin extends the system prompt with a custom prompt instructing the LLM to return code fences marked with the specified language / format
2. The LLM returns a code fence marked with the specified language / format
3. The code fence is rendered using the custom renderer

## Properties

### prompt

> **prompt**: `string`

Any prompt that the plugin may need to add to the system prompt.
Will be appended to the built-in system prompt.

#### Example

```
If the user asks for a chart, use D3 to render it.
Return only a d3 code block. The code will execute in a sandboxed environment where:
- \`d3\` is the D3 library
- \`container\` is the DOM element to render into (use \`d3.select(container)\` NOT \`d3.select('body')\`)
The code should be wrapped in a \`\`\`d3
\`\`\` block.
```

***

### language

> **language**: `string`

The language identifier for the syntax highlighter
e.g mermaid or d3

Does not need to be an official language identifier, can be any string. The important part is that the
prompt adequately instructs the LLM to return code fences marked with the specified language / format

#### Example

```
d3
```

***

### Component

> **Component**: `ComponentType`\<`SyntaxHighlighterProps`\>

The component to use for the syntax highlighter.

***

### Header?

> `optional` **Header**: `ComponentType`\<`CodeHeaderProps`\>

The component to use for the code header.
Will be rendered above the code block.

#### Default

```ts
() => null
```

***

### overrideExisting?

> `optional` **overrideExisting**: `boolean`

Whether to override existing plugins with the same language.

#### Default

```ts
false
```
