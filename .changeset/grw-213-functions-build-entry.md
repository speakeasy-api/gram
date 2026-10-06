---
"@gram-ai/functions": minor
---

Add `build` and `resolveProject` to `@gram-ai/functions/build`, which `speakeasy functions build` and `speakeasy functions push` use to build a project with the SDK version it depends on. `gf build` and `gf push` keep working and now print a notice that points to the `speakeasy functions` commands. Project templates now run `speakeasy functions build` and `speakeasy functions push`.
