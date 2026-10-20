import type { CatalogEntry, CatalogVariable } from "./definition";

const PLACEHOLDER = /\{([a-z_][a-z0-9_]*)\}/g;

export type VariableValues = Record<string, string>;

/** Why a variable's value cannot be used, or null when it can. */
export function variableProblem(
  variable: CatalogVariable,
  value: string,
): string | null {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return `Enter your ${variable.label}.`;
  }
  if (
    variable.pattern !== undefined &&
    !patternMatches(variable.pattern, trimmed)
  ) {
    return variable.patternMessage ?? `${variable.label} is not valid.`;
  }
  return null;
}

/**
 * Whether value matches pattern in full. A pattern this browser cannot compile
 * matches nothing, so the value reads as invalid rather than the step breaking.
 */
function patternMatches(pattern: string, value: string): boolean {
  try {
    return new RegExp(`^(?:${pattern})$`).test(value);
  } catch {
    return false;
  }
}

/**
 * The template with each `{key}` replaced by its trimmed value, or null while
 * any placeholder's value is missing or invalid.
 */
function fillTemplate(
  entry: CatalogEntry,
  template: string,
  values: VariableValues,
): string | null {
  let complete = true;
  const filled = template.replace(PLACEHOLDER, (_, key: string) => {
    const variable = entry.variables.find((v) => v.key === key);
    const value = values[key] ?? "";
    if (variable === undefined || variableProblem(variable, value) !== null) {
      complete = false;
      return "";
    }
    return value.trim();
  });
  return complete ? filled : null;
}

export interface SubjectRule {
  subject: string;
  matchKind: "exact" | "wildcard";
}

/** The access rule an entry's subject template produces for these values. */
export function subjectRule(
  entry: CatalogEntry,
  values: VariableValues,
): SubjectRule | null {
  const filled = fillTemplate(entry, entry.subject.template, values);
  if (filled === null) {
    return null;
  }
  if (entry.subject.wildcard) {
    return { subject: `${filled}*`, matchKind: "wildcard" };
  }
  return { subject: filled, matchKind: "exact" };
}
