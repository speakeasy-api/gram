export type AuthType = "none" | "apikey" | "oauth" | "other";
export type ToolBehavior = "readonly" | "write";
export type PopularityThreshold = 0 | 100 | 1000 | 10000;
export type UpdatedRange = "any" | "week" | "month" | "year";
export type ToolCountThreshold = 0 | 5 | 10;
/** "auto" = supports DCR (one-click setup); "manual" = needs manual auth setup. */
export type SetupType = "auto" | "manual";

export interface FilterValues {
  authTypes: AuthType[];
  toolBehaviors: ToolBehavior[];
  minUsers: PopularityThreshold;
  updatedRange: UpdatedRange;
  minTools: ToolCountThreshold;
  setupTypes: SetupType[];
}
