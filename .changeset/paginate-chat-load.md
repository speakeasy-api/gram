---
"server": minor
"dashboard": minor
---

chat.load now paginates a generation's messages by `seq` keyset (`limit`, `before_seq`, `after_seq`) and exposes each message's `seq` plus `has_more_before`/`has_more_after`. A new `risk_only` flag returns just the messages with active risk findings padded with surrounding context, grouped into contiguous `risk_segments` that can be expanded on demand. The chat detail sheet consumes this with a virtualized transcript (`@tanstack/react-virtual`, constant DOM node count regardless of how many pages are loaded) and infinite scroll (scroll up to load older messages, anchored so the viewport doesn't jump), and renders the risk-only view as expandable segments with load-above/below and gap-fill controls.
