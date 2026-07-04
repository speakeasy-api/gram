---
"dashboard": minor
---

Policy eval workbench: wire the Evaluate step to the real backend. The session
picker now searches actual chat sessions (`chat.list`), each row's Flagged/Clean
badge is the live guardrail judge verdict (`risk.evals.evaluate`, cached per
guardrail+session and debounced so typing doesn't re-judge on every keystroke),
and the transcript renders the real chat with the judge's per-message
highlights (`chat.load`). Verdicts persist as the policy's review set
(`risk.evals.saveReview`/`listReviews`/`deleteReview`) when editing an existing
policy; during create they stay session-local. Removes the session-local mock.
