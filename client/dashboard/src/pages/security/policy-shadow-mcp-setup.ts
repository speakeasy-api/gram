import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";

export type SubmissionKeyCache = {
  current: { fingerprint: string; key: string } | null;
};

type OriginalPolicy = Pick<RiskPolicy, "action" | "enabled" | "sources">;

export function isBlockingShadowMCPPolicy(
  enabled: boolean,
  sources: readonly string[],
  action: string,
): boolean {
  return enabled && action === "block" && sources.includes("shadow_mcp");
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
    isBlockingShadowMCPPolicy(
      originalPolicy.enabled,
      originalPolicy.sources,
      originalPolicy.action,
    )
  ) {
    return [];
  }

  return undefined;
}

export function idempotencyKeyForFingerprint(
  cache: SubmissionKeyCache,
  fingerprint: string,
  createKey: () => string = () => crypto.randomUUID(),
): string {
  if (cache.current?.fingerprint === fingerprint) return cache.current.key;

  const key = createKey();
  cache.current = { fingerprint, key };
  return key;
}
