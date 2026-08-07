---
"server": minor
"dashboard": minor
---

Add a `/sign-up` page that collects the company name before handing off to the
identity provider. `auth.login` takes an optional `org_name` param; when set, the
server validates it and stashes a signup intent against the login nonce, then
creates the organization during the auth callback once the identity provider has
answered. The name never travels through a redirect param or the address bar, and
a failed signup returns to `/sign-up` rather than `/register`. Signup attempts and
the resulting org creation are captured as `onboarding_event` / `new_org_created`
with `created_via: "signup"` so the funnel can be measured end to end.
