import * as z from "zod/v4-mini";
import {
  SendEnterpriseAdminOnboardingEmailRequestBody,
  SendEnterpriseAdminOnboardingEmailRequestBody$Outbound,
} from "../components/sendenterpriseadminonboardingemailrequestbody.js";
export type SendEnterpriseAdminOnboardingEmailSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type SendEnterpriseAdminOnboardingEmailRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  sendEnterpriseAdminOnboardingEmailRequestBody: SendEnterpriseAdminOnboardingEmailRequestBody;
};
/** @internal */
export type SendEnterpriseAdminOnboardingEmailSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SendEnterpriseAdminOnboardingEmailSecurity$outboundSchema: z.ZodMiniType<
  SendEnterpriseAdminOnboardingEmailSecurity$Outbound,
  SendEnterpriseAdminOnboardingEmailSecurity
>;
export declare function sendEnterpriseAdminOnboardingEmailSecurityToJSON(
  sendEnterpriseAdminOnboardingEmailSecurity: SendEnterpriseAdminOnboardingEmailSecurity,
): string;
/** @internal */
export type SendEnterpriseAdminOnboardingEmailRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  SendEnterpriseAdminOnboardingEmailRequestBody: SendEnterpriseAdminOnboardingEmailRequestBody$Outbound;
};
/** @internal */
export declare const SendEnterpriseAdminOnboardingEmailRequest$outboundSchema: z.ZodMiniType<
  SendEnterpriseAdminOnboardingEmailRequest$Outbound,
  SendEnterpriseAdminOnboardingEmailRequest
>;
export declare function sendEnterpriseAdminOnboardingEmailRequestToJSON(
  sendEnterpriseAdminOnboardingEmailRequest: SendEnterpriseAdminOnboardingEmailRequest,
): string;
//# sourceMappingURL=sendenterpriseadminonboardingemail.d.ts.map
