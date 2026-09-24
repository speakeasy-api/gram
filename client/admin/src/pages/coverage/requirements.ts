import { accountFact, type AccountFilter } from "./accounts";
import {
  emptyMapping,
  getFact,
  mappingKey,
  methodReference,
  type Draft,
  type Method,
} from "./model";

export type CoverageTarget = { platformId: string; capabilityId: string };
export type Requirements = {
  combinations: string[][];
  gaps: CoverageTarget[];
  unknownCount: number;
  partialCount: number;
  provisional: boolean;
};

/** Cover all targets with known full support and report the rest as gaps.
 * Preserve inclusion-minimal alternatives, including larger sets that do not
 * contain an already sufficient combination. */
export function integrationRequirements(
  draft: Draft,
  targets: CoverageTarget[],
  candidates: Method[],
  account: AccountFilter = "all",
): Requirements {
  const providers = targets.map((target) =>
    candidates.map((method) => {
      const mapping =
        draft.mappings[mappingKey(method.id, target.platformId)] ??
        emptyMapping;
      return {
        method,
        fact: accountFact(
          draft,
          method,
          mapping,
          getFact(
            mapping,
            target.capabilityId,
            methodReference(draft, method, target.capabilityId),
          ),
          account,
        ),
      };
    }),
  );
  const gaps = targets.filter(
    (_, index) =>
      !providers[index]!.some(({ fact }) => fact.status === "supported"),
  );
  const result: Requirements = {
    combinations: [],
    gaps,
    unknownCount: providers.filter(
      (entries) =>
        !entries.some(({ fact }) => fact.status === "supported") &&
        entries.some(({ fact }) => fact.status === "unknown"),
    ).length,
    partialCount: providers.filter(
      (entries) =>
        !entries.some(({ fact }) => fact.status === "supported") &&
        entries.some(({ fact }) => fact.status === "partial"),
    ).length,
    provisional: false,
  };
  const coveredProviders = providers.filter((entries) =>
    entries.some(({ fact }) => fact.status === "supported"),
  );
  if (!coveredProviders.length) return result;
  // Build covers one target at a time; remove duplicate sets and supersets.
  let covers: Set<string>[] = [new Set()];
  for (const entries of coveredProviders) {
    const options = entries
      .filter(({ fact }) => fact.status === "supported")
      .map(({ method }) => method.id);
    const expanded = covers
      .flatMap((cover) => options.map((id) => new Set([...cover, id])))
      .sort((a, b) => a.size - b.size);
    const minimal: Set<string>[] = [];
    for (const cover of expanded) {
      if (
        !minimal.some((existing) => [...existing].every((id) => cover.has(id)))
      )
        minimal.push(cover);
    }
    covers = minimal;
  }
  result.combinations = covers.map((cover) =>
    candidates
      .filter((method) => cover.has(method.id))
      .map((method) => method.id),
  );
  result.provisional = covers.some((cover) =>
    coveredProviders.some(
      (entries) =>
        !entries.some(
          ({ method, fact }) =>
            cover.has(method.id) && fact.status === "supported" && !fact.verify,
        ),
    ),
  );
  return result;
}
