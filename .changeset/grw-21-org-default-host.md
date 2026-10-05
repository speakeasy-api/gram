---
"server": minor
---

Keep each organization's emailed links on its own host. Invitations, setup and access request emails, trial and billing emails, weekly usage summaries, and custom domain health alerts now build their links from the organization's recorded default host instead of the site URL. Organizations with no recorded host use `GRAM_LEGACY_DEFAULT_HOST`, which defaults to the site URL, so they keep their current links when the canonical host moves. `GRAM_NEW_ORG_DEFAULT_HOST` records a default host on organizations created from now on; empty records none. The worker and admin server now also read `GRAM_PLATFORM_HOSTS` to re-check recorded hosts.
