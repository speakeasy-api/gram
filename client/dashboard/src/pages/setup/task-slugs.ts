// URL slugs for each setup task's own page, /{org}/setup/{slug}. Kept apart
// from the task keys the server uses so a key can change without moving a
// page, and so the URLs read as destinations rather than identifiers.
export const SETUP_TASK_SLUGS: Record<string, string> = {
  "identity-provider": "idp",
  "anthropic-observability": "anthropic-observability",
  "instrument-agents": "other-platforms",
  "additional-agent-config": "integrations",
  "distribute-servers": "distribute-servers",
  "configure-policies": "policies",
  "platform-mcp": "platform-mcp",
};

export function setupTaskSlug(taskKey: string): string {
  return SETUP_TASK_SLUGS[taskKey] ?? taskKey;
}

export function setupTaskKeyForSlug(slug: string): string | undefined {
  const match = Object.entries(SETUP_TASK_SLUGS).find(
    ([, candidate]) => candidate === slug,
  );
  if (match) return match[0];
  // Task keys still work as slugs, so links minted with a key keep resolving.
  return slug in SETUP_TASK_SLUGS ? slug : undefined;
}
