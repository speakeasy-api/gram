// Legacy /{org}/setup/{slug} aliases normalize to the shared ?task=<key> entry.
export const SETUP_TASK_SLUGS: Record<string, string> = {
  "enable-logging": "enable-logging",
  "connect-idp": "connect-idp",
  "directory-sync": "directory-sync",
  "create-marketplace": "create-marketplace",
  "confirm-traffic": "confirm-traffic",
  "identity-provider": "idp",
  "anthropic-observability": "anthropic-observability",
  "anthropic-admin-controls": "anthropic-admin-controls",
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
  return Object.hasOwn(SETUP_TASK_SLUGS, slug) ? slug : undefined;
}

export function canonicalSetupSearch(
  search: URLSearchParams,
  taskSlug?: string,
): URLSearchParams {
  const next = new URLSearchParams(search);
  const step = next.get("step");
  const legacyTask =
    step && Object.hasOwn(SETUP_TASK_SLUGS, step) ? step : undefined;
  if (!next.has("task")) {
    const task = taskSlug
      ? (setupTaskKeyForSlug(taskSlug) ?? taskSlug)
      : legacyTask;
    if (task) next.set("task", task);
    if (legacyTask && !taskSlug) next.delete("step");
  }
  return next;
}
