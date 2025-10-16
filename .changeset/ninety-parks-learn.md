---
"@gram/server": patch
---

Update the gateway to check the `Gram-Invoke-ID` response header from Gram Functions tool calls before proxying the response back to the client. This is an added security measure that asserts the server that ran a function had access to the auth secret and was able to decrypt the bearer token successfully.
