---
"server": patch
---

Add nullable columns to `remote_session_issuers` for the OpenID Connect and OAuth capabilities that drive session enrichment: `userinfo_endpoint`, `introspection_endpoint`, `introspection_endpoint_auth_methods_supported`, `id_token_signing_alg_values_supported`, `claims_supported`, `backchannel_logout_supported`, and `authorization_response_iss_parameter_supported`. Schema only; discovery does not populate them yet.
