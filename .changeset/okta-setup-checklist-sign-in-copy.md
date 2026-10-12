---
"server": patch
---

The Okta setup checklist now says that the AI agent's linked app is the app people use to sign in to Speakeasy. After the agent is registered and linked, you record the agent ID, then a new **Add the agent's public key** step has you use Set up Okta sign-in, paste the public key it shows in the agent's Credentials and click Activate, before activating or editing the linked app, because Okta rejects changes to the linked app until the agent has a public key. The step notes that rotating or publishing a new key means pasting it in Okta again, and that setup needs customer-managed encryption keys and a Google Cloud KMS key. The activate step then adds the Authorization Code and Refresh Token grant types and the sign-in redirect URI. It also says that people who sign in through Okta must already exist in Speakeasy through directory sync (SCIM).

The checklist's first Cross App Access connection step now says, in the same words as the dashboard's notice, that each vendor must also enable enterprise-managed authorization in its own admin console and trust your Okta issuer, with Linear as the example. Many vendors require an Enterprise or SSO plan, and Speakeasy cannot do this step for you.
