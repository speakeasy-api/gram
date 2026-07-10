import * as z from "zod/v4-mini";
import {
  CreateOrganizationRemoteSessionClientForm,
  CreateOrganizationRemoteSessionClientForm$Outbound,
} from "../components/createorganizationremotesessionclientform.js";
export type CreateOrganizationRemoteSessionClientSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type CreateOrganizationRemoteSessionClientRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  createOrganizationRemoteSessionClientForm: CreateOrganizationRemoteSessionClientForm;
};
/** @internal */
export type CreateOrganizationRemoteSessionClientSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const CreateOrganizationRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<
  CreateOrganizationRemoteSessionClientSecurity$Outbound,
  CreateOrganizationRemoteSessionClientSecurity
>;
export declare function createOrganizationRemoteSessionClientSecurityToJSON(
  createOrganizationRemoteSessionClientSecurity: CreateOrganizationRemoteSessionClientSecurity,
): string;
/** @internal */
export type CreateOrganizationRemoteSessionClientRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  CreateOrganizationRemoteSessionClientForm: CreateOrganizationRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const CreateOrganizationRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<
  CreateOrganizationRemoteSessionClientRequest$Outbound,
  CreateOrganizationRemoteSessionClientRequest
>;
export declare function createOrganizationRemoteSessionClientRequestToJSON(
  createOrganizationRemoteSessionClientRequest: CreateOrganizationRemoteSessionClientRequest,
): string;
//# sourceMappingURL=createorganizationremotesessionclient.d.ts.map
