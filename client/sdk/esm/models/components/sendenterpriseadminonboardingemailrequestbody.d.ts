import * as z from "zod/v4-mini";
export type SendEnterpriseAdminOnboardingEmailRequestBody = {
  /**
   * Recipient email addresses.
   */
  recipients: Array<string>;
};
/** @internal */
export type SendEnterpriseAdminOnboardingEmailRequestBody$Outbound = {
  recipients: Array<string>;
};
/** @internal */
export declare const SendEnterpriseAdminOnboardingEmailRequestBody$outboundSchema: z.ZodMiniType<
  SendEnterpriseAdminOnboardingEmailRequestBody$Outbound,
  SendEnterpriseAdminOnboardingEmailRequestBody
>;
export declare function sendEnterpriseAdminOnboardingEmailRequestBodyToJSON(
  sendEnterpriseAdminOnboardingEmailRequestBody: SendEnterpriseAdminOnboardingEmailRequestBody,
): string;
//# sourceMappingURL=sendenterpriseadminonboardingemailrequestbody.d.ts.map
