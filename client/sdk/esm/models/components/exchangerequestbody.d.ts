import * as z from "zod/v4-mini";
export type ExchangeRequestBody = {
  /**
   * Email address of the enrolled user to mint a per-user key for. Resolved to a user within the authenticated org.
   */
  email: string;
};
/** @internal */
export type ExchangeRequestBody$Outbound = {
  email: string;
};
/** @internal */
export declare const ExchangeRequestBody$outboundSchema: z.ZodMiniType<
  ExchangeRequestBody$Outbound,
  ExchangeRequestBody
>;
export declare function exchangeRequestBodyToJSON(
  exchangeRequestBody: ExchangeRequestBody,
): string;
//# sourceMappingURL=exchangerequestbody.d.ts.map
