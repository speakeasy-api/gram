import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

type OriginalPolicy = Pick<RiskPolicy, "action" | "enabled" | "sources">;

/** Default disposition of a shadow MCP blocking policy. block_all denies every
 * server unless allowed (the original behavior); allow_all permits every
 * server unless it appears on the policy's blocked-URL list. Immutable after
 * create. */
export type ShadowMCPDisposition = "block_all" | "allow_all";

export function isShadowMCPBlockConfiguration(
  sources: readonly string[],
  action: string,
): boolean {
  return action === "block" && sources.includes("shadow_mcp");
}

export function isBlockingShadowMCPPolicy(
  enabled: boolean,
  sources: readonly string[],
  action: string,
): boolean {
  return enabled && isShadowMCPBlockConfiguration(sources, action);
}

export function shadowMCPAllowedURLsForMutation({
  action,
  selectedCategories,
  selectedURLs,
  originalPolicy,
  disposition = "block_all",
}: {
  action: string;
  selectedCategories: ReadonlySet<string>;
  selectedURLs: ReadonlySet<string>;
  originalPolicy: OriginalPolicy | null;
  disposition?: ShadowMCPDisposition;
}): string[] | undefined {
  // Allowed URLs are a block_all concept; an allow_all policy carries a
  // blocked list instead (shadowMCPBlockedURLsForMutation) and the server
  // rejects allowed URLs on it.
  if (disposition === "allow_all") return undefined;

  const targetIsShadowMCPBlock = isBlockingShadowMCPPolicy(
    true,
    [...selectedCategories],
    action,
  );
  if (targetIsShadowMCPBlock) return [...selectedURLs].sort();

  if (
    originalPolicy &&
    isShadowMCPBlockConfiguration(originalPolicy.sources, originalPolicy.action)
  ) {
    return [];
  }

  return undefined;
}

export function shadowMCPBlockedURLsForMutation({
  action,
  selectedCategories,
  selectedURLs,
  disposition,
}: {
  action: string;
  selectedCategories: ReadonlySet<string>;
  selectedURLs: ReadonlySet<string>;
  disposition: ShadowMCPDisposition;
}): string[] | undefined {
  // The blocked list only exists on allow_all blocking shadow MCP policies.
  // There is no clear-on-morph branch like the allowed-URL helper has: the
  // server rejects source/action changes on a policy with a stored
  // disposition, so an allow_all policy can never stop being one.
  if (disposition !== "allow_all") return undefined;
  const targetIsShadowMCPBlock = isBlockingShadowMCPPolicy(
    true,
    [...selectedCategories],
    action,
  );
  if (!targetIsShadowMCPBlock) return undefined;
  return [...selectedURLs].sort();
}

export function shadowMCPSelectionIsDirty(
  targetIsShadowMCPBlock: boolean,
  selectedURLs: ReadonlySet<string>,
  originalURLs: ReadonlySet<string> | null,
): boolean {
  if (!targetIsShadowMCPBlock || originalURLs === null) return false;
  if (selectedURLs.size !== originalURLs.size) return true;

  for (const url of selectedURLs) {
    if (!originalURLs.has(url)) return true;
  }
  return false;
}

export function shadowMCPSelectionIsInitialized(
  targetIsShadowMCPBlock: boolean,
  initializedEditorIdentity: string | null,
  editorIdentity: string,
): boolean {
  return (
    !targetIsShadowMCPBlock || initializedEditorIdentity === editorIdentity
  );
}

export interface ShadowMCPDecisionConflict {
  canonicalServerUrl: string;
  serverName?: string;
  decision: "approved" | "denied";
}

/**
 * The standing review decisions this edit's URL toggles contradict —
 * unchecking an approved server, allow-listing a denied one, block-listing
 * an approved one, or unblocking a denied one. Mirrors the server's own
 * conflict check so the confirm dialog can open before the save round-trips;
 * the server independently rejects an unconfirmed contradicting save, so a
 * miss here (e.g. a reopened request whose prior decision still stands)
 * degrades to the error toast, never to a silent supersession.
 */
export function shadowMCPDecisionConflicts({
  servers,
  originalURLs,
  selectedURLs,
  disposition,
}: {
  servers: readonly ShadowMCPInventoryServer[];
  originalURLs: ReadonlySet<string> | null;
  selectedURLs: ReadonlySet<string>;
  disposition: ShadowMCPDisposition;
}): ShadowMCPDecisionConflict[] {
  if (originalURLs === null) return [];

  const conflicts: ShadowMCPDecisionConflict[] = [];
  for (const server of servers) {
    const decision = standingDecisionOf(server.approvalRequest);
    if (decision === undefined) continue;

    const url = server.canonicalServerUrl;
    const has = originalURLs.has(url);
    const wants = selectedURLs.has(url);
    if (has === wants) continue;

    // On an allow list (block_all) removing revokes access and adding grants
    // it; on a block list (allow_all) the directions invert.
    let removingAllow = has && !wants;
    if (disposition === "allow_all") removingAllow = !removingAllow;

    const contradicted =
      (removingAllow && decision === "approved") ||
      (!removingAllow && decision === "denied");
    if (!contradicted) continue;

    conflicts.push({
      canonicalServerUrl: url,
      serverName: server.serverName,
      decision,
    });
  }

  return conflicts.sort((a, b) =>
    a.canonicalServerUrl.localeCompare(b.canonicalServerUrl),
  );
}

/**
 * The decision still standing for a review, independent of its lifecycle
 * status — a reopened request's prior decision keeps enforcing until
 * re-decided, so an edit contradicting it must still confirm. Prefers the
 * server-computed standing_decision; a server one release behind omits it,
 * where the lifecycle status carries the decision except for reopened rows.
 */
function standingDecisionOf(
  request: ShadowMCPInventoryServer["approvalRequest"],
): "approved" | "denied" | undefined {
  if (!request) return undefined;
  if (
    request.standingDecision === "approved" ||
    request.standingDecision === "denied"
  ) {
    return request.standingDecision;
  }
  if (request.status === "approved" || request.status === "denied") {
    return request.status;
  }
  return undefined;
}

export function shadowMCPSelectionBaselineForUpdate(body: {
  shadowMcpAllowedUrls?: readonly string[];
  shadowMcpBlockedUrls?: readonly string[];
}): Set<string> | undefined {
  // The two lists are disposition-exclusive, so at most one key is present.
  if (Object.prototype.hasOwnProperty.call(body, "shadowMcpBlockedUrls")) {
    return new Set(body.shadowMcpBlockedUrls ?? []);
  }
  if (!Object.prototype.hasOwnProperty.call(body, "shadowMcpAllowedUrls")) {
    return undefined;
  }
  return new Set(body.shadowMcpAllowedUrls ?? []);
}
