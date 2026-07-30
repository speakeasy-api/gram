import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type RemoveGrantsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type RemoveGrantsRequest = {
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
export type RemoveGrantsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemoveGrantsSecurity$outboundSchema: z.ZodMiniType<
  RemoveGrantsSecurity$Outbound,
  RemoveGrantsSecurity
>;
export declare function removeGrantsSecurityToJSON(
  removeGrantsSecurity: RemoveGrantsSecurity,
): string;
/** @internal */
export type RemoveGrantsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  GrantsForm: components.GrantsForm$Outbound;
};
/** @internal */
export declare const RemoveGrantsRequest$outboundSchema: z.ZodMiniType<
  RemoveGrantsRequest$Outbound,
  RemoveGrantsRequest
>;
export declare function removeGrantsRequestToJSON(
  removeGrantsRequest: RemoveGrantsRequest,
): string;
//# sourceMappingURL=removegrants.d.ts.map
