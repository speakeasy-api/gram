import * as z from "zod/v4-mini";
export type RemovePrincipalGrantsRequestBody = {
  /**
   * The user or role to revoke all permissions from (e.g. "user:user_abc", "role:admin").
   */
  principalUrn: string;
};
/** @internal */
export type RemovePrincipalGrantsRequestBody$Outbound = {
  principal_urn: string;
};
/** @internal */
export declare const RemovePrincipalGrantsRequestBody$outboundSchema: z.ZodMiniType<
  RemovePrincipalGrantsRequestBody$Outbound,
  RemovePrincipalGrantsRequestBody
>;
export declare function removePrincipalGrantsRequestBodyToJSON(
  removePrincipalGrantsRequestBody: RemovePrincipalGrantsRequestBody,
): string;
//# sourceMappingURL=removeprincipalgrantsrequestbody.d.ts.map
