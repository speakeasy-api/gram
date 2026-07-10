import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type CreateOtelForwardingDestinationSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type CreateOtelForwardingDestinationRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createDestinationRequestBody: components.CreateDestinationRequestBody;
};
/** @internal */
export type CreateOtelForwardingDestinationSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateOtelForwardingDestinationSecurity$outboundSchema: z.ZodMiniType<
  CreateOtelForwardingDestinationSecurity$Outbound,
  CreateOtelForwardingDestinationSecurity
>;
export declare function createOtelForwardingDestinationSecurityToJSON(
  createOtelForwardingDestinationSecurity: CreateOtelForwardingDestinationSecurity,
): string;
/** @internal */
export type CreateOtelForwardingDestinationRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  CreateDestinationRequestBody: components.CreateDestinationRequestBody$Outbound;
};
/** @internal */
export declare const CreateOtelForwardingDestinationRequest$outboundSchema: z.ZodMiniType<
  CreateOtelForwardingDestinationRequest$Outbound,
  CreateOtelForwardingDestinationRequest
>;
export declare function createOtelForwardingDestinationRequestToJSON(
  createOtelForwardingDestinationRequest: CreateOtelForwardingDestinationRequest,
): string;
//# sourceMappingURL=createotelforwardingdestination.d.ts.map
