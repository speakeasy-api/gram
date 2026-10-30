export type AuthType = "none" | "apikey" | "oauth" | "other" | "unknown";
export type UpdatedRange = "any" | "week" | "month" | "year";
/** Automatic includes no-auth servers and supported OAuth client setup. */
export type SetupType = "auto" | "manual" | "unknown";

export interface FilterValues {
  authTypes: AuthType[];
  updatedRange: UpdatedRange;
  setupTypes: SetupType[];
}
