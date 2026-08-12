import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { ExclusionFields } from "./exclusion-expression";
import { getRuleTitleFallback } from "./risk-utils";

// The ready-made rules offered for a finding-originated exclusion. Each option
// carries a complete rule in `fields`, which goes straight to the mutation,
// skipping the expression serialize -> parse round trip. Two options have no
// fields and so cannot be saved as-is: "custom", which reveals the DSL textarea
// instead, and an "exact" whose plaintext is still behind an unfired reveal.
export interface ExclusionOption {
  value: "exact" | "rule" | "source" | "custom";
  title: string;
  hint: string;
  fields?: ExclusionFields;
}

/** The value an exact-match rule would be written against. The masked list
 * pages hold only `label` (the redacted placeholder) until the operator
 * selects the option and the audited reveal resolves into `value`. */
export interface ExactCandidate {
  value?: string;
  label: string;
}

// Where the plaintext for an exact rule comes from, in order: the finding
// itself (the chat surfaces carry it), a resolved reveal, or — on the masked
// list pages — nothing yet, in which case the redacted placeholder labels an
// option that exists only to be selected. `undefined` means no exact rule is
// on offer at all: a multi-row selection, or `canReveal` false because the
// caller lacks `chat:read` or the finding has no match behind it (a judge
// verdict), in which case the option must not appear as a dead affordance.
export function exactCandidate(
  single: RiskResult | undefined,
  revealed: string | null,
  canReveal: boolean,
): ExactCandidate | undefined {
  if (!single) return undefined;
  if (single.match) return { value: single.match, label: single.match };
  if (revealed) return { value: revealed, label: revealed };
  if (canReveal && single.matchRedacted) return { label: single.matchRedacted };
  return undefined;
}

const CUSTOM_OPTION: ExclusionOption = {
  value: "custom",
  title: "Write it myself",
  hint: "Regex, entity types, and rule/source filters.",
};

function fields(
  matchType: ExclusionFields["matchType"],
  matchValue: string,
): ExclusionFields {
  return { matchType, matchValue, ruleIdFilter: "", sourceFilter: "" };
}

function shared<K extends "ruleId" | "source">(
  results: RiskResult[],
  key: K,
): string | undefined {
  const value = results[0]?.[key];
  return value && results.every((r) => r[key] === value) ? value : undefined;
}

// Builds the options valid for a selection. An exact-value rule needs a single
// finding — its plaintext comes from the finding itself on the chat surfaces,
// or from `exact` on the masked list pages, where the raw match never reaches
// the browser until it is unmasked. Rule and source rules need every selected
// row to agree on that field.
export function exclusionOptions(
  results: RiskResult[],
  exact?: ExactCandidate,
): ExclusionOption[] {
  const options: ExclusionOption[] = [];

  const own = results.length === 1 ? results[0]?.match : undefined;
  const match = exact?.value ?? own;
  const label = exact?.label ?? own;
  if (label) {
    options.push({
      value: "exact",
      title: `Just this value — ${label}`,
      hint: match
        ? "Only this exact value stops being flagged."
        : "Select to reveal the value — this is recorded in the audit log.",
      // Absent while a reveal is still pending: the option renders and can be
      // selected (that is what fires the reveal), but cannot yet be saved.
      fields: match ? fields("exact", match) : undefined,
    });
  }

  const ruleId = shared(results, "ruleId");
  if (ruleId) {
    options.push({
      value: "rule",
      title: `Any ${getRuleTitleFallback(ruleId)} finding`,
      hint: "Every finding from this detection rule stops being flagged.",
      fields: fields("rule_id", ruleId),
    });
  }

  const source = shared(results, "source");
  if (source) {
    options.push({
      value: "source",
      title: `Anything detected by ${source}`,
      hint: "Every finding from this detector stops being flagged.",
      fields: fields("source", source),
    });
  }

  options.push(CUSTOM_OPTION);
  return options;
}
