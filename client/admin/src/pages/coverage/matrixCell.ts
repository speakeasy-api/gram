import { accountFact, methodAccountFact, type AccountFilter } from "./accounts";
import {
  cellFact,
  claimFact,
  platformSupport,
  summarize,
  unknown,
  type Capability,
  type Catalog,
  type Fact,
  type Method,
  type Platform,
  type PlatformSupport,
} from "./model";

export type MethodContribution = { method: Method; fact: Fact };

export type MatrixCell = {
  method?: Method;
  platform?: Platform;
  capability?: Capability;
  /** The method's entry for the platform, when both are named. */
  support?: PlatformSupport;
  fact: Fact;
  contributions: MethodContribution[];
};

/** Resolve one cell of the explorer. Naming a method and a platform without a
 * capability yields their applicability; naming a platform and a capability
 * without a method summarises every method's cell. */
export function resolveMatrixCell(
  catalog: Catalog,
  ids: { methods?: string; platforms?: string; capabilities?: string },
  account: AccountFilter = "all",
): MatrixCell {
  const { methods, platforms, capabilities } = catalog;
  const method = methods.find((item) => item.id === ids.methods);
  const platform = platforms.find((item) => item.id === ids.platforms);
  const capability = capabilities.find((item) => item.id === ids.capabilities);
  const support =
    method && platform ? platformSupport(method, platform.id) : undefined;
  const candidates = method ? [method] : methods;
  const contributions: MethodContribution[] = [];
  if (capability && (method || platform)) {
    for (const candidate of candidates) {
      if (platform) {
        const candidateSupport = platformSupport(candidate, platform.id);
        contributions.push({
          method: candidate,
          fact: accountFact(
            candidateSupport,
            cellFact(candidateSupport, capability.id),
            account,
          ),
        });
      } else {
        contributions.push({
          method: candidate,
          fact: methodAccountFact(
            candidate,
            claimFact(candidate, capability.id),
            account,
          ),
        });
      }
    }
  }
  let fact = unknown;
  if (method) fact = contributions[0]?.fact ?? unknown;
  else if (platform && capability)
    fact = summarize(contributions.map((entry) => entry.fact));
  return { method, platform, capability, support, fact, contributions };
}

const osNames = { mac: "Mac", windows: "Windows", linux: "Linux" } as const;

/** What is known per operating system, or nothing when nothing is. */
export function osSummary(cell: MatrixCell): string {
  const os = cell.support?.os;
  if (!os) return "";
  return (Object.keys(osNames) as (keyof typeof osNames)[])
    .filter((name) => os[name])
    .map((name) => `${osNames[name]}${os[name] === "verify" ? "*" : ""}`)
    .join(" · ");
}
