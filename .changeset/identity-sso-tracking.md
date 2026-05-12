---
"dashboard": minor
---

Rename the org "Security" tab to "Identity" and refresh the SSO / Directory Sync cards: drop the SAML-specific branding (Single Sign-On / SSO instead of SAML SSO), replace the hover popover with a tooltip on a fully clickable Configure button, and capture an `identity_provider_interest` PostHog event on click so the team is pinged in Slack when a customer expresses interest. Clicking now confirms with a success toast.
