---
"@gram-ai/functions": patch
---

Removed `import.meta.url` check for bin scripts. The value for that meta
property is not resolving to be equal to process.argv[1] when running the
compiled JavaScript files as bin scripts.
