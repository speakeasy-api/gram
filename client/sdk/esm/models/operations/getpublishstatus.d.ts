import * as z from "zod/v4-mini";
export type GetPublishStatusSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetPublishStatusRequest = {
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
export type GetPublishStatusSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetPublishStatusSecurity$outboundSchema: z.ZodMiniType<
  GetPublishStatusSecurity$Outbound,
  GetPublishStatusSecurity
>;
export declare function getPublishStatusSecurityToJSON(
  getPublishStatusSecurity: GetPublishStatusSecurity,
): string;
/** @internal */
export type GetPublishStatusRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetPublishStatusRequest$outboundSchema: z.ZodMiniType<
  GetPublishStatusRequest$Outbound,
  GetPublishStatusRequest
>;
export declare function getPublishStatusRequestToJSON(
  getPublishStatusRequest: GetPublishStatusRequest,
): string;
//# sourceMappingURL=getpublishstatus.d.ts.map
