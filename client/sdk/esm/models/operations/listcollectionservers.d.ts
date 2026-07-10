import * as z from "zod/v4-mini";
export type ListCollectionServersSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type ListCollectionServersRequest = {
  /**
   * Slug of the collection to serve
   */
  collectionSlug: string;
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
export type ListCollectionServersSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListCollectionServersSecurity$outboundSchema: z.ZodMiniType<
  ListCollectionServersSecurity$Outbound,
  ListCollectionServersSecurity
>;
export declare function listCollectionServersSecurityToJSON(
  listCollectionServersSecurity: ListCollectionServersSecurity,
): string;
/** @internal */
export type ListCollectionServersRequest$Outbound = {
  collection_slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListCollectionServersRequest$outboundSchema: z.ZodMiniType<
  ListCollectionServersRequest$Outbound,
  ListCollectionServersRequest
>;
export declare function listCollectionServersRequestToJSON(
  listCollectionServersRequest: ListCollectionServersRequest,
): string;
//# sourceMappingURL=listcollectionservers.d.ts.map
