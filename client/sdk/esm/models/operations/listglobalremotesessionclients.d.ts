import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListRemoteSessionClientsResult } from "../components/listremotesessionclientsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListGlobalRemoteSessionClientsSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListGlobalRemoteSessionClientsRequest = {
  /**
   * The global remote_session_issuer id to list clients for.
   */
  remoteSessionIssuerId: string;
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
};
export type ListGlobalRemoteSessionClientsResponse = {
  result: ListRemoteSessionClientsResult;
};
/** @internal */
export type ListGlobalRemoteSessionClientsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGlobalRemoteSessionClientsSecurity$outboundSchema: z.ZodMiniType<
  ListGlobalRemoteSessionClientsSecurity$Outbound,
  ListGlobalRemoteSessionClientsSecurity
>;
export declare function listGlobalRemoteSessionClientsSecurityToJSON(
  listGlobalRemoteSessionClientsSecurity: ListGlobalRemoteSessionClientsSecurity,
): string;
/** @internal */
export type ListGlobalRemoteSessionClientsRequest$Outbound = {
  remote_session_issuer_id: string;
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGlobalRemoteSessionClientsRequest$outboundSchema: z.ZodMiniType<
  ListGlobalRemoteSessionClientsRequest$Outbound,
  ListGlobalRemoteSessionClientsRequest
>;
export declare function listGlobalRemoteSessionClientsRequestToJSON(
  listGlobalRemoteSessionClientsRequest: ListGlobalRemoteSessionClientsRequest,
): string;
/** @internal */
export declare const ListGlobalRemoteSessionClientsResponse$inboundSchema: z.ZodMiniType<
  ListGlobalRemoteSessionClientsResponse,
  unknown
>;
export declare function listGlobalRemoteSessionClientsResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListGlobalRemoteSessionClientsResponse, SDKValidationError>;
//# sourceMappingURL=listglobalremotesessionclients.d.ts.map
