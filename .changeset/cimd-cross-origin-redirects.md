---
"server": patch
---

Client ID Metadata Documents may now register `redirect_uris` on a different origin than the `client_id` URL: the validator's same-origin binding is removed in favor of a `cimd.redirect_uris.cross_origin` counter and a log line naming the cross-origin redirect origins, while exact-match validation of the authorization request's `redirect_uri` against the document remains the enforced control. Non-loopback redirect URIs in a document must now use https explicitly; previously custom schemes were rejected only as a side effect of the origin binding, and RFC 8252 http loopback redirects are unchanged.
