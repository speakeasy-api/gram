import * as z from "zod/v4-mini";
import {
  UpdateRemoteSessionIssuerForm,
  UpdateRemoteSessionIssuerForm$Outbound,
} from "../components/updateremotesessionissuerform.js";
export type UpdateOrganizationRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type UpdateOrganizationRemoteSessionIssuerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  updateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm;
};
/** @internal */
export type UpdateOrganizationRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const UpdateOrganizationRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  UpdateOrganizationRemoteSessionIssuerSecurity$Outbound,
  UpdateOrganizationRemoteSessionIssuerSecurity
>;
export declare function updateOrganizationRemoteSessionIssuerSecurityToJSON(
  updateOrganizationRemoteSessionIssuerSecurity: UpdateOrganizationRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type UpdateOrganizationRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  UpdateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm$Outbound;
};
/** @internal */
export declare const UpdateOrganizationRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  UpdateOrganizationRemoteSessionIssuerRequest$Outbound,
  UpdateOrganizationRemoteSessionIssuerRequest
>;
export declare function updateOrganizationRemoteSessionIssuerRequestToJSON(
  updateOrganizationRemoteSessionIssuerRequest: UpdateOrganizationRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=updateorganizationremotesessionissuer.d.ts.map
