---
"server": patch
---

Align self-serve PAYG Stripe billing cycles to UTC midnight. Checkout retries now reuse one durable session intent, active product trials retain their exact local end while Stripe supplies the free stub to midnight, and completed subscriptions persist the confirmed paid-period anchor.
