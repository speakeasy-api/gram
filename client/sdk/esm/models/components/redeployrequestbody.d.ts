import * as z from "zod/v4-mini";
export type RedeployRequestBody = {
  /**
   * The ID of the deployment to redeploy.
   */
  deploymentId: string;
};
/** @internal */
export type RedeployRequestBody$Outbound = {
  deployment_id: string;
};
/** @internal */
export declare const RedeployRequestBody$outboundSchema: z.ZodMiniType<
  RedeployRequestBody$Outbound,
  RedeployRequestBody
>;
export declare function redeployRequestBodyToJSON(
  redeployRequestBody: RedeployRequestBody,
): string;
//# sourceMappingURL=redeployrequestbody.d.ts.map
