import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";

type OriginalPolicy = Pick<RiskPolicy, "action" | "enabled" | "sources">;

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
}: {
  action: string;
  selectedCategories: ReadonlySet<string>;
  selectedURLs: ReadonlySet<string>;
  originalPolicy: OriginalPolicy | null;
}): string[] | undefined {
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

export function shadowMCPSelectionBaselineForUpdate(body: {
  shadowMcpAllowedUrls?: readonly string[];
}): Set<string> | undefined {
  if (!Object.prototype.hasOwnProperty.call(body, "shadowMcpAllowedUrls")) {
    return undefined;
  }
  return new Set(body.shadowMcpAllowedUrls ?? []);
}
