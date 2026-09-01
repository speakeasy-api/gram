export type WelcomeCardId =
  | "demo"
  | "guide"
  | "enterprise"
  | "platformMcp"
  | "defaultProject";

export type WelcomeBannerInputs = {
  isTrial: boolean;
  isAdmin: boolean;
  isZeroData: boolean;
  canSetUpOrg: boolean;
  platformMcpEnabled: boolean;
};

export type OverviewZeroDataSummary = {
  activeServersCount: number;
  totalToolCalls: number;
};

/**
 * Pick the primary path cards for the org-home welcome banner.
 * Announcements are appended by the banner, not here.
 */
export function selectWelcomeCardIds(
  input: WelcomeBannerInputs,
): WelcomeCardId[] {
  const enterprise: WelcomeCardId[] = input.canSetUpOrg ? ["enterprise"] : [];
  if (input.isTrial) return ["demo", "guide", ...enterprise];
  if (input.isZeroData) return ["guide", ...enterprise];
  if (input.isAdmin) {
    return [input.platformMcpEnabled ? "platformMcp" : "guide", ...enterprise];
  }
  return ["defaultProject"];
}

export function recommendedWelcomeCardId(
  ids: readonly WelcomeCardId[],
): WelcomeCardId | undefined {
  if (ids.includes("platformMcp")) return "platformMcp";
  if (ids.includes("defaultProject")) return "defaultProject";
  if (ids.includes("guide")) return "guide";
  return ids[0];
}

/** Display copy above the cards. One card is a prompt, not a choice. */
export function welcomeHeadline({
  columnCount,
  isTrial,
  isZeroData,
}: {
  columnCount: number;
  isTrial: boolean;
  isZeroData: boolean;
}): readonly string[] {
  if (columnCount === 1 && (isTrial || isZeroData))
    return ["Let’s get started"];
  if (isTrial || isZeroData) return ["Choose your", "first move"];
  return ["Pick up where", "you left off"];
}

/**
 * Onboarding-biased: missing overview, logs off, or a pending fetch all
 * read as zero data so Platform MCP never flashes in then swaps out.
 */
export function isOverviewZeroData(
  overview: { summary?: OverviewZeroDataSummary } | undefined,
): boolean {
  const summary = overview?.summary;
  if (summary === undefined) return true;
  return summary.activeServersCount === 0 && summary.totalToolCalls === 0;
}
