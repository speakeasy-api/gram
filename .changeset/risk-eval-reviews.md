---
"server": minor
---

Risk: add the policy-eval review set endpoints under `risk.evals` —
`saveReview` (upsert a reviewer's ground-truth verdict for a chat session under
a prompt-based policy), `listReviews` (the active regression set for a policy),
and `deleteReview` (clear your own verdict). Verdicts persist in the new
`risk_policy_eval_reviews` table, physically separate from live findings — the
eval workbench scores the live guardrail's agreement against them and derives the
tune direction (false positive → tighten, missed → broaden).
