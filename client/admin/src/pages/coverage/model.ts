import { z } from "zod";

export const statusLabels = {
  supported: "Supported",
  partial: "Partial",
  unimplemented: "Not implemented",
  impossible: "Not possible",
  na: "Not applicable",
  unknown: "Unknown",
} as const;
export type Status = keyof typeof statusLabels;
export const symbols: Record<Status, string> = {
  supported: "✓",
  partial: "◐",
  unimplemented: "×",
  impossible: "⊘",
  na: "—",
  unknown: "?",
};
export type Capability = { id: string; name: string; group: string };
export type Product = {
  id: string;
  name: string;
  vendor: string;
  family: string;
  surface: string;
};
const factSchema = z.object({
  status: z.enum([
    "supported",
    "partial",
    "unimplemented",
    "impossible",
    "na",
    "unknown",
  ]),
  note: z.string(),
  verify: z.boolean(),
});
export type Fact = z.infer<typeof factSchema>;
export const unknown: Fact = { status: "unknown", note: "", verify: false };
/** Which account types can use a method, keyed by account type. On a method an
 * absent account type is unknown; on a platform mapping it defers to the
 * method, so a mapping only carries the account types it differs on. */
export const accountsSchema = z.record(
  z.string(),
  z.enum(["supported", "unsupported", "unknown"]),
);
export type Accounts = z.infer<typeof accountsSchema>;
export type Method = {
  id: string;
  name: string;
  vendor: string;
  plans: string;
  accounts: Accounts;
  facts: Record<string, Fact>;
};
export const mappingSchema = z.object({
  applicability: z.enum(["unknown", "applicable", "na"]),
  conditions: z.string(),
  accounts: accountsSchema,
  facts: z.record(z.string(), factSchema),
});
export type Mapping = z.infer<typeof mappingSchema>;
export const emptyMapping: Mapping = {
  applicability: "unknown",
  conditions: "",
  accounts: {},
  facts: {},
};
export const draftSchema = z.object({
  mappings: z.record(z.string(), mappingSchema),
  references: z.record(z.string(), z.record(z.string(), factSchema)),
  accounts: z.record(z.string(), accountsSchema),
});
export type Draft = z.infer<typeof draftSchema>;
export const storageKey = "gram-integration-coverage-v1";
export function mappingKey(methodId: string, productId: string): string {
  return `${methodId}/${productId}`;
}
export function getFact(
  mapping: Mapping,
  capabilityId: string,
  reference: Fact = unknown,
): Fact {
  if (mapping.applicability === "na")
    return {
      status: "na",
      note: "Method does not apply to this product",
      verify: false,
    };
  if (mapping.applicability === "unknown") return unknown;
  const explicit = mapping.facts[capabilityId];
  if (explicit) return explicit;
  const conditions = mapping.conditions;
  const note = ["Derived from method reference", reference.note, conditions]
    .filter(Boolean)
    .join("; ");
  const verify =
    reference.verify || /\b(?:verify|wip|maybe)\b|\?/i.test(conditions);
  // Platform qualifiers narrow the method's general capability claims.
  if (
    (/\bcost only\b/i.test(conditions) && capabilityId !== "cost") ||
    (/\bsession tracking only\b/i.test(conditions) &&
      capabilityId !== "session")
  )
    return { status: "na", note, verify };
  if (
    /\bno hooks\b/i.test(conditions) &&
    /\b(?:via )?hooks\b/i.test(reference.note)
  )
    return { status: "unimplemented", note, verify };
  if (/\bwip\b/i.test(conditions) && reference.status === "supported")
    return { status: "partial", note, verify: true };
  return { ...reference, note, verify };
}

export function methodReference(
  draft: Draft,
  method: Method,
  capabilityId: string,
): Fact {
  return (
    draft.references[method.id]?.[capabilityId] ??
    method.facts[capabilityId] ??
    unknown
  );
}
export function methodAccounts(draft: Draft, method: Method): Accounts {
  return draft.accounts[method.id] ?? method.accounts;
}
export function summarize(facts: Fact[]): Fact {
  // A known positive is useful, but unknown methods must not yield a negative claim.
  const supported = facts.filter((fact) => fact.status === "supported");
  if (supported.length)
    return {
      status: "supported",
      note: `${supported.length} method${supported.length === 1 ? "" : "s"}`,
      verify: supported.every((fact) => fact.verify),
    };
  const partial = facts.filter((fact) => fact.status === "partial");
  if (partial.length)
    return {
      status: "partial",
      note: `${partial.length} partial method${partial.length === 1 ? "" : "s"}`,
      verify: partial.every((fact) => fact.verify),
    };
  if (!facts.length || facts.some((fact) => fact.status === "unknown"))
    return unknown;
  const applicable = facts.filter((fact) => fact.status !== "na");
  if (!applicable.length) return { ...unknown, status: "na" };
  if (applicable.every((fact) => fact.status === "impossible"))
    return { ...unknown, status: "impossible" };
  return { ...unknown, status: "unimplemented" };
}

export const catalogSchema = z.object({
  methods: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      vendor: z.string(),
      plans: z.string(),
      accounts: accountsSchema,
      facts: z.record(z.string(), factSchema),
    }),
  ),
  products: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      vendor: z.string(),
      family: z.string(),
      surface: z.string(),
    }),
  ),
  capabilities: z.array(
    z.object({ id: z.string(), name: z.string(), group: z.string() }),
  ),
});
export type Catalog = z.infer<typeof catalogSchema>;
export const snapshotSchema = catalogSchema.extend({
  draft: draftSchema,
  revision: z.string(),
});
export type Snapshot = z.infer<typeof snapshotSchema>;
