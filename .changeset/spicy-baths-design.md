---
"@gram-ai/functions": patch
---

Function authors can now receive the authenticated Gram user's email by specifying `gramEmailVariable` in the `authInput` config. When an authenticated user invokes a tool, the specified environment variable will be populated with their email address.

```ts
const gram = new Gram({
  envSchema: {
    GRAM_USER_EMAIL: z.optional(z.string()),
  },
  authInput: {
    oauthVariable: "OAUTH_TOKEN",
    gramEmailVariable: "GRAM_USER_EMAIL",
  },
});
```
