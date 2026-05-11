---
"server": minor
---

Add wake triggers: one-shot self-wakes that an assistant schedules from inside its own turn to resume work later. New `platform_schedule_wake` and `platform_cancel_wake` tools let an assistant set a future fire time (up to 30 days out) with an optional self-note; when the wake fires, dispatch lands on the same thread it was scheduled from. Pending wakes are cancelled automatically when the owning assistant is deleted.
