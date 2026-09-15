import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPAccessSummary } from "@gram/client/models/components/shadowmcpaccesssummary.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { type BadgeProps } from "@/components/ui/Badge";

export type ShadowMCPPolicyState =
  | "blocking"
  | "warning"
  | "flagging"
  | "none"
  | "unavailable";

export type ShadowMCPInventoryStatus =
  | "allowed"
  | "blocked"
  | "restricted"
  | "observed"
  | "pending";

/**
 * The slice of a server row the status helpers read: the server-computed
 * verdict plus the open-request count. Everything the badge and its subtext
 * say comes from these two fields — the client never re-derives enforcement
 * from policy lists or review status, which is how the inventory used to
 * over-report both access and control.
 */
type ShadowMCPInventoryAccess = Pick<
  ShadowMCPInventoryServer,
  "access" | "accessSummary" | "requestCount"
>;

/**
 * The verdict to render from. accessSummary is optional for one release so a
 * dashboard deployed ahead of a rolled-back server keeps working: absent, we
 * synthesize a coarse summary from the legacy access string — right badge,
 * generic wording — instead of failing the page.
 */
export function shadowMCPAccessSummaryOf(
  server: ShadowMCPInventoryAccess,
): ShadowMCPAccessSummary {
  if (server.accessSummary) return server.accessSummary;
  switch (server.access) {
    case "allowed":
      return {
        state: "allowed",
        allowedFor: "everyone",
        blockedFor: "none",
        blockingDefault: "deny",
        decision: undefined,
        decisionCoverage: "none",
      };
    case "blocked":
      return {
        state: "blocked",
        allowedFor: "none",
        blockedFor: "none",
        blockingDefault: "deny",
        decision: undefined,
        decisionCoverage: "none",
      };
    case "restricted":
      return {
        state: "restricted",
        allowedFor: "none",
        blockedFor: "some",
        blockingDefault: "allow",
        decision: undefined,
        decisionCoverage: "none",
      };
    case "none":
      return {
        state: "unenforced",
        allowedFor: "none",
        blockedFor: "none",
        blockingDefault: "none",
        decision: undefined,
        decisionCoverage: "none",
      };
  }
}

export type ShadowMCPPolicyDisposition = "block_all" | "allow_all";

export type ShadowMCPPolicy = Pick<
  RiskPolicy,
  | "audienceType"
  | "audiencePrincipalUrns"
  | "id"
  | "name"
  | "shadowMcpDisposition"
>;

/**
 * The effective disposition of the project's enabled blocking shadow MCP
 * policies, or null when none exist. Feeds the Decide Access sheet's form
 * wording and write path — row rendering reads the server-computed access
 * summary instead. With legacy multi-policy data, deny-by-default wins:
 * allow_all only applies when every blocking policy declares it.
 */
export function shadowMCPBlockingPolicyDisposition(
  policies: Pick<RiskPolicy, "shadowMcpDisposition">[],
): ShadowMCPPolicyDisposition | null {
  if (policies.length === 0) return null;
  return policies.every((policy) => policy.shadowMcpDisposition === "allow_all")
    ? "allow_all"
    : "block_all";
}

export function eligibleShadowMCPAllowRulePolicies(
  policies: RiskPolicy[] | undefined,
): RiskPolicy[] {
  return (
    policies?.filter(
      (policy) =>
        policy.enabled &&
        policy.action === "block" &&
        policy.sources.includes("shadow_mcp"),
    ) ?? []
  );
}

/**
 * The page-level posture banner (Blocking / Flagging / No Policy). This is
 * about the policy set, not any one server — per-row state comes from the
 * server-computed access summary instead.
 */
export function shadowMCPPolicyState(
  policies: RiskPolicy[] | undefined,
): ShadowMCPPolicyState {
  if (!policies) return "unavailable";

  const shadowPolicies = policies.filter(
    (policy) => policy.enabled && policy.sources.includes("shadow_mcp"),
  );

  if (shadowPolicies.some((policy) => policy.action === "block")) {
    return "blocking";
  }

  if (shadowPolicies.some((policy) => policy.action === "warn")) {
    return "warning";
  }

  if (shadowPolicies.some((policy) => policy.action === "flag")) {
    return "flagging";
  }

  return "none";
}

export function shadowMCPInventoryStatus(
  server: ShadowMCPInventoryAccess,
): ShadowMCPInventoryStatus {
  // Open requests outrank the steady state: a server someone is waiting on
  // surfaces for triage even when it is already allowed.
  if (server.requestCount > 0) return "pending";
  switch (shadowMCPAccessSummaryOf(server).state) {
    case "allowed":
      return "allowed";
    case "blocked":
      return "blocked";
    case "restricted":
      return "restricted";
    case "unenforced":
      return "observed";
  }
}

export function shadowMCPInventoryStatusLabel(
  status: ShadowMCPInventoryStatus,
): string {
  switch (status) {
    case "allowed":
      return "Allowed";
    case "blocked":
      return "Blocked";
    case "restricted":
      return "Restricted";
    case "observed":
      return "Observed";
    case "pending":
      return "Pending";
  }
}

export function shadowMCPInventoryStatusBadgeVariant(
  status: ShadowMCPInventoryStatus,
): BadgeProps["variant"] {
  switch (status) {
    case "allowed":
      return "success";
    case "blocked":
      return "destructive";
    case "restricted":
      return "warning";
    case "observed":
      return "neutral";
    case "pending":
      // Blue, not orange: pending means "awaiting a decision", the same
      // vocabulary as the review's own badge, and orange stays reserved for
      // the partial-access family.
      return "information";
  }
}

export function shadowMCPInventoryStatusDescription(
  server: ShadowMCPInventoryAccess,
): string {
  if (server.requestCount > 0) {
    return `${server.requestCount} access ${server.requestCount === 1 ? "request" : "requests"} pending`;
  }
  // The skew fallback knows the legacy verdict but not the mechanism — an
  // allow-by-default project would be mis-credited to "policy" or "URL rule".
  // Say only what the old field actually knew.
  if (!server.accessSummary) {
    switch (shadowMCPAccessSummaryOf(server).state) {
      case "allowed":
        return "Allowed";
      case "blocked":
        return "Blocked";
      case "restricted":
        return "Access varies by user";
      case "unenforced":
        return "Not blocking";
    }
  }
  return describeShadowMCPAccess(server.accessSummary);
}

/**
 * One line naming the mechanism behind the verdict. A pure lookup on the
 * server-computed summary: state says the shape, the mechanism fields say
 * why, and a decision is credited only as far as enforcement carries it —
 * a denial no policy can enforce reads as dormant, never as a block.
 */
function describeShadowMCPAccess(summary: ShadowMCPAccessSummary): string {
  switch (summary.state) {
    case "allowed": {
      // A denial the current rules no longer carry must not vanish under a
      // green badge — and the subtext names what displaced it: an allow rule
      // that wins over the denial, or the denial's own block rule having
      // been removed (a denial under allow-by-default always writes one, so
      // allowed-yet-denied means it is gone).
      if (summary.decision === "denied") {
        return summary.allowedFor !== "none"
          ? "Denied by review, but overridden by an allow rule"
          : "Denied by review, but its block rule was removed";
      }
      // Credit the review only while its own grants carry the allow; an
      // approval whose grants were removed leaves the server allowed by
      // whatever mechanism actually remains.
      if (
        summary.decision === "approved" &&
        summary.decisionCoverage === "full"
      ) {
        return "Allowed by review";
      }
      if (summary.allowedFor === "everyone") return "Allowed by URL rule";
      return "Allowed by default";
    }
    case "blocked": {
      if (summary.decision === "denied") {
        // Under deny-by-default the policy already blocked it and the denial
        // compounds that; under allow-by-default the denial is the whole
        // block.
        return summary.blockingDefault === "deny"
          ? "Blocked by policy & review"
          : "Blocked by review";
      }
      if (summary.decision === "approved") {
        // The symmetric contradiction, naming the mechanism: an explicit
        // block rule that wins over the approval, or the approval's own
        // grants having been removed (an approval always writes them, so
        // blocked-yet-approved with no block rule means they are gone).
        return summary.blockedFor !== "none"
          ? "Approved by review, but overridden by a block rule"
          : "Approved by review, but its allow grants were removed";
      }
      return summary.blockingDefault === "deny"
        ? "Blocked by policy"
        : "Blocked by rule";
    }
    case "restricted": {
      // A denial only a targeted policy carries is not a project-wide block,
      // and saying "Blocked" here would over-report control the same way
      // "Allowed" used to over-report access.
      if (summary.decision === "denied") {
        // Two ways a denial leaves the row only restricted: standing grants
        // from an earlier approval still let their people through, or the
        // only enforcement is a policy that never covered everyone.
        return summary.allowedFor !== "none"
          ? "Denied by review, but standing grants still allow some users"
          : "Denied by review, but enforced only for the policy's audience";
      }
      const hasScopedAllow = summary.allowedFor !== "none";
      const hasExplicitBlock = summary.blockedFor !== "none";
      if (hasScopedAllow && hasExplicitBlock) {
        return "Allowed for some, blocked for others";
      }
      if (hasScopedAllow) {
        return "Allowed for selected users";
      }
      return "Blocked for some users";
    }
    case "unenforced": {
      // A decision with nothing to enforce it is dormant, not done — the one
      // state here that should worry an admin, so it says so.
      if (summary.decision === "denied") {
        return "Denied by review, but not enforced until a blocking policy exists";
      }
      if (summary.decision === "approved") {
        return "Approved by review, but not enforced until a blocking policy exists";
      }
      return "Not blocking";
    }
  }
}
