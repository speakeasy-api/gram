---
"server": minor
"dashboard": patch
---

Eagerly warm the Project Assistant's runtime VM. The dominant latency in the sidebar was a cold-start at send time — the runtime was only booted (`Ensure`) lazily when the first turn arrived, and went cold after a short warm window. `ensureManagedAssistant` (called on sidebar open) now also enqueues a no-op "warm" event that drives the coordinator to boot/refresh the VM without running a turn, and the dashboard keeps it warm with a periodic re-ensure while the sidebar stays open. So the VM is ready before the user's first message instead of booting after it.
