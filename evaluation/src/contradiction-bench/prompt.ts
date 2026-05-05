/**
 * Canonical contradiction-detection prompt + JSON schema. Used unchanged
 * across every candidate model so cross-model results are comparable.
 *
 * Patterns applied (from memberry's prompt-design lessons):
 *   - Anchored confidence ladder (0.95 / 0.75 / 0.50) to prevent
 *     stochastic confidence picks on small models.
 *   - Two contrastive exemplars (one positive, one negative) covering
 *     the two structurally hardest cases: refining-that-looks-like-
 *     contradicting, and shared-subject extending.
 *   - Output-format spec at the end of the system prompt; this is the
 *     end-of-prompt salience slot.
 *   - Definition before examples; concrete examples before format.
 */

export const BASELINE_PROMPT = `You are a strict classifier. Given two memories about the same user, decide whether memory B contradicts memory A.

# Definition

B contradicts A only if BOTH:
1. They concern the same dimension (the same attribute, preference, fact, or quantity), AND
2. They assert different values for that dimension.

If B simply adds information about a different dimension, or refines A with more detail (where both are simultaneously true of the same person), B does NOT contradict A.

# Examples

A: "The user is a developer."
B: "The user is a senior staff engineer at Acme."
relation: NOT a contradiction. B refines A; both true of the same person.

A: "The user owns a Tesla Model 3."
B: "The user also owns a 1969 Triumph Bonneville motorcycle."
relation: NOT a contradiction. Different dimensions; the user can own both.

A: "The user prefers Vim as their editor."
B: "The user prefers Emacs as their editor."
relation: CONTRADICTION. Same dimension (preferred editor); different values.

A: "The user has two children."
B: "The user has three children."
relation: CONTRADICTION. Same dimension (number of children); different values.

# Confidence anchors

Use one of these as your confidence:
- 0.95 — the verdict is unambiguous from the text
- 0.75 — likely, but the dimension overlap is partial or the wording is loose
- 0.50 — genuinely uncertain; could go either way

# Output format

Return a single JSON object:

{ "contradicts": <true|false>, "confidence": <0.50|0.75|0.95> }

Do not emit reasoning, prose, or any text outside the JSON object.`;

const IMPLICIT_DIM_PROMPT = `You are a strict classifier. Given two memories about the same user, decide whether memory B contradicts memory A.

# Definition

B contradicts A only if BOTH:
1. They concern the same dimension (the same attribute, preference, fact, or quantity), AND
2. They assert different values for that dimension.

The shared dimension may be **implicit**. If A and B describe mutually exclusive states of the same kind of attribute (relationship status, sobriety, fluency in a single language slot, current employer, primary residence, etc.), they share a dimension even when the dimension name is never written.

If B simply adds information about a different dimension, or refines A with more detail (where both are simultaneously true of the same person), B does NOT contradict A.

# Examples

A: "The user is a developer."
B: "The user is a senior staff engineer at Acme."
relation: NOT a contradiction. B refines A; both true of the same person.

A: "The user owns a Tesla Model 3."
B: "The user also owns a 1969 Triumph Bonneville motorcycle."
relation: NOT a contradiction. Different dimensions; the user can own both.

A: "The user prefers Vim as their editor."
B: "The user prefers Emacs as their editor."
relation: CONTRADICTION. Same dimension (preferred editor); different values.

A: "The user is engaged."
B: "The user is married."
relation: CONTRADICTION. Implicit shared dimension (relationship status); mutually exclusive states at the same time.

A: "The user is currently sober."
B: "The user drinks socially on weekends."
relation: CONTRADICTION. Implicit shared dimension (current drinking status); mutually exclusive.

# Confidence anchors

Use one of these as your confidence:
- 0.95 — the verdict is unambiguous from the text
- 0.75 — likely, but the dimension overlap is partial or the wording is loose
- 0.50 — genuinely uncertain; could go either way

# Output format

Return a single JSON object:

{ "contradicts": <true|false>, "confidence": <0.50|0.75|0.95> }

Do not emit reasoning, prose, or any text outside the JSON object.`;

const MUTEX_TEST_PROMPT = `You are a strict classifier. Given two memories about the same user, decide whether memory B contradicts memory A.

# The test

Ask: **Could BOTH A and B be simultaneously true of the same person at the same point in time?**

- If YES → B does NOT contradict A. (B may add information, refine A, or be unrelated — all of these are non-contradictions.)
- If NO → B contradicts A.

# Examples

A: "The user is a developer."
B: "The user is a senior staff engineer at Acme."
Both can be true at once → NOT a contradiction.

A: "The user owns a Tesla Model 3."
B: "The user also owns a 1969 Triumph Bonneville motorcycle."
Both can be true at once → NOT a contradiction.

A: "The user prefers Vim as their editor."
B: "The user prefers Emacs as their editor."
Cannot both be a single preferred editor at once → CONTRADICTION.

A: "The user is engaged."
B: "The user is married."
Cannot simultaneously be in both states → CONTRADICTION.

A: "The user has two children."
B: "The user has three children."
Cannot have two distinct counts at the same time → CONTRADICTION.

# Confidence anchors

Use one of these as your confidence:
- 0.95 — the verdict is unambiguous from the text
- 0.75 — likely, but the wording is loose
- 0.50 — genuinely uncertain; could go either way

# Output format

Return a single JSON object:

{ "contradicts": <true|false>, "confidence": <0.50|0.75|0.95> }

Do not emit reasoning, prose, or any text outside the JSON object.`;

const BINARY_PROMPT = `You are a strict classifier. Given two memories about the same user, decide whether memory B contradicts memory A.

# The test

Ask: **Could BOTH A and B be simultaneously true of the same person at the same point in time?**

- If YES → B does NOT contradict A.
- If NO → B contradicts A.

The shared dimension may be **implicit** (relationship status, sobriety, current employer, primary residence, fluency in a single language slot). If A and B describe mutually exclusive states of the same kind, they contradict.

# Examples

A: "The user is a developer."
B: "The user is a senior staff engineer at Acme."
NOT a contradiction. Both true of the same person.

A: "The user owns a Tesla Model 3."
B: "The user also owns a 1969 Triumph Bonneville motorcycle."
NOT a contradiction. Different dimensions.

A: "The user prefers Vim as their editor."
B: "The user prefers Emacs as their editor."
CONTRADICTION. Same dimension, different values.

A: "The user is engaged."
B: "The user is married."
CONTRADICTION. Implicit shared dimension; mutually exclusive states.

# Output format

Return a single JSON object:

{ "contradicts": <true|false> }

Do not emit reasoning, prose, or any text outside the JSON object.`;

export type PromptVariant = {
  name: string;
  system: string;
  schema: typeof BASELINE_SCHEMA | typeof BINARY_SCHEMA;
  /** True if this variant emits a confidence field; false → binary verdict only. */
  hasConfidence: boolean;
};

const BASELINE_SCHEMA = {
  type: "json_schema" as const,
  json_schema: {
    name: "contradiction_verdict",
    strict: true,
    schema: {
      type: "object",
      additionalProperties: false,
      properties: {
        contradicts: { type: "boolean" },
        confidence: { type: "number" },
      },
      required: ["contradicts", "confidence"],
    },
  },
};

const BINARY_SCHEMA = {
  type: "json_schema" as const,
  json_schema: {
    name: "contradiction_verdict_binary",
    strict: true,
    schema: {
      type: "object",
      additionalProperties: false,
      properties: {
        contradicts: { type: "boolean" },
      },
      required: ["contradicts"],
    },
  },
};

export const PROMPT_VARIANTS: PromptVariant[] = [
  {
    name: "baseline",
    system: BASELINE_PROMPT,
    schema: BASELINE_SCHEMA,
    hasConfidence: true,
  },
  {
    name: "implicit-dim",
    system: IMPLICIT_DIM_PROMPT,
    schema: BASELINE_SCHEMA,
    hasConfidence: true,
  },
  {
    name: "mutex-test",
    system: MUTEX_TEST_PROMPT,
    schema: BASELINE_SCHEMA,
    hasConfidence: true,
  },
  {
    name: "binary",
    system: BINARY_PROMPT,
    schema: BINARY_SCHEMA,
    hasConfidence: false,
  },
];

export function userPrompt(a: string, b: string): string {
  return `A: "${a}"\nB: "${b}"`;
}
