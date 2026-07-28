import * as z from "zod/v4-mini";
export type DeleteSlackAppSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteSlackAppRequest = {
  /**
   * The Slack app ID
   */
  id: string;
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
export type DeleteSlackAppSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteSlackAppSecurity$outboundSchema: z.ZodMiniType<
  DeleteSlackAppSecurity$Outbound,
  DeleteSlackAppSecurity
>;
export declare function deleteSlackAppSecurityToJSON(
  deleteSlackAppSecurity: DeleteSlackAppSecurity,
): string;
/** @internal */
export type DeleteSlackAppRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteSlackAppRequest$outboundSchema: z.ZodMiniType<
  DeleteSlackAppRequest$Outbound,
  DeleteSlackAppRequest
>;
export declare function deleteSlackAppRequestToJSON(
  deleteSlackAppRequest: DeleteSlackAppRequest,
): string;
//# sourceMappingURL=deleteslackapp.d.ts.map
