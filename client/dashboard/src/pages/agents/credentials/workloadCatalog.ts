import { isKnownEnumValue } from "@/lib/enum";
import { WorkloadPlatformConstantVisibility } from "@gram/client/models/components/workloadplatformconstant.js";
import { WorkloadPlatformVariableTier } from "@gram/client/models/components/workloadplatformvariable.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import type { WorkloadPlatform } from "@gram/client/models/components/workloadplatform.js";
import { useWorkloadPlatforms } from "@gram/client/react-query/workloadPlatforms.js";
import { useMemo } from "react";

/**
 * A platform's federation policy, as the server ships it.
 *
 * This is what an operator cannot be expected to know — the platform's issuer,
 * where it publishes its keys, the shape of its subjects — and what they must
 * supply themselves. The server validates tokens against it, so it stays the
 * server's. How a setup is explained is the dashboard's, and lives in the
 * instructions beside this file.
 */

interface CatalogConstant {
  value: string;
  visibility: WorkloadPlatformConstantVisibility;
}

/**
 * Something the operator supplies. A platform-tier variable is fixed once per
 * trusted platform; a rule-tier variable is supplied once per access rule.
 */
export interface CatalogVariable {
  key: string;
  tier: WorkloadPlatformVariableTier;
  label: string;
  help?: string;
  placeholder?: string;
  /** Anchored by the validator; a value must match it in full. */
  pattern?: string;
  patternMessage?: string;
}

/**
 * The subject an access rule matches, with `{key}` placeholders for rule-tier
 * variables. A wildcard template is a stem: the rule is the filled template
 * followed by `*`, and the template must end on a delimiter of the platform's
 * subject format so the stem cannot run into a neighboring value.
 */
interface SubjectTemplate {
  template: string;
  wildcard: boolean;
}

export interface CatalogEntry {
  /** Permanent: written onto the rows this entry creates. */
  key: string;
  displayName: string;
  description: string;
  /** The platform's logo, a path on the dashboard's origin. */
  icon?: string;
  issuer: CatalogConstant;
  jwksUri: CatalogConstant;
  variables: CatalogVariable[];
  subject: SubjectTemplate;
  /** A disabled entry stays listed so operators can see it is known. */
  enabled: boolean;
}

/** Unknown policy metadata stays visible, but cannot drive a guided setup. */
export function isSupportedCatalogEntry(entry: CatalogEntry): boolean {
  return (
    isKnownEnumValue(
      WorkloadPlatformConstantVisibility,
      entry.issuer.visibility,
    ) &&
    isKnownEnumValue(
      WorkloadPlatformConstantVisibility,
      entry.jwksUri.visibility,
    ) &&
    entry.variables.every((variable) =>
      isKnownEnumValue(WorkloadPlatformVariableTier, variable.tier),
    )
  );
}

/**
 * Values the server derives for the platform to be pointed at. The
 * instructions can place them but never compute them, so the console values an
 * operator copies are the ones the authorization server's metadata publishes.
 */
export type ComputedValueKey = "token_endpoint" | "issuer_url" | "mcp_host";

/**
 * The catalog the server ships. The server validates it; this reshapes.
 *
 * `enabled` is for a caller that may not need the catalog at all: reading it
 * is a workload-policy request, so a screen that never shows a platform
 * should not make one.
 */
export function useCatalogEntries(enabled = true): {
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
} {
  const query = useWorkloadPlatforms({}, undefined, {
    enabled,
    throwOnError: false,
  });
  const entries = useMemo(
    () => (query.data?.platforms ?? []).map(toCatalogEntry),
    [query.data],
  );
  return {
    entries,
    isPending: query.isPending,
    isError: query.isError,
    refetch: () => void query.refetch(),
  };
}

function toCatalogEntry(platform: WorkloadPlatform): CatalogEntry {
  return {
    key: platform.key,
    displayName: platform.displayName,
    description: platform.description,
    icon: optional(platform.icon),
    enabled: platform.enabled,
    issuer: platform.issuer,
    jwksUri: platform.jwksUri,
    variables: platform.variables.map((variable) => ({
      key: variable.key,
      tier: variable.tier,
      label: variable.label,
      help: optional(variable.help),
      placeholder: optional(variable.placeholder),
      pattern: optional(variable.pattern),
      patternMessage: optional(variable.patternMessage),
    })),
    subject: platform.subject,
  };
}

/** An empty string, which the server sends for an unset field, as absent. */
function optional(value: string): string | undefined {
  return value === "" ? undefined : value;
}

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

/**
 * The organization-wide access rule already admitting this rule under the
 * entry's issuer, if there is one. The server refuses a second one, so the
 * setup says so before the operator gets that far.
 */
export function existingRule(
  issuer: string,
  admissions: WorkloadAdmission[],
  rule: SubjectRule,
): WorkloadAdmission | undefined {
  return admissions.find(
    (admission) =>
      admission.issuer === issuer &&
      admission.subject === rule.subject &&
      admission.matchKind === rule.matchKind &&
      admission.projectId === "",
  );
}

/**
 * Says the access rule the operator's values produce already exists, naming
 * the values by their labels, such as "This Anthropic organization ID".
 */
export function duplicateRuleMessage(
  entry: CatalogEntry | undefined,
  platformName: string,
  agentName?: string,
): string {
  const labels = (entry?.variables ?? [])
    .filter((variable) => variable.tier === "rule")
    .map((variable) => variable.label);
  const what = labels.length === 1 ? `This ${labels[0]}` : "This access rule";
  const under = agentName === undefined ? "" : `, under ${agentName}`;
  return `${what} is already connected to ${platformName}${under}. Use a different one, or remove it from that agent first.`;
}

/**
 * The trusted platform an issuer URL corresponds to, if the organization
 * trusts it. Matched on the issuer URL, as admitting a subject is, until rows
 * carry the catalog key they were created from (AIM-374).
 */
export function connectedIssuer(
  issuer: string,
  issuers: WorkloadIssuer[],
): WorkloadIssuer | undefined {
  return issuers.find((candidate) => candidate.issuer === issuer);
}

/**
 * The values a filled template was made from, read back out of it, or null
 * when it was not made from this template. A placeholder reads one path
 * segment: every catalog pattern refuses "/", so a value never spans two.
 */
function readTemplate(template: string, filled: string): VariableValues | null {
  const keys: string[] = [];
  let source = "";
  let last = 0;
  for (const match of template.matchAll(PLACEHOLDER)) {
    source += escapeRegExp(template.slice(last, match.index)) + "([^/]+)";
    keys.push(match[1] ?? "");
    last = match.index + match[0].length;
  }
  source += escapeRegExp(template.slice(last));
  const match = new RegExp(`^${source}$`).exec(filled);
  if (match === null) return null;
  return Object.fromEntries(
    keys.map((key, index) => [key, match[index + 1] ?? ""]),
  );
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * The variable values a subject was built from, read back out of it.
 *
 * Editing shows what the rule says, and the rule is all there is: the values
 * someone typed to make it are not stored anywhere else.
 */
export function valuesFromSubject(
  entry: CatalogEntry,
  subject: string,
): VariableValues {
  const stem = entry.subject.wildcard ? subject.replace(/\*$/, "") : subject;
  return readTemplate(entry.subject.template, stem) ?? {};
}

/**
 * The platform-tier values an issuer was built from, such as a cluster's
 * project, location and name. Empty for an entry whose issuer is a constant.
 */
export function valuesFromIssuer(
  entry: CatalogEntry,
  issuer: string,
): VariableValues {
  return readTemplate(entry.issuer.value, issuer) ?? {};
}

/**
 * The catalog entry a trusted issuer came from. An entry whose issuer varies
 * per platform, such as one per cluster, produces many issuers, so the match
 * is on the template rather than on its text.
 */
export function catalogEntryForIssuer(
  entries: CatalogEntry[],
  issuer: string,
): CatalogEntry | undefined {
  return entries.find(
    (entry) => readTemplate(entry.issuer.value, issuer) !== null,
  );
}

/**
 * The issuer and key URL these values make, or null while any platform-tier
 * value they need is missing or invalid. Identical to the entry's own for an
 * entry whose issuer is a constant.
 */
export function platformEndpoints(
  entry: CatalogEntry,
  values: VariableValues,
): { issuer: string; jwksUri: string } | null {
  const issuer = fillTemplate(entry, entry.issuer.value, values);
  const jwksUri = fillTemplate(entry, entry.jwksUri.value, values);
  if (issuer === null || jwksUri === null) return null;
  return { issuer, jwksUri };
}
