import * as z from "zod/v4-mini";
export type DeleteCollectionSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type DeleteCollectionRequest = {
  /**
   * ID of the collection to delete
   */
  collectionId: string;
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
export type DeleteCollectionSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DeleteCollectionSecurity$outboundSchema: z.ZodMiniType<
  DeleteCollectionSecurity$Outbound,
  DeleteCollectionSecurity
>;
export declare function deleteCollectionSecurityToJSON(
  deleteCollectionSecurity: DeleteCollectionSecurity,
): string;
/** @internal */
export type DeleteCollectionRequest$Outbound = {
  collection_id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DeleteCollectionRequest$outboundSchema: z.ZodMiniType<
  DeleteCollectionRequest$Outbound,
  DeleteCollectionRequest
>;
export declare function deleteCollectionRequestToJSON(
  deleteCollectionRequest: DeleteCollectionRequest,
): string;
//# sourceMappingURL=deletecollection.d.ts.map
