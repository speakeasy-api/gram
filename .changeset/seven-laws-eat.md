---
"@gram/functions": patch
---

updates the Gram Functions web server to set a `Gram-Invoke-ID` header containing the decrypted invocation ID from the authorization bearer token. By including this ID in the response, we can add an extra layer of defense in Gram that asserts a function call was handled by a server holding the auth secret.
