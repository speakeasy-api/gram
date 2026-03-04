---
"@gram-ai/functions": patch
---

Function authors can now receive the authenticated Gram user's email by setting `gramEmail: true` in the `authInput` config. When an authenticated user invokes a tool, the `GRAM_USER_EMAIL` environment variable will be populated with their email address.

```ts
const gram = new Gram({
  authInput: {
    oauthVariable: "OAUTH_TOKEN",
    gramEmail: true,
  },
});
```
