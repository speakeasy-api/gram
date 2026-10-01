import { z } from "zod";

// The support matrix as the server serves it: the file it was built with
// (server/internal/supportmatrix/matrix.yaml), with one explicit cell per
// capability wherever a method applies to a platform. Nothing here derives a
// status from prose; the file states every cell.

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
export const applicabilityLabels = {
  unknown: "Unknown",
  applicable: "Applies",
  na: "Does not apply",
} as const;
export type Applicability = keyof typeof applicabilityLabels;

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
export const notApplicable: Fact = {
  status: "na",
  note: "Method does not apply to this platform",
  verify: false,
};

export const eligibilities = ["supported", "unsupported", "unknown"] as const;
export type Eligibility = (typeof eligibilities)[number];
const osSupport = z.enum(["supported", "verify"]);
export type OSSupport = z.infer<typeof osSupport>;

const supportSchema = z.object({
  platform: z.string(),
  applicability: z.enum(["unknown", "applicable", "na"]),
  accounts: z.object({
    personal: z.enum(eligibilities),
    team: z.enum(eligibilities),
    enterprise: z.enum(eligibilities),
  }),
  os: z
    .object({
      mac: osSupport.optional(),
      windows: osSupport.optional(),
      linux: osSupport.optional(),
    })
    .optional(),
  note: z.string(),
  cells: z.record(z.string(), factSchema),
});
/** One method on one platform. */
export type PlatformSupport = z.infer<typeof supportSchema>;

const capabilitySchema = z.object({
  id: z.string(),
  name: z.string(),
  group: z.string(),
});
export type Capability = z.infer<typeof capabilitySchema>;
const platformSchema = z.object({
  id: z.string(),
  name: z.string(),
  vendor: z.string(),
  family: z.string(),
  surface: z.string(),
});
export type Platform = z.infer<typeof platformSchema>;
const methodSchema = z.object({
  id: z.string(),
  name: z.string(),
  vendor: z.string(),
  plans: z.string(),
  claims: z.record(z.string(), factSchema),
  platforms: z.array(supportSchema),
});
export type Method = z.infer<typeof methodSchema>;

export const catalogSchema = z.object({
  capabilities: z.array(capabilitySchema),
  platforms: z.array(platformSchema),
  methods: z.array(methodSchema),
});
export type Catalog = z.infer<typeof catalogSchema>;
export const snapshotSchema = catalogSchema.extend({ revision: z.string() });
export type Snapshot = z.infer<typeof snapshotSchema>;

/** Where the matrix lives, for readers who want to change it. */
export const matrixSourceURL =
  "https://github.com/speakeasy-api/gram/blob/main/server/internal/supportmatrix/matrix.yaml";

export function platformSupport(
  method: Method,
  platformId: string,
): PlatformSupport | undefined {
  return method.platforms.find((support) => support.platform === platformId);
}

/** The cell of a method on a platform, across every account type. A method
 * that does not apply is not applicable for every capability, and one whose
 * applicability is unknown is unknown for every capability. */
export function cellFact(
  support: PlatformSupport | undefined,
  capabilityId: string,
): Fact {
  if (!support || support.applicability === "unknown") return unknown;
  if (support.applicability === "na") return notApplicable;
  return support.cells[capabilityId] ?? unknown;
}

/** What a method claims for a capability, platform aside. */
export function claimFact(method: Method, capabilityId: string): Fact {
  return method.claims[capabilityId] ?? unknown;
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
