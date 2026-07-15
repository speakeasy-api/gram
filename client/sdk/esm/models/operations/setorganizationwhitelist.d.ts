import * as z from "zod/v4-mini";
import {
  SetOrganizationWhitelistRequestBody,
  SetOrganizationWhitelistRequestBody$Outbound,
} from "../components/setorganizationwhitelistrequestbody.js";
export type SetOrganizationWhitelistSecurity = {
  apikeyHeaderGramKey?: string | undefined;
};
export type SetOrganizationWhitelistRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  setOrganizationWhitelistRequestBody: SetOrganizationWhitelistRequestBody;
};
/** @internal */
export type SetOrganizationWhitelistSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const SetOrganizationWhitelistSecurity$outboundSchema: z.ZodMiniType<
  SetOrganizationWhitelistSecurity$Outbound,
  SetOrganizationWhitelistSecurity
>;
export declare function setOrganizationWhitelistSecurityToJSON(
  setOrganizationWhitelistSecurity: SetOrganizationWhitelistSecurity,
): string;
/** @internal */
export type SetOrganizationWhitelistRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  SetOrganizationWhitelistRequestBody: SetOrganizationWhitelistRequestBody$Outbound;
};
/** @internal */
export declare const SetOrganizationWhitelistRequest$outboundSchema: z.ZodMiniType<
  SetOrganizationWhitelistRequest$Outbound,
  SetOrganizationWhitelistRequest
>;
export declare function setOrganizationWhitelistRequestToJSON(
  setOrganizationWhitelistRequest: SetOrganizationWhitelistRequest,
): string;
//# sourceMappingURL=setorganizationwhitelist.d.ts.map
