import * as z from "zod/v4-mini";
export type GetRoleSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetRoleRequest = {
  /**
   * The ID of the role.
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
export type GetRoleSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRoleSecurity$outboundSchema: z.ZodMiniType<
  GetRoleSecurity$Outbound,
  GetRoleSecurity
>;
export declare function getRoleSecurityToJSON(
  getRoleSecurity: GetRoleSecurity,
): string;
/** @internal */
export type GetRoleRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRoleRequest$outboundSchema: z.ZodMiniType<
  GetRoleRequest$Outbound,
  GetRoleRequest
>;
export declare function getRoleRequestToJSON(
  getRoleRequest: GetRoleRequest,
): string;
//# sourceMappingURL=getrole.d.ts.map
