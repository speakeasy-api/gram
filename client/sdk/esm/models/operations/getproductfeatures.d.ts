import * as z from "zod/v4-mini";
export type GetProductFeaturesSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetProductFeaturesRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetProductFeaturesSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetProductFeaturesSecurity$outboundSchema: z.ZodMiniType<
  GetProductFeaturesSecurity$Outbound,
  GetProductFeaturesSecurity
>;
export declare function getProductFeaturesSecurityToJSON(
  getProductFeaturesSecurity: GetProductFeaturesSecurity,
): string;
/** @internal */
export type GetProductFeaturesRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetProductFeaturesRequest$outboundSchema: z.ZodMiniType<
  GetProductFeaturesRequest$Outbound,
  GetProductFeaturesRequest
>;
export declare function getProductFeaturesRequestToJSON(
  getProductFeaturesRequest: GetProductFeaturesRequest,
): string;
//# sourceMappingURL=getproductfeatures.d.ts.map
