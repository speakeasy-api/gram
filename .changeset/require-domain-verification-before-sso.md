---
"server": minor
"dashboard": minor
---

Organizations now verify a domain before setting up single sign-on. WorkOS refuses to start an SSO connection until a domain is verified, so the IdP and SSO page gains a Domain verification card that opens the WorkOS Admin Portal. Once verified, the card lists every verified domain, because SSO only applies to users on those domains. Until a domain is verified, the Single Sign-On and Directory Sync cards are dimmed, their Configure buttons are disabled, and an amber warning explains why. The setup board adds a "Verify your domain" task that the identity provider task now depends on. Organizations that already have an active SSO connection are treated as verified and are not blocked.

The verified domains are kept in sync from WorkOS events: `organization_domain.verified` adds a domain, `organization_domain.deleted` removes one, and `organization.created` / `organization.updated` replace the list from the organization's full domain set. Onboarding status reports `domain_verified` and `verified_domains`.
