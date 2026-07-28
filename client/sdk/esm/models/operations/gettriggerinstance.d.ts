import * as z from "zod/v4-mini";
export type GetTriggerInstanceSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetTriggerInstanceRequest = {
  /**
   * The trigger instance ID.
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
export type GetTriggerInstanceSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetTriggerInstanceSecurity$outboundSchema: z.ZodMiniType<
  GetTriggerInstanceSecurity$Outbound,
  GetTriggerInstanceSecurity
>;
export declare function getTriggerInstanceSecurityToJSON(
  getTriggerInstanceSecurity: GetTriggerInstanceSecurity,
): string;
/** @internal */
export type GetTriggerInstanceRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetTriggerInstanceRequest$outboundSchema: z.ZodMiniType<
  GetTriggerInstanceRequest$Outbound,
  GetTriggerInstanceRequest
>;
export declare function getTriggerInstanceRequestToJSON(
  getTriggerInstanceRequest: GetTriggerInstanceRequest,
): string;
//# sourceMappingURL=gettriggerinstance.d.ts.map
