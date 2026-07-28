import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListOrganizationRemoteSessionsResult } from "../components/listorganizationremotesessionsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListOrganizationRemoteSessionClientSessionsSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type ListOrganizationRemoteSessionClientSessionsRequest = {
  /**
   * The remote_session_client id.
   */
  clientId: string;
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
};
export type ListOrganizationRemoteSessionClientSessionsResponse = {
  result: ListOrganizationRemoteSessionsResult;
};
/** @internal */
export type ListOrganizationRemoteSessionClientSessionsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientSessionsSecurity$outboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionClientSessionsSecurity$Outbound,
  ListOrganizationRemoteSessionClientSessionsSecurity
>;
export declare function listOrganizationRemoteSessionClientSessionsSecurityToJSON(
  listOrganizationRemoteSessionClientSessionsSecurity: ListOrganizationRemoteSessionClientSessionsSecurity,
): string;
/** @internal */
export type ListOrganizationRemoteSessionClientSessionsRequest$Outbound = {
  client_id: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientSessionsRequest$outboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionClientSessionsRequest$Outbound,
  ListOrganizationRemoteSessionClientSessionsRequest
>;
export declare function listOrganizationRemoteSessionClientSessionsRequestToJSON(
  listOrganizationRemoteSessionClientSessionsRequest: ListOrganizationRemoteSessionClientSessionsRequest,
): string;
/** @internal */
export declare const ListOrganizationRemoteSessionClientSessionsResponse$inboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionClientSessionsResponse,
  unknown
>;
export declare function listOrganizationRemoteSessionClientSessionsResponseFromJSON(
  jsonString: string,
): SafeParseResult<
  ListOrganizationRemoteSessionClientSessionsResponse,
  SDKValidationError
>;
//# sourceMappingURL=listorganizationremotesessionclientsessions.d.ts.map
