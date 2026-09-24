---
"dashboard": patch
---

Render the admit dialog's subject warning in the destructive color. It was passed as a `className`, which lost to the `Text` component's own `text-stone-800`, so a rule that would admit nothing was flagged in ordinary body text. Uses the component's `destructive` prop instead.
