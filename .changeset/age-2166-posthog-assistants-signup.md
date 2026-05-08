---
"server": patch
---

Capture a `gram_assistants_signup` PostHog event when the auth callback auto-provisions an org for a user landing with `?disposition=assistants`. The event is keyed on the user's email (matches `is_first_time_user_signup`) and carries `organization_id`, `organization_slug`, `disposition`, and `has_assistants_subscription` so the funnel from signup → benefit attach is observable.
