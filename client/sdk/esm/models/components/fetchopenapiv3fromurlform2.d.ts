import * as z from "zod/v4-mini";
export type FetchOpenAPIv3FromURLForm2 = {
  /**
   * The URL to fetch the OpenAPI document from
   */
  url: string;
};
/** @internal */
export type FetchOpenAPIv3FromURLForm2$Outbound = {
  url: string;
};
/** @internal */
export declare const FetchOpenAPIv3FromURLForm2$outboundSchema: z.ZodMiniType<
  FetchOpenAPIv3FromURLForm2$Outbound,
  FetchOpenAPIv3FromURLForm2
>;
export declare function fetchOpenAPIv3FromURLForm2ToJSON(
  fetchOpenAPIv3FromURLForm2: FetchOpenAPIv3FromURLForm2,
): string;
//# sourceMappingURL=fetchopenapiv3fromurlform2.d.ts.map
