---
"dashboard": patch
---

Redesign the "Book a demo" enterprise gate on the shared auth shell, so it matches the login, register, and switch-organization screens. The page now carries the brand gradient strip, the control-plane header with a Log out action, and the animated governed-agent session alongside the booking card. The Cal.com embed is framed as a native booking card — its own theme variables are set from the auth-brand palette so the calendar reads as part of the page rather than an embedded iframe — and the details handed to the calendar are footnoted underneath.

The embed's appearance settings are now sent through Cal's `ui` instruction. They were previously passed as prefill config, where Cal ignored them, so the embed had been rendering its own event-title block over the card header and none of the brand colors were applied.
