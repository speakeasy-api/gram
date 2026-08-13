---
"dashboard": patch
"@gram/server": patch
---

Fix "Suggest with AI" exclusion suggestions being rejected as invalid regexes. The exclusion form now validates regex criteria with the same RE2 engine the platform matches with, so valid suggestions like `(?i)`-prefixed patterns save instead of failing with "Invalid regex pattern", server-side validation errors surface in the form, and a suggestion that fails validation is retried once with corrective feedback before falling back.
