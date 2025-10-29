---
"gram-template-gram": patch
"@gram-ai/functions": patch
---

Reverted the Gram Functions TS framework template to export the instance of
`Gram` as the default export in `src/gram.ts`. This makes the boilerplate code
work again when deployed.
