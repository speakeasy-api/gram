import * as z from "zod/v4-mini";
export type GetOtelForwardingDestinationSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetOtelForwardingDestinationRequest = {
  /**
   * The destination ID.
   */
  id: string;
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
export type GetOtelForwardingDestinationSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOtelForwardingDestinationSecurity$outboundSchema: z.ZodMiniType<
  GetOtelForwardingDestinationSecurity$Outbound,
  GetOtelForwardingDestinationSecurity
>;
export declare function getOtelForwardingDestinationSecurityToJSON(
  getOtelForwardingDestinationSecurity: GetOtelForwardingDestinationSecurity,
): string;
/** @internal */
export type GetOtelForwardingDestinationRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOtelForwardingDestinationRequest$outboundSchema: z.ZodMiniType<
  GetOtelForwardingDestinationRequest$Outbound,
  GetOtelForwardingDestinationRequest
>;
export declare function getOtelForwardingDestinationRequestToJSON(
  getOtelForwardingDestinationRequest: GetOtelForwardingDestinationRequest,
): string;
//# sourceMappingURL=getotelforwardingdestination.d.ts.map
