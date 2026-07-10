import * as z from "zod/v4-mini";
export type ValidateAPIKeySecurity = {
  apikeyHeaderGramKey?: string | undefined;
};
export type ValidateAPIKeyRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
};
/** @internal */
export type ValidateAPIKeySecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ValidateAPIKeySecurity$outboundSchema: z.ZodMiniType<
  ValidateAPIKeySecurity$Outbound,
  ValidateAPIKeySecurity
>;
export declare function validateAPIKeySecurityToJSON(
  validateAPIKeySecurity: ValidateAPIKeySecurity,
): string;
/** @internal */
export type ValidateAPIKeyRequest$Outbound = {
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ValidateAPIKeyRequest$outboundSchema: z.ZodMiniType<
  ValidateAPIKeyRequest$Outbound,
  ValidateAPIKeyRequest
>;
export declare function validateAPIKeyRequestToJSON(
  validateAPIKeyRequest: ValidateAPIKeyRequest,
): string;
//# sourceMappingURL=validateapikey.d.ts.map
