import * as z from "zod/v4-mini";
export type ListGrantsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListGrantsRequest = {
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
export type ListGrantsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGrantsSecurity$outboundSchema: z.ZodMiniType<
  ListGrantsSecurity$Outbound,
  ListGrantsSecurity
>;
export declare function listGrantsSecurityToJSON(
  listGrantsSecurity: ListGrantsSecurity,
): string;
/** @internal */
export type ListGrantsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGrantsRequest$outboundSchema: z.ZodMiniType<
  ListGrantsRequest$Outbound,
  ListGrantsRequest
>;
export declare function listGrantsRequestToJSON(
  listGrantsRequest: ListGrantsRequest,
): string;
//# sourceMappingURL=listgrants.d.ts.map
