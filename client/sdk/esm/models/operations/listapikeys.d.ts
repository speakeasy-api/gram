import * as z from "zod/v4-mini";
export type ListAPIKeysSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListAPIKeysRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListAPIKeysSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAPIKeysSecurity$outboundSchema: z.ZodMiniType<
  ListAPIKeysSecurity$Outbound,
  ListAPIKeysSecurity
>;
export declare function listAPIKeysSecurityToJSON(
  listAPIKeysSecurity: ListAPIKeysSecurity,
): string;
/** @internal */
export type ListAPIKeysRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAPIKeysRequest$outboundSchema: z.ZodMiniType<
  ListAPIKeysRequest$Outbound,
  ListAPIKeysRequest
>;
export declare function listAPIKeysRequestToJSON(
  listAPIKeysRequest: ListAPIKeysRequest,
): string;
//# sourceMappingURL=listapikeys.d.ts.map
