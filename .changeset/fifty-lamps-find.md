---
"dashboard": minor
"server": patch
---

Reshape organization onboarding around outcomes. "Connect identity provider" and "Directory sync" merge into one "Set up identity provider" card; publishing the plugin marketplace becomes a section at the front of the cards that need it instead of a card of its own; Claude Code and Claude Cowork (which the device agent cannot reach) move into a new "Set up Anthropic observability" card and every other coding assistant into "Set up observability in other platforms"; and confirming traffic becomes the last section of both of those cards, filtered to the platforms each one covers. Claude Code and Claude Cowork each get a step of their own on the Anthropic card, with every platform's instructions laid out inline in its step instead of behind a sheet. The board is now the only way into setup: the linear wizard at /setup/wizard and the Wizard/Board switcher are gone, and that URL lands on the board.
