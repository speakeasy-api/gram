import * as z from "zod/v4-mini";
export type DiscoverRemoteSessionIssuerRequestBody = {
  /**
   * Issuer URL to discover (e.g. https://login.linear.com).
   */
  issuer: string;
};
/** @internal */
export type DiscoverRemoteSessionIssuerRequestBody$Outbound = {
  issuer: string;
};
/** @internal */
export declare const DiscoverRemoteSessionIssuerRequestBody$outboundSchema: z.ZodMiniType<
  DiscoverRemoteSessionIssuerRequestBody$Outbound,
  DiscoverRemoteSessionIssuerRequestBody
>;
export declare function discoverRemoteSessionIssuerRequestBodyToJSON(
  discoverRemoteSessionIssuerRequestBody: DiscoverRemoteSessionIssuerRequestBody,
): string;
//# sourceMappingURL=discoverremotesessionissuerrequestbody.d.ts.map
