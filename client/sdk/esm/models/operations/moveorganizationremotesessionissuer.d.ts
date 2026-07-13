import * as z from "zod/v4-mini";
import {
  MoveIssuerRequestBody,
  MoveIssuerRequestBody$Outbound,
} from "../components/moveissuerrequestbody.js";
export type MoveOrganizationRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type MoveOrganizationRemoteSessionIssuerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  moveIssuerRequestBody: MoveIssuerRequestBody;
};
/** @internal */
export type MoveOrganizationRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const MoveOrganizationRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  MoveOrganizationRemoteSessionIssuerSecurity$Outbound,
  MoveOrganizationRemoteSessionIssuerSecurity
>;
export declare function moveOrganizationRemoteSessionIssuerSecurityToJSON(
  moveOrganizationRemoteSessionIssuerSecurity: MoveOrganizationRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type MoveOrganizationRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  MoveIssuerRequestBody: MoveIssuerRequestBody$Outbound;
};
/** @internal */
export declare const MoveOrganizationRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  MoveOrganizationRemoteSessionIssuerRequest$Outbound,
  MoveOrganizationRemoteSessionIssuerRequest
>;
export declare function moveOrganizationRemoteSessionIssuerRequestToJSON(
  moveOrganizationRemoteSessionIssuerRequest: MoveOrganizationRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=moveorganizationremotesessionissuer.d.ts.map
