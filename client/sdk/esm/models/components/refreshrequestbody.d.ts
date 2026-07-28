import * as z from "zod/v4-mini";
export type RefreshRequestBody = {
  /**
   * The opaque refresh token issued by a prior exchange or refresh call.
   */
  refreshToken: string;
};
/** @internal */
export type RefreshRequestBody$Outbound = {
  refresh_token: string;
};
/** @internal */
export declare const RefreshRequestBody$outboundSchema: z.ZodMiniType<
  RefreshRequestBody$Outbound,
  RefreshRequestBody
>;
export declare function refreshRequestBodyToJSON(
  refreshRequestBody: RefreshRequestBody,
): string;
//# sourceMappingURL=refreshrequestbody.d.ts.map
