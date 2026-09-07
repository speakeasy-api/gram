import {
  POLICY_MESSAGE_TYPE_META,
  type PolicyMessageType,
} from "./policy-data";
import { ALL_POLICY_MESSAGE_TYPES } from "./policy-form";

export type Scope = {
  scopeInclude?: string;
  scopeExempt?: string;
};

export type CategoryScope = Scope & {
  category: string;
};

export type CategoryScopeRecommendation = {
  key: string;
  recommendedScopeApplicable: boolean;
  recommendedScopeInclude?: string;
  recommendedScopeExempt?: string;
};

type AdditionalPolicyMessageType = "prompt_attachment";
type EffectivePolicyMessageType =
  | PolicyMessageType
  | AdditionalPolicyMessageType;

export type EffectivePolicyScope = {
  kinds: Set<PolicyMessageType>;
  additionalKinds: Set<AdditionalPolicyMessageType>;
  custom: boolean;
};

type EffectivePolicyScopeInput = Scope & {
  categories: Iterable<string>;
  detectionScopes?: CategoryScope[];
  categoryDefinitions?: CategoryScopeRecommendation[];
  messageTypes?: string[];
};

function isPolicyMessageType(value: string): value is PolicyMessageType {
  return ALL_POLICY_MESSAGE_TYPES.includes(value as PolicyMessageType);
}

export function encodeKindScope(types: PolicyMessageType[]): string {
  const values = [...new Set(types)].sort();
  if (values.length === 0) {
    throw new Error("detection scope message types must not be empty");
  }
  if (!values.every(isPolicyMessageType)) {
    throw new Error("unsupported detection scope message type");
  }
  return `kind in ${JSON.stringify(values)}`;
}

export function replaceCategoryDetectionScope(
  scopes: CategoryScope[] | undefined,
  replacement: CategoryScope,
): CategoryScope[] {
  return [
    ...(scopes ?? []).filter(
      (scope) => scope.category !== replacement.category,
    ),
    replacement,
  ];
}

export function kindScopeForMessageTypes(types: PolicyMessageType[]): Scope {
  if (types.length > 0) return { scopeInclude: encodeKindScope(types) };

  const allKinds = encodeKindScope(ALL_POLICY_MESSAGE_TYPES);
  return { scopeInclude: allKinds, scopeExempt: allKinds };
}

function isEffectivePolicyMessageType(
  value: string,
): value is EffectivePolicyMessageType {
  return isPolicyMessageType(value) || value === "prompt_attachment";
}

function decodeKindEquality(cel: string): EffectivePolicyMessageType | null {
  const kind = /^\(?\s*kind\s*==\s*"(\w+)"\s*\)?$/.exec(cel.trim())?.[1];
  return kind && isEffectivePolicyMessageType(kind) ? kind : null;
}

/** `kind in [...]` in any order, with or without duplicates — the server and
 *  hand-written policies are under no obligation to emit our canonical form. */
function decodeKindMembership(
  cel: string,
): EffectivePolicyMessageType[] | null {
  const list = /^\s*kind\s+in\s+(\[[\s\S]*\])\s*$/.exec(cel)?.[1];
  if (!list) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(list);
  } catch {
    return null;
  }
  if (!Array.isArray(parsed) || parsed.length === 0) return null;

  const kinds = [...new Set(parsed)];
  return kinds.every(
    (kind): kind is EffectivePolicyMessageType =>
      typeof kind === "string" && isEffectivePolicyMessageType(kind),
  )
    ? kinds
    : null;
}

function decodeEffectiveKindScope(
  cel: string,
): EffectivePolicyMessageType[] | null {
  const membership = decodeKindMembership(cel);
  if (membership) return membership;

  const equality = decodeKindEquality(cel);
  if (equality) return [equality];

  const terms = cel.split("||");
  if (terms.length > 1) {
    const values = terms.map(decodeKindEquality);
    if (
      values.every(
        (value): value is EffectivePolicyMessageType => value !== null,
      )
    ) {
      return [...new Set(values)];
    }
  }

  return null;
}

export function decodeKindScope(cel: string): PolicyMessageType[] | null {
  const decoded = decodeEffectiveKindScope(cel);
  return decoded?.every(isPolicyMessageType) ? decoded : null;
}

function promptAttachmentScopeMatch(cel: string): boolean | null {
  const decoded = decodeEffectiveKindScope(cel);
  return decoded ? decoded.includes("prompt_attachment") : null;
}

function scopeAllowsPromptAttachment({
  scopeInclude,
  scopeExempt,
}: Scope): boolean {
  let included = true;
  if (scopeInclude) {
    included = promptAttachmentScopeMatch(scopeInclude) ?? true;
  }
  if (included && scopeExempt) {
    included = !(promptAttachmentScopeMatch(scopeExempt) ?? false);
  }
  return included;
}

export function effectiveScopeKinds({ scopeInclude, scopeExempt }: Scope): {
  kinds: Set<PolicyMessageType>;
  custom: boolean;
} {
  const kinds = new Set(ALL_POLICY_MESSAGE_TYPES);
  let custom = false;

  if (scopeInclude) {
    const included = decodeEffectiveKindScope(scopeInclude);
    if (included) {
      const includedKinds = new Set(included);
      for (const kind of kinds) {
        if (!includedKinds.has(kind)) kinds.delete(kind);
      }
    } else {
      custom = true;
    }
  }

  if (scopeExempt) {
    const exempted = decodeEffectiveKindScope(scopeExempt);
    if (exempted) {
      for (const kind of exempted) {
        if (isPolicyMessageType(kind)) kinds.delete(kind);
      }
    } else {
      custom = true;
    }
  }

  return { kinds, custom };
}

/** The recommended scope for a category, or undefined when the category has no
 *  recommendation or is session-scoped (message scoping does not apply). */
export function categoryRecommendationScope(
  definition: CategoryScopeRecommendation | undefined,
): Scope | undefined {
  if (!definition || !definition.recommendedScopeApplicable) return undefined;

  const scopeInclude = definition.recommendedScopeInclude || undefined;
  const scopeExempt = definition.recommendedScopeExempt || undefined;
  return scopeInclude || scopeExempt
    ? { scopeInclude, scopeExempt }
    : undefined;
}

function definitionsByKey(
  definitions: CategoryScopeRecommendation[] | undefined,
): Map<string, CategoryScopeRecommendation> {
  return new Map((definitions ?? []).map((d) => [d.key, d]));
}

export function effectivePolicyScopeKinds({
  categories,
  detectionScopes = [],
  categoryDefinitions = [],
  messageTypes,
  scopeInclude,
  scopeExempt,
}: EffectivePolicyScopeInput): EffectivePolicyScope {
  const scopesByCategory = new Map(
    detectionScopes.map((scope) => [scope.category, scope]),
  );
  const definitions = definitionsByKey(categoryDefinitions);
  const kinds = new Set<PolicyMessageType>();
  let promptAttachmentInCategoryScope = false;
  let custom = false;

  for (const category of categories) {
    const definition = definitions.get(category);
    if (definition?.recommendedScopeApplicable === false) continue;

    const scope =
      scopesByCategory.get(category) ??
      categoryRecommendationScope(definition) ??
      {};
    const effective = effectiveScopeKinds(scope);
    for (const kind of effective.kinds) kinds.add(kind);
    promptAttachmentInCategoryScope ||= scopeAllowsPromptAttachment(scope);
    custom ||= effective.custom;
  }

  const policyScope = effectiveScopeKinds({ scopeInclude, scopeExempt });
  for (const kind of kinds) {
    if (!policyScope.kinds.has(kind)) kinds.delete(kind);
  }
  custom ||= policyScope.custom;

  if (messageTypes && messageTypes.length > 0) {
    const legacyKinds = new Set(messageTypes.filter(isPolicyMessageType));
    for (const kind of kinds) {
      if (!legacyKinds.has(kind)) kinds.delete(kind);
    }
    custom ||= messageTypes.some(
      (type) => !isPolicyMessageType(type) && type !== "prompt_attachment",
    );
  }

  const promptAttachmentSelected =
    !messageTypes?.length || messageTypes.includes("prompt_attachment");
  const promptAttachmentInPolicyScope = scopeAllowsPromptAttachment({
    scopeInclude,
    scopeExempt,
  });
  const additionalKinds = new Set<AdditionalPolicyMessageType>();
  if (
    promptAttachmentSelected &&
    promptAttachmentInCategoryScope &&
    promptAttachmentInPolicyScope
  ) {
    additionalKinds.add("prompt_attachment");
  }

  return { kinds, additionalKinds, custom };
}

/** Rewrite `source` so it covers exactly `kinds`, keeping whatever the kind
 *  list cannot express: a custom include is intersected, a custom exemption is
 *  carried forward. Decodable predicates are dropped — the kind list already
 *  says everything they said, so keeping them would fight the user's pick. */
export function narrowScopeToKinds(
  source: Scope | undefined,
  kinds: PolicyMessageType[],
): Scope {
  if (kinds.length === 0) return kindScopeForMessageTypes([]);

  const include = encodeKindScope(kinds);
  const customInclude =
    source?.scopeInclude && !decodeEffectiveKindScope(source.scopeInclude)
      ? source.scopeInclude
      : undefined;
  const customExempt =
    source?.scopeExempt && !decodeEffectiveKindScope(source.scopeExempt)
      ? source.scopeExempt
      : undefined;

  return {
    scopeInclude: customInclude ? `(${customInclude}) && ${include}` : include,
    ...(customExempt ? { scopeExempt: customExempt } : {}),
  };
}

/** Next `detection_scopes` for a policy whose `category` scope is being set to
 *  `kinds`.
 *
 *  Writing a category scope clears the policy's legacy `message_types` (an
 *  empty list means "all types" on the wire), so any narrowing that list
 *  carried is first pinned onto the policy's other categories — otherwise they
 *  silently widen. */
export function detectionScopesForCategoryEdit({
  category,
  kinds,
  policyCategories,
  detectionScopes,
  categoryDefinitions,
  messageTypes,
}: {
  category: string;
  kinds: PolicyMessageType[];
  policyCategories: Iterable<string>;
  detectionScopes?: CategoryScope[];
  categoryDefinitions?: CategoryScopeRecommendation[];
  messageTypes?: string[];
}): CategoryScope[] {
  const definitions = definitionsByKey(categoryDefinitions);
  const scopesByCategory = new Map(
    (detectionScopes ?? []).map((scope) => [scope.category, scope]),
  );
  const scopeFor = (cat: string): Scope | undefined =>
    scopesByCategory.get(cat) ??
    categoryRecommendationScope(definitions.get(cat));

  let next = detectionScopes ?? [];

  const legacyKinds = new Set((messageTypes ?? []).filter(isPolicyMessageType));
  const legacyNarrows =
    legacyKinds.size > 0 && legacyKinds.size < ALL_POLICY_MESSAGE_TYPES.length;
  if (legacyNarrows) {
    for (const cat of policyCategories) {
      if (cat === category) continue;
      if (definitions.get(cat)?.recommendedScopeApplicable === false) continue;

      const source = scopeFor(cat);
      const effective = effectiveScopeKinds(source ?? {});
      const pinned = ALL_POLICY_MESSAGE_TYPES.filter(
        (kind) => effective.kinds.has(kind) && legacyKinds.has(kind),
      );
      next = replaceCategoryDetectionScope(next, {
        category: cat,
        ...narrowScopeToKinds(source, pinned),
      });
    }
  }

  return replaceCategoryDetectionScope(next, {
    category,
    ...narrowScopeToKinds(scopeFor(category), kinds),
  });
}

const TOOL_CALL_MESSAGE_TYPES = new Set<PolicyMessageType>([
  "tool_request",
  "tool_response",
]);

export function hasOnlyToolCallMessageTypes(
  types: Set<PolicyMessageType>,
): boolean {
  return (
    types.size === TOOL_CALL_MESSAGE_TYPES.size &&
    [...types].every((type) => TOOL_CALL_MESSAGE_TYPES.has(type))
  );
}

export function messageTypesSummary(
  selectedMessageTypes: Set<PolicyMessageType>,
): string {
  if (selectedMessageTypes.size === ALL_POLICY_MESSAGE_TYPES.length) {
    return "All types";
  }

  if (hasOnlyToolCallMessageTypes(selectedMessageTypes)) {
    return "Tool Calls";
  }

  if (
    selectedMessageTypes.size === 1 &&
    selectedMessageTypes.has("tool_request")
  ) {
    return "Tool Requests";
  }

  return `${selectedMessageTypes.size} of ${ALL_POLICY_MESSAGE_TYPES.length} types selected`;
}

/** One-line scope description for the policy list. */
export function describePolicyScope({
  kinds,
  additionalKinds,
  custom,
}: EffectivePolicyScope): { summary: string; tooltip: string } {
  const types = ALL_POLICY_MESSAGE_TYPES.filter((type) => kinds.has(type));
  const labels = [
    ...types.map((type) => POLICY_MESSAGE_TYPE_META[type].label),
    ...(additionalKinds.has("prompt_attachment") ? ["Prompt Attachments"] : []),
  ];

  // A CEL predicate we could not decode may narrow or widen at scan time, so
  // the decoded kinds are an upper bound and must never be shown as the scope.
  if (custom) {
    return {
      summary: "Custom scope",
      tooltip: labels.length
        ? `Custom CEL scope. At most: ${labels.join(", ")}`
        : "Custom CEL scope",
    };
  }

  if (labels.length === 0) {
    return {
      summary: "Nothing in scope",
      tooltip: "No message types in scope",
    };
  }

  const typeSet = new Set(types);
  const summary =
    additionalKinds.size === 0 &&
    (typeSet.size === ALL_POLICY_MESSAGE_TYPES.length ||
      hasOnlyToolCallMessageTypes(typeSet))
      ? messageTypesSummary(typeSet)
      : labels.join(", ");

  return { summary, tooltip: labels.join(", ") };
}
