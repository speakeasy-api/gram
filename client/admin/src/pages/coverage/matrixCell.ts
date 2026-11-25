import { accountFact, type AccountFilter } from "./accounts";
import {
  emptyMapping,
  getFact,
  mappingKey,
  methodReference,
  summarize,
  unknown,
  type Draft,
  type Catalog,
  type Fact,
  type Mapping,
  type Method,
  type Product,
  type Capability,
} from "./model";

export type MethodContribution = { method: Method; fact: Fact };

export function resolveMatrixCell(
  draft: Draft,
  catalog: Catalog,
  ids: { methods?: string; platforms?: string; capabilities?: string },
  account: AccountFilter = "all",
): {
  method?: Method;
  product?: Product;
  capability?: Capability;
  mapping: Mapping;
  fact: Fact;
  contributions: MethodContribution[];
} {
  const { methods, products, capabilities } = catalog;
  const method = methods.find((item) => item.id === ids.methods);
  const product = products.find((item) => item.id === ids.platforms);
  const capability = capabilities.find((item) => item.id === ids.capabilities);
  const mapping =
    method && product
      ? (draft.mappings[mappingKey(method.id, product.id)] ?? emptyMapping)
      : emptyMapping;
  const candidates = method ? [method] : methods;
  const contributions: MethodContribution[] = [];
  if (capability && (method || product)) {
    for (const candidate of candidates) {
      const candidateMapping = product
        ? (draft.mappings[mappingKey(candidate.id, product.id)] ?? emptyMapping)
        : emptyMapping;
      const reference = methodReference(draft, candidate, capability.id);
      const claim = product
        ? getFact(candidateMapping, capability.id, reference)
        : reference;
      contributions.push({
        method: candidate,
        fact: accountFact(
          candidate,
          claim,
          account,
          candidateMapping.conditions,
        ),
      });
    }
  }
  let fact = unknown;
  if (method) fact = contributions[0]?.fact ?? unknown;
  else if (product && capability)
    fact = summarize(contributions.map((entry) => entry.fact));
  return { method, product, capability, mapping, fact, contributions };
}
