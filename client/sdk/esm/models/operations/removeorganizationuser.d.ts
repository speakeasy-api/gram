import * as z from "zod/v4-mini";
export type RemoveOrganizationUserSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type RemoveOrganizationUserRequest = {
  /**
   * Gram user ID to remove.
   */
  userId: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type RemoveOrganizationUserSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemoveOrganizationUserSecurity$outboundSchema: z.ZodMiniType<
  RemoveOrganizationUserSecurity$Outbound,
  RemoveOrganizationUserSecurity
>;
export declare function removeOrganizationUserSecurityToJSON(
  removeOrganizationUserSecurity: RemoveOrganizationUserSecurity,
): string;
/** @internal */
export type RemoveOrganizationUserRequest$Outbound = {
  user_id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemoveOrganizationUserRequest$outboundSchema: z.ZodMiniType<
  RemoveOrganizationUserRequest$Outbound,
  RemoveOrganizationUserRequest
>;
export declare function removeOrganizationUserRequestToJSON(
  removeOrganizationUserRequest: RemoveOrganizationUserRequest,
): string;
//# sourceMappingURL=removeorganizationuser.d.ts.map
