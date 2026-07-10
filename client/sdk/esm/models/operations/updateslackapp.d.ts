import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type UpdateSlackAppSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateSlackAppRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updateSlackAppRequestBody: components.UpdateSlackAppRequestBody;
};
/** @internal */
export type UpdateSlackAppSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateSlackAppSecurity$outboundSchema: z.ZodMiniType<
  UpdateSlackAppSecurity$Outbound,
  UpdateSlackAppSecurity
>;
export declare function updateSlackAppSecurityToJSON(
  updateSlackAppSecurity: UpdateSlackAppSecurity,
): string;
/** @internal */
export type UpdateSlackAppRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateSlackAppRequestBody: components.UpdateSlackAppRequestBody$Outbound;
};
/** @internal */
export declare const UpdateSlackAppRequest$outboundSchema: z.ZodMiniType<
  UpdateSlackAppRequest$Outbound,
  UpdateSlackAppRequest
>;
export declare function updateSlackAppRequestToJSON(
  updateSlackAppRequest: UpdateSlackAppRequest,
): string;
//# sourceMappingURL=updateslackapp.d.ts.map
