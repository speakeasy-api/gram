import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { ShadowMCPPolicyDisposition } from "./shadowMCPInventoryStatus";
import type { ShadowMCPMode } from "./shadowMCPMode";

export type StandingDecision = "approved" | "denied";

/** The decision an administrator recorded on a server, if any. */
export function standingDecisionOf(
  server: Pick<ShadowMCPInventoryServer, "accessSummary" | "approvalRequest">,
): StandingDecision | undefined {
  const decision: string | undefined =
    server.accessSummary?.decision ?? server.approvalRequest?.standingDecision;
  if (decision === "approved" || decision === "denied") return decision;
  return undefined;
}

/**
 * The servers whose behaviour a policy change flips. A server someone
 * approved or denied for everyone keeps that decision whatever the default
 * is, a linked server keeps sending agents to its official version, and a
 * local command has no URL a policy can enforce against (its decision is
 * recorded, never enforced). Everything else follows the default, so it
 * changes when the default does, including a server whose decision covers
 * only some people: the default still decides for everyone else.
 */
export function serversChangedByPolicy(
  servers: readonly ShadowMCPInventoryServer[],
  from: ShadowMCPMode,
  to: ShadowMCPMode,
): ShadowMCPInventoryServer[] {
  if (from === to) return [];
  return sortForPolicyChange(
    servers.filter((server) => {
      if (server.targetKind === "stdio_command") return false;
      if (standingDecisionOf(server) && !isScopedDecision(server)) {
        return false;
      }
      if (server.officialServer && !server.officialServer.directAccessAllowed) {
        return false;
      }
      return true;
    }),
  );
}

/**
 * A decision that covers only some people: an approval whose grants free
 * only part of the audience, or a block rule for some people. The default
 * still decides for everyone else, so a policy change reaches them.
 */
export function isScopedDecision(
  server: Pick<ShadowMCPInventoryServer, "accessSummary" | "approvalRequest">,
): boolean {
  const decision = standingDecisionOf(server);
  if (decision === "approved") {
    return server.accessSummary?.allowedFor === "selected";
  }
  if (decision === "denied") {
    // Under allow by default a narrowed denial is a block rule for some
    // people. Under a block-by-default policy "some" can also mean the
    // policy's own audience, which says nothing about the decision.
    return (
      server.accessSummary?.blockedFor === "some" &&
      server.accessSummary.blockingDefault === "none"
    );
  }
  return false;
}

/** Most blocked calls first, then most used: the servers people notice. */
function sortForPolicyChange(
  servers: readonly ShadowMCPInventoryServer[],
): ShadowMCPInventoryServer[] {
  return [...servers].sort(
    (a, b) =>
      b.blockedCallCount - a.blockedCallCount ||
      b.observedUseCount - a.observedUseCount ||
      shadowMCPServerLabel(a).localeCompare(shadowMCPServerLabel(b)),
  );
}

export function shadowMCPServerLabel(
  server: Pick<
    ShadowMCPInventoryServer,
    "serverName" | "urlHost" | "canonicalServerUrl"
  >,
): string {
  return server.serverName || server.urlHost || server.canonicalServerUrl;
}

/**
 * The decision that keeps a server working as it does today when the
 * default flips: approve it before blocking by default, deny it before
 * allowing by default.
 */
export function keepAsTodayDecision(to: ShadowMCPMode): StandingDecision {
  return to === "blocked" ? "approved" : "denied";
}

/** The one-line policy summary shown at the top of the Shadow MCP page. */
export function shadowMCPPolicySentence(
  mode: ShadowMCPMode,
  narrowedTo: string | null,
): string {
  if (mode === "allowed") return "All Shadow MCPs are allowed by default.";
  // A block-by-default rule narrowed to an empty audience covers nobody.
  if (narrowedTo === "") {
    return "Block by default applies to nobody, so Shadow MCPs are allowed by default.";
  }
  if (narrowedTo) {
    return `Shadow MCPs are blocked by default for ${narrowedTo}. Everyone else is allowed by default.`;
  }
  return "All Shadow MCPs are blocked by default.";
}

/**
 * The one action worth offering first. Under allow by default a server
 * already connects, so the action is to block it; under block by default it
 * is already blocked, so the action is to approve it. A server that already
 * carries that decision is offered the reverse, so the action always changes
 * something. The other decision stays reachable under Advanced.
 */
export function applicableDecision(
  disposition: ShadowMCPPolicyDisposition | null,
  currentDecision: StandingDecision | undefined,
): StandingDecision {
  const fromDefault: StandingDecision =
    disposition === "block_all" ? "approved" : "denied";
  if (currentDecision !== fromDefault) return fromDefault;
  return fromDefault === "approved" ? "denied" : "approved";
}

/** The row and button label for the applicable action. */
export function decisionActionLabel(decision: StandingDecision): string {
  return decision === "approved" ? "Approve server" : "Block server";
}
