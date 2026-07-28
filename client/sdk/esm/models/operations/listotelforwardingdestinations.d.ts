import * as z from "zod/v4-mini";
export type ListOtelForwardingDestinationsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListOtelForwardingDestinationsRequest = {
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
export type ListOtelForwardingDestinationsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListOtelForwardingDestinationsSecurity$outboundSchema: z.ZodMiniType<
  ListOtelForwardingDestinationsSecurity$Outbound,
  ListOtelForwardingDestinationsSecurity
>;
export declare function listOtelForwardingDestinationsSecurityToJSON(
  listOtelForwardingDestinationsSecurity: ListOtelForwardingDestinationsSecurity,
): string;
/** @internal */
export type ListOtelForwardingDestinationsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListOtelForwardingDestinationsRequest$outboundSchema: z.ZodMiniType<
  ListOtelForwardingDestinationsRequest$Outbound,
  ListOtelForwardingDestinationsRequest
>;
export declare function listOtelForwardingDestinationsRequestToJSON(
  listOtelForwardingDestinationsRequest: ListOtelForwardingDestinationsRequest,
): string;
//# sourceMappingURL=listotelforwardingdestinations.d.ts.map
