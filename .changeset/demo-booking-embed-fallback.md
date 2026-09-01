---
"dashboard": patch
---

The demo booking calendar no longer depends on the Cal.com embed working. The `ui` instruction and the embed now share a dedicated Cal namespace, so it can no longer be delivered to an instance with no iframe, and it is retried once Cal reports the iframe ready. A branded cover holds the card until the calendar is actually visible, and an embed that never becomes ready hands the visitor a direct booking link and the sales email instead of an empty box. Both outcomes are captured as `demo_booking_embed_loaded` and `demo_booking_embed_failed`, so a calendar that stops rendering is now a metric rather than something to notice by hand.
