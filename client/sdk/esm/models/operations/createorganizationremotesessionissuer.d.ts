import * as z from "zod/v4-mini";
import {
  CreateIssuerRequestBody,
  CreateIssuerRequestBody$Outbound,
} from "../components/createissuerrequestbody.js";
export type CreateOrganizationRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type CreateOrganizationRemoteSessionIssuerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  createIssuerRequestBody: CreateIssuerRequestBody;
};
/** @internal */
export type CreateOrganizationRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const CreateOrganizationRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  CreateOrganizationRemoteSessionIssuerSecurity$Outbound,
  CreateOrganizationRemoteSessionIssuerSecurity
>;
export declare function createOrganizationRemoteSessionIssuerSecurityToJSON(
  createOrganizationRemoteSessionIssuerSecurity: CreateOrganizationRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type CreateOrganizationRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  CreateIssuerRequestBody: CreateIssuerRequestBody$Outbound;
};
/** @internal */
export declare const CreateOrganizationRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  CreateOrganizationRemoteSessionIssuerRequest$Outbound,
  CreateOrganizationRemoteSessionIssuerRequest
>;
export declare function createOrganizationRemoteSessionIssuerRequestToJSON(
  createOrganizationRemoteSessionIssuerRequest: CreateOrganizationRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=createorganizationremotesessionissuer.d.ts.map
