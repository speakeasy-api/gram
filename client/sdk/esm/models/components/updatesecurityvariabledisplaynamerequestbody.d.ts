import * as z from "zod/v3";
export type UpdateSecurityVariableDisplayNameRequestBody = {
  /**
   * The user-friendly display name. Set to empty string to clear and use the original name.
   */
  displayName: string;
  /**
   * The security scheme key (e.g., 'BearerAuth', 'ApiKeyAuth') from the OpenAPI spec
   */
  securityKey: string;
  /**
   * The slug of the toolset containing the security variable
   */
  toolsetSlug: string;
};
/** @internal */
export type UpdateSecurityVariableDisplayNameRequestBody$Outbound = {
  display_name: string;
  security_key: string;
  toolset_slug: string;
};
/** @internal */
export declare const UpdateSecurityVariableDisplayNameRequestBody$outboundSchema: z.ZodType<
  UpdateSecurityVariableDisplayNameRequestBody$Outbound,
  z.ZodTypeDef,
  UpdateSecurityVariableDisplayNameRequestBody
>;
export declare function updateSecurityVariableDisplayNameRequestBodyToJSON(
  updateSecurityVariableDisplayNameRequestBody: UpdateSecurityVariableDisplayNameRequestBody,
): string;
//# sourceMappingURL=updatesecurityvariabledisplaynamerequestbody.d.ts.map
