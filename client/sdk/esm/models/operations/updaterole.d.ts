import * as z from "zod/v4-mini";
import {
  UpdateRoleForm,
  UpdateRoleForm$Outbound,
} from "../components/updateroleform.js";
export type UpdateRoleSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateRoleRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  updateRoleForm: UpdateRoleForm;
};
/** @internal */
export type UpdateRoleSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateRoleSecurity$outboundSchema: z.ZodMiniType<
  UpdateRoleSecurity$Outbound,
  UpdateRoleSecurity
>;
export declare function updateRoleSecurityToJSON(
  updateRoleSecurity: UpdateRoleSecurity,
): string;
/** @internal */
export type UpdateRoleRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  UpdateRoleForm: UpdateRoleForm$Outbound;
};
/** @internal */
export declare const UpdateRoleRequest$outboundSchema: z.ZodMiniType<
  UpdateRoleRequest$Outbound,
  UpdateRoleRequest
>;
export declare function updateRoleRequestToJSON(
  updateRoleRequest: UpdateRoleRequest,
): string;
//# sourceMappingURL=updaterole.d.ts.map
