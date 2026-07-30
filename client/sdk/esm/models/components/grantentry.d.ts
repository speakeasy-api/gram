import * as z from "zod/v4-mini";
/**
 * A permission entry identifying who it applies to, what action it covers, and which resource it targets.
 */
export type GrantEntry = {
  /**
   * The user or role this permission entry applies to (e.g. "user:user_abc", "role:admin").
   */
  principalUrn: string;
  /**
   * The resource this permission applies to. Use "*" for unrestricted access.
   */
  resource: string;
  /**
   * The action being permitted (e.g. "build:read", "mcp:connect").
   */
  scope: string;
};
/** @internal */
export type GrantEntry$Outbound = {
  principal_urn: string;
  resource: string;
  scope: string;
};
/** @internal */
export declare const GrantEntry$outboundSchema: z.ZodMiniType<
  GrantEntry$Outbound,
  GrantEntry
>;
export declare function grantEntryToJSON(grantEntry: GrantEntry): string;
//# sourceMappingURL=grantentry.d.ts.map
