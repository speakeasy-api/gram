---
"@gram-ai/create-function": patch
"@gram-ai/functions": patch
---

Updated esbuild config for Gram Functions to default to emitting a CommonJS require shim, allowing dynamic require() calls to work in bundled code. This is necessary for compatibility with certain dependencies that use dynamic requires.
