---
"server": minor
"dashboard": minor
---

Add the Non-Corporate Accounts risk-policy category (detection source `account_identity`). Policies can now flag sessions authenticated with a personal AI account (`identity.personal_account`) or with an AI-account email domain outside a configurable approved list (`identity.unapproved_domain`), reusing the account attribution captured by session ingest. The create/update policy endpoints accept `approved_email_domains`, findings are emitted once per session, and the Policy Center exposes the approved-domains input in the category's Customize sheet (flag-only, like other agent-integrity detectors).
