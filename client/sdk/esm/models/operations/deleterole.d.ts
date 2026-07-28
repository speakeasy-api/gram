import * as z from "zod/v4-mini";
export type DeleteRoleSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteRoleRequest = {
  /**
   * The ID of the role to delete.
   */
  id: string;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type DeleteRoleSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteRoleSecurity$outboundSchema: z.ZodMiniType<
  DeleteRoleSecurity$Outbound,
  DeleteRoleSecurity
>;
export declare function deleteRoleSecurityToJSON(
  deleteRoleSecurity: DeleteRoleSecurity,
): string;
/** @internal */
export type DeleteRoleRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteRoleRequest$outboundSchema: z.ZodMiniType<
  DeleteRoleRequest$Outbound,
  DeleteRoleRequest
>;
export declare function deleteRoleRequestToJSON(
  deleteRoleRequest: DeleteRoleRequest,
): string;
//# sourceMappingURL=deleterole.d.ts.map
