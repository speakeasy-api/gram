import * as z from "zod/v4-mini";
export type ListCollectionsSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type ListCollectionsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
};
/** @internal */
export type ListCollectionsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListCollectionsSecurity$outboundSchema: z.ZodMiniType<
  ListCollectionsSecurity$Outbound,
  ListCollectionsSecurity
>;
export declare function listCollectionsSecurityToJSON(
  listCollectionsSecurity: ListCollectionsSecurity,
): string;
/** @internal */
export type ListCollectionsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListCollectionsRequest$outboundSchema: z.ZodMiniType<
  ListCollectionsRequest$Outbound,
  ListCollectionsRequest
>;
export declare function listCollectionsRequestToJSON(
  listCollectionsRequest: ListCollectionsRequest,
): string;
//# sourceMappingURL=listcollections.d.ts.map
