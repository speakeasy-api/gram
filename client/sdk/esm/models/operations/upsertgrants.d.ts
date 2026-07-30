import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type UpsertGrantsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpsertGrantsRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  grantsForm: components.GrantsForm;
};
/** @internal */
export type UpsertGrantsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpsertGrantsSecurity$outboundSchema: z.ZodMiniType<
  UpsertGrantsSecurity$Outbound,
  UpsertGrantsSecurity
>;
export declare function upsertGrantsSecurityToJSON(
  upsertGrantsSecurity: UpsertGrantsSecurity,
): string;
/** @internal */
export type UpsertGrantsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  GrantsForm: components.GrantsForm$Outbound;
};
/** @internal */
export declare const UpsertGrantsRequest$outboundSchema: z.ZodMiniType<
  UpsertGrantsRequest$Outbound,
  UpsertGrantsRequest
>;
export declare function upsertGrantsRequestToJSON(
  upsertGrantsRequest: UpsertGrantsRequest,
): string;
//# sourceMappingURL=upsertgrants.d.ts.map
