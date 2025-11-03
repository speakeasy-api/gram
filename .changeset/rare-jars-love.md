---
"@gram-ai/create-function": patch
---

Fixed an issue where Gram Functions templates were not enforcing a minimum
Node.js version due to a typo in the generated package.json files. The field was
incorrectly named "engine" instead of "engines".
