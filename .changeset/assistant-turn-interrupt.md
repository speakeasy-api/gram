---
"server": minor
"dashboard": minor
---

The composer's stop button now actually stops the assistant. It previously only aborted the browser's view of the turn: the reply kept generating server-side, kept calling tools, kept spending, and reappeared in the transcript on reload. Pressing stop now calls a new `assistants.interruptTurn` endpoint, which cancels turns still queued on the conversation's thread — the case that matters while a cold runtime is booting — and asks the runtime to interrupt the turn in flight. The runner cancels cooperatively, so the partial reply stays in the transcript instead of being discarded, and a terminal frame goes out on the turn stream so every tab watching the chat settles rather than tailing a turn that has ended.
