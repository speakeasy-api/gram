---
"server": minor
"dashboard": minor
---

Collect a work email on the sign-up page and hand it to the hosted AuthKit
screen. `auth.login` takes an optional `email`; when a login carries a company
name — the marker that it began on `/sign-up` — the server sets WorkOS's
`login_hint` so the email field arrives pre-filled, and `screen_hint=sign-up` so
the user lands on the sign-up screen rather than sign-in. The email is validated
before the login nonce is minted and is never stored. The call to action now
reads "Start Trial"; it previously named a single identity provider, which
misdescribed a hand-off that has always been generic.
