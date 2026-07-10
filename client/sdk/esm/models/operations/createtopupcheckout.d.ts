import * as z from "zod/v4-mini";
export type CreateTopUpCheckoutSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateTopUpCheckoutRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type CreateTopUpCheckoutSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateTopUpCheckoutSecurity$outboundSchema: z.ZodMiniType<
  CreateTopUpCheckoutSecurity$Outbound,
  CreateTopUpCheckoutSecurity
>;
export declare function createTopUpCheckoutSecurityToJSON(
  createTopUpCheckoutSecurity: CreateTopUpCheckoutSecurity,
): string;
/** @internal */
export type CreateTopUpCheckoutRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateTopUpCheckoutRequest$outboundSchema: z.ZodMiniType<
  CreateTopUpCheckoutRequest$Outbound,
  CreateTopUpCheckoutRequest
>;
export declare function createTopUpCheckoutRequestToJSON(
  createTopUpCheckoutRequest: CreateTopUpCheckoutRequest,
): string;
//# sourceMappingURL=createtopupcheckout.d.ts.map
