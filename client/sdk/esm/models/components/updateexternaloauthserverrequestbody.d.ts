import * as z from "zod/v3";
export type UpdateExternalOAuthServerRequestBody = {
  /**
   * The updated metadata for the external OAuth server
   */
  metadata?: any | undefined;
};
/** @internal */
export type UpdateExternalOAuthServerRequestBody$Outbound = {
  metadata?: any | undefined;
};
/** @internal */
export declare const UpdateExternalOAuthServerRequestBody$outboundSchema: z.ZodType<
  UpdateExternalOAuthServerRequestBody$Outbound,
  z.ZodTypeDef,
  UpdateExternalOAuthServerRequestBody
>;
export declare function updateExternalOAuthServerRequestBodyToJSON(
  updateExternalOAuthServerRequestBody: UpdateExternalOAuthServerRequestBody,
): string;
//# sourceMappingURL=updateexternaloauthserverrequestbody.d.ts.map
