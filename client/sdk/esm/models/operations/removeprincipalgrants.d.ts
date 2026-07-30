import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type RemovePrincipalGrantsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type RemovePrincipalGrantsRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  removePrincipalGrantsRequestBody: components.RemovePrincipalGrantsRequestBody;
};
/** @internal */
export type RemovePrincipalGrantsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemovePrincipalGrantsSecurity$outboundSchema: z.ZodMiniType<
  RemovePrincipalGrantsSecurity$Outbound,
  RemovePrincipalGrantsSecurity
>;
export declare function removePrincipalGrantsSecurityToJSON(
  removePrincipalGrantsSecurity: RemovePrincipalGrantsSecurity,
): string;
/** @internal */
export type RemovePrincipalGrantsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  RemovePrincipalGrantsRequestBody: components.RemovePrincipalGrantsRequestBody$Outbound;
};
/** @internal */
export declare const RemovePrincipalGrantsRequest$outboundSchema: z.ZodMiniType<
  RemovePrincipalGrantsRequest$Outbound,
  RemovePrincipalGrantsRequest
>;
export declare function removePrincipalGrantsRequestToJSON(
  removePrincipalGrantsRequest: RemovePrincipalGrantsRequest,
): string;
//# sourceMappingURL=removeprincipalgrants.d.ts.map
