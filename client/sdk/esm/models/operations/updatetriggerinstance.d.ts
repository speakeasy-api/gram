import * as z from "zod/v4-mini";
import {
  UpdateTriggerInstanceForm,
  UpdateTriggerInstanceForm$Outbound,
} from "../components/updatetriggerinstanceform.js";
export type UpdateTriggerInstanceSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateTriggerInstanceRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updateTriggerInstanceForm: UpdateTriggerInstanceForm;
};
/** @internal */
export type UpdateTriggerInstanceSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateTriggerInstanceSecurity$outboundSchema: z.ZodMiniType<
  UpdateTriggerInstanceSecurity$Outbound,
  UpdateTriggerInstanceSecurity
>;
export declare function updateTriggerInstanceSecurityToJSON(
  updateTriggerInstanceSecurity: UpdateTriggerInstanceSecurity,
): string;
/** @internal */
export type UpdateTriggerInstanceRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateTriggerInstanceForm: UpdateTriggerInstanceForm$Outbound;
};
/** @internal */
export declare const UpdateTriggerInstanceRequest$outboundSchema: z.ZodMiniType<
  UpdateTriggerInstanceRequest$Outbound,
  UpdateTriggerInstanceRequest
>;
export declare function updateTriggerInstanceRequestToJSON(
  updateTriggerInstanceRequest: UpdateTriggerInstanceRequest,
): string;
//# sourceMappingURL=updatetriggerinstance.d.ts.map
