import * as z from "zod/v3";
import * as components from "../components/index.js";
export type UpdateSlackConnectionSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateSlackConnectionRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updateSlackConnectionRequestBody: components.UpdateSlackConnectionRequestBody;
};
/** @internal */
export type UpdateSlackConnectionSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateSlackConnectionSecurity$outboundSchema: z.ZodType<
  UpdateSlackConnectionSecurity$Outbound,
  z.ZodTypeDef,
  UpdateSlackConnectionSecurity
>;
export declare function updateSlackConnectionSecurityToJSON(
  updateSlackConnectionSecurity: UpdateSlackConnectionSecurity,
): string;
/** @internal */
export type UpdateSlackConnectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateSlackConnectionRequestBody: components.UpdateSlackConnectionRequestBody$Outbound;
};
/** @internal */
export declare const UpdateSlackConnectionRequest$outboundSchema: z.ZodType<
  UpdateSlackConnectionRequest$Outbound,
  z.ZodTypeDef,
  UpdateSlackConnectionRequest
>;
export declare function updateSlackConnectionRequestToJSON(
  updateSlackConnectionRequest: UpdateSlackConnectionRequest,
): string;
//# sourceMappingURL=updateslackconnection.d.ts.map
