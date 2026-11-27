---
"server": patch
---

Disabling an organization now also stops its Anthropic inference webhook, which denies every signed request, its plugin marketplace install URLs, device enrollment with join tokens issued earlier, Channels, scheduled device integration syncs, AI integration usage polls and Okta application syncs, public skill share links, and signed chat attachment URLs. Each takes effect from the next request or run, and resumes when the organization is re-enabled.
