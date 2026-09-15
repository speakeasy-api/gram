---
"dashboard": minor
---

Add a guided Okta path to the identity provider setup card. Picking Okta over OIDC, offered ahead of the SAML entry and marked Guided, replaces the two WorkOS portal round trips with Okta's own five sub-steps: connect Okta, single sign-on, directory, applications and access, and token exchange. The connect step explains the Okta console ceremony inline, states up front that granting API scopes needs an Okta Super Administrator, and says why nothing secret is pasted in either direction. The four later sub-steps are named and locked, so the shape of the work is visible from the start, and the choice of provider stays reversible until a value is submitted. Every other provider keeps the existing portal flow unchanged.
  