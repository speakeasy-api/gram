import * as z from "zod/v4-mini";
import {
  RemoveClientFromMcpServerRequestBody,
  RemoveClientFromMcpServerRequestBody$Outbound,
} from "../components/removeclientfrommcpserverrequestbody.js";
export type RemoveOrganizationRemoteSessionClientFromMcpServerSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type RemoveOrganizationRemoteSessionClientFromMcpServerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  removeClientFromMcpServerRequestBody: RemoveClientFromMcpServerRequestBody;
};
/** @internal */
export type RemoveOrganizationRemoteSessionClientFromMcpServerSecurity$Outbound =
  {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
  };
/** @internal */
export declare const RemoveOrganizationRemoteSessionClientFromMcpServerSecurity$outboundSchema: z.ZodMiniType<
  RemoveOrganizationRemoteSessionClientFromMcpServerSecurity$Outbound,
  RemoveOrganizationRemoteSessionClientFromMcpServerSecurity
>;
export declare function removeOrganizationRemoteSessionClientFromMcpServerSecurityToJSON(
  removeOrganizationRemoteSessionClientFromMcpServerSecurity: RemoveOrganizationRemoteSessionClientFromMcpServerSecurity,
): string;
/** @internal */
export type RemoveOrganizationRemoteSessionClientFromMcpServerRequest$Outbound =
  {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    RemoveClientFromMcpServerRequestBody: RemoveClientFromMcpServerRequestBody$Outbound;
  };
/** @internal */
export declare const RemoveOrganizationRemoteSessionClientFromMcpServerRequest$outboundSchema: z.ZodMiniType<
  RemoveOrganizationRemoteSessionClientFromMcpServerRequest$Outbound,
  RemoveOrganizationRemoteSessionClientFromMcpServerRequest
>;
export declare function removeOrganizationRemoteSessionClientFromMcpServerRequestToJSON(
  removeOrganizationRemoteSessionClientFromMcpServerRequest: RemoveOrganizationRemoteSessionClientFromMcpServerRequest,
): string;
//# sourceMappingURL=removeorganizationremotesessionclientfrommcpserver.d.ts.map
