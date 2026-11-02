---
"server": patch
---

Keep a person's saved company sign-in when the identity provider rejects a refresh for a reason other than an invalid token. The next request after five minutes tries again, so access can return without a new sign-in once an administrator fixes the problem at the provider.
