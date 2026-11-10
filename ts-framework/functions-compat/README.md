# @gram-ai/functions

> [!NOTE]
> This package is deprecated. It is now published as
> [`@speakeasy-api/functions`](https://www.npmjs.com/package/@speakeasy-api/functions).

`@gram-ai/functions` re-exports `@speakeasy-api/functions` at the same
version, including the `/mcp` and `/build` subpaths and the deprecated `gf`
command, so existing projects keep working without changes.

To migrate, replace the dependency and the imports:

```diff
- import { Gram } from "@gram-ai/functions";
+ import { Functions } from "@speakeasy-api/functions";
```

`Gram`, `fromGram` and `withGram` remain available from
`@speakeasy-api/functions` as deprecated aliases of `Functions`,
`fromFunctions` and `withFunctions`. See the
[`@speakeasy-api/functions` README](../functions/README.md) for the full list
of renamed files and environment variables.
