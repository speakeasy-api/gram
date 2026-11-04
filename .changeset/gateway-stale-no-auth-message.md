---
"server": patch
"dashboard": patch
---

When a gateway server gets a sign-in after a person approved it as needing none, calling that server now says what to do: reconnect the gateway in their AI app, or ask an administrator if reconnecting can't fix it. The session keeps working for the gateway's other servers, and token refresh is unchanged. In Code Mode, `tools.connect` now returns that message when reconnecting can't fix it, instead of a sign-in link. Gateway messages now say "Reconnect this gateway in your AI app" wherever that's the fix.
