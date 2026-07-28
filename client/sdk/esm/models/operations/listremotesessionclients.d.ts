import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListRemoteSessionClientsResult } from "../components/listremotesessionclientsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListRemoteSessionClientsSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListRemoteSessionClientsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListRemoteSessionClientsSecurity = {
  option1?: ListRemoteSessionClientsSecurityOption1 | undefined;
  option2?: ListRemoteSessionClientsSecurityOption2 | undefined;
};
export type ListRemoteSessionClientsRequest = {
  /**
   * Filter to clients registered with this issuer.
   */
  remoteSessionIssuerId?: string | undefined;
  /**
   * Filter to clients paired with this user_session_issuer.
   */
  userSessionIssuerId?: string | undefined;
  /**
   * Pagination cursor.
   */
  cursor?: string | undefined;
  /**
   * Page size (default 50, max 100).
   */
  limit?: number | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
export type ListRemoteSessionClientsResponse = {
  result: ListRemoteSessionClientsResult;
};
/** @internal */
export type ListRemoteSessionClientsSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRemoteSessionClientsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListRemoteSessionClientsSecurityOption1$Outbound,
  ListRemoteSessionClientsSecurityOption1
>;
export declare function listRemoteSessionClientsSecurityOption1ToJSON(
  listRemoteSessionClientsSecurityOption1: ListRemoteSessionClientsSecurityOption1,
): string;
/** @internal */
export type ListRemoteSessionClientsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRemoteSessionClientsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListRemoteSessionClientsSecurityOption2$Outbound,
  ListRemoteSessionClientsSecurityOption2
>;
export declare function listRemoteSessionClientsSecurityOption2ToJSON(
  listRemoteSessionClientsSecurityOption2: ListRemoteSessionClientsSecurityOption2,
): string;
/** @internal */
export type ListRemoteSessionClientsSecurity$Outbound = {
  Option1?: ListRemoteSessionClientsSecurityOption1$Outbound | undefined;
  Option2?: ListRemoteSessionClientsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRemoteSessionClientsSecurity$outboundSchema: z.ZodMiniType<
  ListRemoteSessionClientsSecurity$Outbound,
  ListRemoteSessionClientsSecurity
>;
export declare function listRemoteSessionClientsSecurityToJSON(
  listRemoteSessionClientsSecurity: ListRemoteSessionClientsSecurity,
): string;
/** @internal */
export type ListRemoteSessionClientsRequest$Outbound = {
  remote_session_issuer_id?: string | undefined;
  user_session_issuer_id?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRemoteSessionClientsRequest$outboundSchema: z.ZodMiniType<
  ListRemoteSessionClientsRequest$Outbound,
  ListRemoteSessionClientsRequest
>;
export declare function listRemoteSessionClientsRequestToJSON(
  listRemoteSessionClientsRequest: ListRemoteSessionClientsRequest,
): string;
/** @internal */
export declare const ListRemoteSessionClientsResponse$inboundSchema: z.ZodMiniType<
  ListRemoteSessionClientsResponse,
  unknown
>;
export declare function listRemoteSessionClientsResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListRemoteSessionClientsResponse, SDKValidationError>;
//# sourceMappingURL=listremotesessionclients.d.ts.map
