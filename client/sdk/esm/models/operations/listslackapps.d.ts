import * as z from "zod/v4-mini";
export type ListSlackAppsSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListSlackAppsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type ListSlackAppsSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListSlackAppsSecurity$outboundSchema: z.ZodMiniType<
  ListSlackAppsSecurity$Outbound,
  ListSlackAppsSecurity
>;
export declare function listSlackAppsSecurityToJSON(
  listSlackAppsSecurity: ListSlackAppsSecurity,
): string;
/** @internal */
export type ListSlackAppsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListSlackAppsRequest$outboundSchema: z.ZodMiniType<
  ListSlackAppsRequest$Outbound,
  ListSlackAppsRequest
>;
export declare function listSlackAppsRequestToJSON(
  listSlackAppsRequest: ListSlackAppsRequest,
): string;
//# sourceMappingURL=listslackapps.d.ts.map
