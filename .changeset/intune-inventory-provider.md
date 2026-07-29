---
"server": minor
"dashboard": patch
---

Add the Microsoft Intune inventory-source provider to device integrations:
Entra ID client-credentials auth (classifying Entra's 400 invalid_client
shape as a credential rejection), field-selected managed-device pulls via
Microsoft Graph with server-driven nextLink pagination (cursor validated to
stay on the Graph host), and mapping into the normalized managed-device
shape with emailAddress-then-UPN user attribution.
