import {
  ALL_POLICY_MESSAGE_TYPES,
  type PolicyMessageType,
} from "./policy-data";

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
  /** Every category the policy detects is session-scoped: it inspects the
   *  session, not individual messages, so message scoping does not apply. */
  sessionScopedOnly: boolean;
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

/** Scope predicate that matches no message at all. `kind in []` compiles on
 *  the server (celenv) and decodes back to an empty kind list, so an empty
 *  selection round-trips instead of needing an exempt-everything companion. */
const NO_KINDS_SCOPE = "kind in []";

export function kindScopeForMessageTypes(types: PolicyMessageType[]): Scope {
  return {
    scopeInclude: types.length > 0 ? encodeKindScope(types) : NO_KINDS_SCOPE,
  };
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
  if (!Array.isArray(parsed)) return null;

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

  // Both predicates can only ever narrow, so an undecodable one tells us
  // nothing once the scope is already empty.
  return { kinds, custom: custom && kinds.size > 0 };
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

/** Session-scoped categories reject message scoping outright, and writing one
 *  fails the whole update, so never put one on the wire. Categories with no
 *  recommendation (`custom`) do accept a specified scope: a custom rule written
 *  over `content` matches every message kind, so its scope is the only thing
 *  narrowing it. */
export function acceptsDetectionScope(
  definition: CategoryScopeRecommendation | undefined,
): boolean {
  return definition !== undefined && definition.recommendedScopeApplicable;
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
  let categoryCount = 0;
  let sessionScopedCount = 0;

  for (const category of categories) {
    categoryCount++;
    const definition = definitions.get(category);
    if (definition?.recommendedScopeApplicable === false) {
      sessionScopedCount++;
      continue;
    }

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

  return {
    kinds,
    additionalKinds,
    // An undecodable predicate can only narrow, so it says nothing once the
    // scope is already empty.
    custom: custom && (kinds.size > 0 || additionalKinds.size > 0),
    sessionScopedOnly:
      categoryCount > 0 && sessionScopedCount === categoryCount,
  };
}

/** Rewrite `source` so it covers exactly `kinds`, keeping whatever the kind
 *  list cannot express: a custom include is intersected, a custom exemption is
 *  carried forward. Decodable predicates are dropped — the kind list already
 *  says everything they said, so keeping them would fight the user's pick.
 *
 *  An empty pick scopes to no kind at all rather than clearing the scope, so
 *  disabling every message type and re-enabling one restores the custom
 *  predicates instead of silently discarding them. */
/** The part of an include predicate a kind list cannot express, or undefined
 *  when the whole thing is one. Unwraps the `(custom) && kind in [...]` form
 *  this function writes, so re-narrowing replaces the old kind list instead of
 *  stacking a second one on top of it. */
function customIncludePart(include: string | undefined): string | undefined {
  if (!include || decodeEffectiveKindScope(include)) return undefined;

  const composed = /^\((.*)\) && (kind (?:in|==) .*)$/s.exec(include.trim());
  return composed && decodeEffectiveKindScope(composed[2]!)
    ? composed[1]
    : include;
}

export function narrowScopeToKinds(
  source: Scope | undefined,
  kinds: PolicyMessageType[],
): Scope {
  const include = kinds.length > 0 ? encodeKindScope(kinds) : NO_KINDS_SCOPE;
  const customInclude = customIncludePart(source?.scopeInclude);
  const customExempt =
    source?.scopeExempt && !decodeEffectiveKindScope(source.scopeExempt)
      ? source.scopeExempt
      : undefined;

  return {
    scopeInclude: customInclude ? `(${customInclude}) && ${include}` : include,
    ...(customExempt ? { scopeExempt: customExempt } : {}),
  };
}

/** The scope half of an update for a policy whose `category` scope is being set
 *  to `kinds`: the next `detection_scopes` and the next legacy `message_types`.
 *
 *  Writing a category scope wants the legacy `message_types` list gone (an
 *  empty list means "all types" on the wire), so whatever narrowing it carried
 *  is first pinned onto the policy's other categories. A category the API
 *  refuses a scope for cannot be pinned, so the legacy list survives there,
 *  widened only by the kinds the user just added. */
export function policyScopeUpdateForCategoryEdit({
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
}): { detectionScopes: CategoryScope[]; messageTypes: string[] } {
  const definitions = definitionsByKey(categoryDefinitions);
  const scopesByCategory = new Map(
    (detectionScopes ?? []).map((scope) => [scope.category, scope]),
  );
  const scopeFor = (cat: string): Scope | undefined =>
    scopesByCategory.get(cat) ??
    categoryRecommendationScope(definitions.get(cat));

  const siblings = [...policyCategories].filter((cat) => cat !== category);
  // Session-scoped categories ignore message scoping entirely, so the legacy
  // list was never narrowing them and they need no pin.
  const unpinnable = siblings.filter(
    (cat) =>
      !acceptsDetectionScope(definitions.get(cat)) &&
      definitions.get(cat)?.recommendedScopeApplicable !== false,
  );

  const legacyKinds = new Set((messageTypes ?? []).filter(isPolicyMessageType));
  const legacyNarrows =
    legacyKinds.size > 0 && legacyKinds.size < ALL_POLICY_MESSAGE_TYPES.length;

  let next = detectionScopes ?? [];
  if (legacyNarrows) {
    for (const cat of siblings) {
      if (!acceptsDetectionScope(definitions.get(cat))) continue;

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

  next = replaceCategoryDetectionScope(next, {
    category,
    ...narrowScopeToKinds(scopeFor(category), kinds),
  });

  return {
    detectionScopes: next,
    messageTypes:
      legacyNarrows && unpinnable.length > 0
        ? [...new Set([...(messageTypes ?? []), ...kinds])]
        : [],
  };
}
