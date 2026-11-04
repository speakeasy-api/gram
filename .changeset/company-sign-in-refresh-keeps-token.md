---
"server": patch
---

Keep a person's saved company sign-in when the identity provider rejects a refresh for a reason that doesn't say the token is dead. Later requests try again every five minutes, so access can return without a new sign-in once an administrator fixes the problem at the provider. If the provider keeps refusing for three days, or a retry gets no clear answer from it, the person is asked to sign in again. A rejection of the client itself doesn't count toward the three days, since a sign-in can't fix it. A rate limit on a working sign-in no longer forces a new one. The connect page now says a kept sign-in is retried by itself. The person is also asked to sign in when a refresh was left unfinished or the sign-in setup changed, and company sign-in errors no longer show raw codes.
