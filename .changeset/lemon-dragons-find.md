---
"@gram-ai/functions": patch
---

Added an "engines" field to the package.json files of the `@gram-ai/functions`
requiring Node.js version 22.18.0 or higher. This ensures that we are in a
runtime that supports import assertions and native support for running
TypeScript files without experimental flags.
