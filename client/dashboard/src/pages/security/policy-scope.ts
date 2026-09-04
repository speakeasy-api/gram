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

type EffectivePolicyScopeInput = {
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

  if (cel.startsWith("kind == ")) {
    try {
      const parsed: unknown = JSON.parse(cel.slice("kind == ".length));
      return typeof parsed === "string" &&
        isPolicyMessageType(parsed) &&
        `kind == ${JSON.stringify(parsed)}` === cel
        ? [parsed]
        : null;
    } catch {
      return null;
    }
  }

  return null;
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
}: EffectivePolicyScopeInput): {
  kinds: Set<PolicyMessageType>;
  custom: boolean;
} {
  const scopesByCategory = new Map(
    detectionScopes.map((scope) => [scope.category, scope]),
  );
  const definitionsByCategory = new Map(
    categoryDefinitions.map((definition) => [definition.key, definition]),
  );
  const kinds = new Set<PolicyMessageType>();
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
    custom ||= effective.custom;
  }

  if (messageTypes && messageTypes.length > 0) {
    const legacyKinds = new Set(messageTypes.filter(isPolicyMessageType));
    for (const kind of kinds) {
      if (!legacyKinds.has(kind)) kinds.delete(kind);
    }
  }

  return { kinds, custom };
}
