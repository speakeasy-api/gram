# Measuring skill efficacy

Gram's skill insights estimate whether an activated skill helped an agent complete a session with fewer wrong turns, corrections, or rework. The measurements are intended for comparing and improving skills, not evaluating individual users or agents.

## What is scored

The scoring unit is one project, surface, session, and exact skill version. Only explicit skill activations count. A session becomes eligible after at least 30 minutes have passed since both the activation and the last transcript activity.

An automated judge reads the authored skill version and the eligible session transcript, then returns:

- An efficacy score from 0 to 1. Zero means the skill provided no demonstrated help; one means it decisively drove the outcome or prevented substantial rework.
- A short rationale citing the observed evidence.
- Optional estimates of conversation turns and wall-clock minutes saved.
- Low, medium, or high confidence in those estimates.
- Raw flags for ignored, misapplied, partially followed, or harmful guidance.

## Sampling

Efficacy scoring is sampled. By default, Gram evaluates up to 10 sessions per skill per UTC day, 100 sessions per organization per UTC day, and a lifetime burst of 25 sessions for each new skill version. Organization administrators can change these limits or disable scoring.

An efficacy percentage averages only sessions that received a score. An unscored session is not treated as zero efficacy, and a missing efficacy value means that no sampled score is available for the selected window.

## Estimated ROI

Estimated ROI is the sum of the judge's supported time-saved estimates across sampled sessions. The dashboard shows the estimate only when a scored session contains enough evidence to make one. It is directional rather than a billing or productivity guarantee.

Use the accompanying confidence counts and sample size when interpreting the total. A larger estimate based on a few low-confidence samples is weaker evidence than a consistent result across many high-confidence samples.

## Session cost attribution

Cost is measured at session granularity. The full session cost is attributed to every skill version activated in that session so each skill can be viewed in the context of the work it supported.

Because attribution fans out, costs across skills or versions are not additive. Do not sum these figures to calculate project spend; use the Cost dashboard for project-wide totals.

## Reading trends and flags

Version-specific trend lines show when each skill version was active and make changes in efficacy, activation volume, or attributed session cost visible after a new version appears.

Flags are raw signals, not independent verdicts. For example, "marked ignored in 30% of scored sessions" means the judge included the ignored flag in that share of the sampled sessions. Review the session rationale and transcript before changing a skill based on a flag.
