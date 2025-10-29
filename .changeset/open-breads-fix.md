---
"@gram-ai/functions": patch
---

Switched away from `import.meta.main` to `import.meta.url`. The former approach
is supported primarily by Deno and Bun and only gained experimental support in
Node.js 22.18.0. To ensure broader compatibility across different Node.js
versions, we replace these checks with a more traditional method that compares
`import.meta.url` to the script's file URL derived from `process.argv[1]`.
