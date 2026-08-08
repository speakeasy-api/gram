import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { ExclusionFields } from "./exclusion-expression";
import { getRuleTitleFallback } from "./risk-utils";

// The ready-made rules offered for a finding-originated exclusion. Each option
// is a complete, savable rule: `fields` goes straight to the mutation, skipping
// the expression serialize -> parse round trip. Only "custom" has no fields —
// it reveals the DSL textarea instead.
export interface ExclusionOption {
  value: "exact" | "rule" | "source" | "custom";
  title: string;
  hint: string;
  fields?: ExclusionFields;
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
// finding whose plaintext we actually hold — `revealed` covers the masked list
// pages, where the raw match never reaches the browser until it is unmasked.
// Rule and source rules need every selected row to agree on that field.
export function exclusionOptions(
  results: RiskResult[],
  revealed?: string,
): ExclusionOption[] {
  const options: ExclusionOption[] = [];

  const match = results.length === 1 ? (revealed ?? results[0]?.match) : "";
  if (match) {
    options.push({
      value: "exact",
      title: `Just this value — ${match}`,
      hint: "Only this exact value stops being flagged.",
      fields: fields("exact", match),
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
