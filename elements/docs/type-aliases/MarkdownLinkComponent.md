[**@gram-ai/elements v1.41.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / MarkdownLinkComponent

# Type Alias: MarkdownLinkComponent

> **MarkdownLinkComponent** = `ComponentType`\<`ComponentPropsWithoutRef`\<`"a"`\>\>

An `<a>`-shaped component the host supplies to render links inside assistant
markdown with its own design system (e.g. a Moonshine `Link`). Receives the
already-resolved `href`/`target`/`rel`; Elements falls back to a plain `<a>`
when this is not provided.
