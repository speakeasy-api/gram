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
} {
  const { methods, products, capabilities } = catalog;
  const method = methods.find((item) => item.id === ids.methods);
  const product = products.find((item) => item.id === ids.platforms);
  const capability = capabilities.find((item) => item.id === ids.capabilities);
  const mapping =
    method && product
      ? (draft.mappings[mappingKey(method.id, product.id)] ?? emptyMapping)
      : emptyMapping;
  let fact = unknown;
  if (capability) {
    if (method && product)
      fact = getFact(
        mapping,
        capability.id,
        methodReference(draft, method, capability.id),
      );
    else if (method) fact = methodReference(draft, method, capability.id);
    else if (product)
      fact = summarize(
        methods.map((item) =>
          accountFact(
            item,
            getFact(
              draft.mappings[mappingKey(item.id, product.id)] ?? emptyMapping,
              capability.id,
              methodReference(draft, item, capability.id),
            ),
            account,
            draft.mappings[mappingKey(item.id, product.id)]?.conditions,
          ),
        ),
      );
  }
  if (method) fact = accountFact(method, fact, account, mapping.conditions);
  return { method, product, capability, mapping, fact };
}
