import type { RiskResult } from "@gram/client/models/components/riskresult.js";

export type ToolRiskField = "tool.args" | "tool_result";

export interface ToolSectionRiskMatch {
  value: string;
  result: RiskResult;
}

/**
 * Returns only findings that can be rendered inside a specific tool section.
 *
 * A tool-request result belongs to the whole chat message, so it may describe
 * the function name rather than its arguments. Newer results attribute every
 * span to a field; legacy detectors do not, so those are retained only when the
 * matched value is actually present in the section content.
 */
export function toolSectionRiskMatches(
  results: RiskResult[] | undefined,
  content: string | undefined,
  field: ToolRiskField,
): ToolSectionRiskMatch[] {
  if (!results?.length || !content) return [];

  const byValue = new Map<string, ToolSectionRiskMatch>();
  for (const result of results) {
    const spans =
      result.spans && result.spans.length > 0
        ? result.spans
        : result.match
          ? [{ match: result.match }]
          : [];

    for (const span of spans) {
      if (
        !span.match ||
        (span.field && span.field !== field) ||
        !content.includes(span.match) ||
        byValue.has(span.match)
      ) {
        continue;
      }
      byValue.set(span.match, { value: span.match, result });
    }
  }

  return [...byValue.values()].toSorted(
    (a, b) => b.value.length - a.value.length,
  );
}
