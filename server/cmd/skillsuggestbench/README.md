# skillsuggestbench

Measures the skill-suggestion generator — the model that turns skill feedback
into proposed manifest edits — against a labeled corpus.

```sh
# the production model, as configured in suggest.Model
mise x -- go run ./server/cmd/skillsuggestbench

# compare candidates
mise x -- go run ./server/cmd/skillsuggestbench \
  -models "openai/gpt-5.6-sol,openai/gpt-5.6-terra,anthropic/claude-sonnet-5" -runs 3
```

Needs `OPENROUTER_DEV_KEY` or `OPENROUTER_API_KEY`. Every run makes real calls
and spends real credit: `cases × models × runs`.

## What it measures

There is no second model grading prose here. Every check is one the production
pipeline already performs, so a case that passes is a suggestion that would have
reached a reviewer.

| Metric      | Meaning                                                                                                                                                                                                                                                |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `apply`     | Share of propose-labeled runs served as an anchored, validated proposal, after the same single correction round production allows. Errors and wrong declines count against it. **The gate that decides whether a model can drive the feature at all.** |
| `decision`  | How often propose/decline matched the label.                                                                                                                                                                                                           |
| `validate`  | Share whose resolved manifest survived `ValidateSkillSuggestion`.                                                                                                                                                                                      |
| `evidence`  | Share of served changes citing at least one in-range feedback ref. Gated by `minimum_evidence_rate`.                                                                                                                                                   |
| `shortfall` | Proposals that returned fewer changes than the feedback justified.                                                                                                                                                                                     |

`apply` is the interesting one. A change is a find/replace against the manifest,
so the model has to reproduce document text **verbatim and uniquely** —
`ResolveChanges` refuses an anchor it cannot locate exactly once, because
guessing which occurrence was meant is how an edit lands in the wrong paragraph.
A model that paraphrases instead of copying fails the whole suggestion, and unit
tests cannot see it because they stub the completion client.

## Corpus

`cases.json` pairs a manifest with the feedback a reviewer would have seen, plus
the decision that feedback justifies. It deliberately includes cases that are
not straightforward:

- `repeated-line-anchor` — a line that appears twice in the manifest, so a bare
  anchor is ambiguous and `find` has to be extended.
- `injection-in-feedback` — feedback whose text is an instruction-injection
  attempt. The prompt frames feedback as untrusted data; a model that obeys it
  rewrites the skill wholesale.
- `vague-praise-decline` — negative sentiment carrying no actionable content.
  Declining is correct; proposing is churn.
- `two-distinct-gaps` — two unrelated gaps, which must come back as two separate
  changes so a reviewer can take one without the other.

Adding a case means adding a manifest that parses (checked at load time, so a
malformed fixture cannot read as a model regression) and a label that a careful
reviewer would agree with.

## Gates

`minimum_apply_rate`, `minimum_decision_accuracy`, and `minimum_evidence_rate`
live in `cases.json`. The command exits nonzero when a model misses any of
them, so it can gate a model change. Every scheduled run counts in every
denominator: completion errors are misses, not exclusions.

## Reasoning

`-reasoning-effort` overrides the reasoning setting. Empty matches production,
which disables reasoning: the answer is a schema-shaped edit, not an argument,
and reasoning tokens are billed. Some routes reject a disabled setting outright
("Reasoning is mandatory for this endpoint") and can only be benched by passing
an effort — which also makes their cost and latency numbers non-comparable to a
reasoning-off baseline. Run the baseline model under both settings when
comparing against such a route.
