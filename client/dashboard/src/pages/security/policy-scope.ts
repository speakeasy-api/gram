import type { PolicyMessageType } from "./policy-data";
import { ALL_POLICY_MESSAGE_TYPES } from "./policy-form";

type Scope = {
  scopeInclude?: string;
  scopeExempt?: string;
};

type CategoryScope = Scope & {
  category: string;
};

type CategoryScopeRecommendation = {
  key: string;
  recommendedScopeApplicable: boolean;
  recommendedScopeInclude?: string;
  recommendedScopeExempt?: string;
};

type AdditionalPolicyMessageType = "prompt_attachment";

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

function decodeKindEquality(cel: string): PolicyMessageType | null {
  if (!cel.startsWith("kind == ")) return null;

  try {
    const parsed: unknown = JSON.parse(cel.slice("kind == ".length));
    return typeof parsed === "string" &&
      isPolicyMessageType(parsed) &&
      `kind == ${JSON.stringify(parsed)}` === cel
      ? parsed
      : null;
  } catch {
    return null;
  }
}

export function decodeKindScope(cel: string): PolicyMessageType[] | null {
  if (cel.startsWith("kind in ")) {
    try {
      const parsed: unknown = JSON.parse(cel.slice("kind in ".length));
      if (
        !Array.isArray(parsed) ||
        parsed.length === 0 ||
        !parsed.every(
          (value): value is PolicyMessageType =>
            typeof value === "string" && isPolicyMessageType(value),
        )
      ) {
        return null;
      }
      return encodeKindScope(parsed) === cel ? parsed : null;
    } catch {
      return null;
    }
  }

  const equality = decodeKindEquality(cel);
  if (equality) return [equality];

  const terms = cel.split(" || ");
  if (terms.length > 1) {
    const values = terms.map(decodeKindEquality);
    if (
      values.every((value): value is PolicyMessageType => value !== null) &&
      new Set(values).size === values.length
    ) {
      return values;
    }
  }

  return null;
}

function promptAttachmentScopeMatch(cel: string): boolean | null {
  if (cel === 'kind == "prompt_attachment"') return true;
  if (decodeKindScope(cel)) return false;

  if (cel.startsWith("kind in ")) {
    try {
      const parsed: unknown = JSON.parse(cel.slice("kind in ".length));
      if (
        !Array.isArray(parsed) ||
        !parsed.every((value) => typeof value === "string")
      ) {
        return null;
      }
      return parsed.includes("prompt_attachment");
    } catch {
      return null;
    }
  }

  const terms = cel.split(" || ");
  if (terms.length > 1) {
    if (terms.every((term) => decodeKindEquality(term) !== null)) return false;
    if (terms.every((term) => term.startsWith("kind == "))) {
      return terms.includes('kind == "prompt_attachment"');
    }
  }

  return null;
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
    const included = decodeKindScope(scopeInclude);
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
    const exempted = decodeKindScope(scopeExempt);
    if (exempted) {
      for (const kind of exempted) kinds.delete(kind);
    } else {
      custom = true;
    }
  }

  return { kinds, custom };
}

export function effectivePolicyScopeKinds({
  categories,
  detectionScopes = [],
  categoryDefinitions = [],
  messageTypes,
  scopeInclude,
  scopeExempt,
}: EffectivePolicyScopeInput): {
  kinds: Set<PolicyMessageType>;
  additionalKinds: Set<AdditionalPolicyMessageType>;
  custom: boolean;
} {
  const scopesByCategory = new Map(
    detectionScopes.map((scope) => [scope.category, scope]),
  );
  const definitionsByCategory = new Map(
    categoryDefinitions.map((definition) => [definition.key, definition]),
  );
  const kinds = new Set<PolicyMessageType>();
  let promptAttachmentInCategoryScope = false;
  let custom = false;

  for (const category of categories) {
    const definition = definitionsByCategory.get(category);
    if (definition?.recommendedScopeApplicable === false) continue;

    const override = scopesByCategory.get(category);
    const effective = effectiveScopeKinds(
      override ?? {
        scopeInclude: definition?.recommendedScopeInclude,
        scopeExempt: definition?.recommendedScopeExempt,
      },
    );
    for (const kind of effective.kinds) kinds.add(kind);
    promptAttachmentInCategoryScope ||= scopeAllowsPromptAttachment(
      override ?? {
        scopeInclude: definition?.recommendedScopeInclude,
        scopeExempt: definition?.recommendedScopeExempt,
      },
    );
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
