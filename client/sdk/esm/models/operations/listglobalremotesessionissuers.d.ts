import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListRemoteSessionIssuersResult } from "../components/listremotesessionissuersresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListGlobalRemoteSessionIssuersSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListGlobalRemoteSessionIssuersRequest = {
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
export type ListGlobalRemoteSessionIssuersResponse = {
  result: ListRemoteSessionIssuersResult;
};
/** @internal */
export type ListGlobalRemoteSessionIssuersSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGlobalRemoteSessionIssuersSecurity$outboundSchema: z.ZodMiniType<
  ListGlobalRemoteSessionIssuersSecurity$Outbound,
  ListGlobalRemoteSessionIssuersSecurity
>;
export declare function listGlobalRemoteSessionIssuersSecurityToJSON(
  listGlobalRemoteSessionIssuersSecurity: ListGlobalRemoteSessionIssuersSecurity,
): string;
/** @internal */
export type ListGlobalRemoteSessionIssuersRequest$Outbound = {
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGlobalRemoteSessionIssuersRequest$outboundSchema: z.ZodMiniType<
  ListGlobalRemoteSessionIssuersRequest$Outbound,
  ListGlobalRemoteSessionIssuersRequest
>;
export declare function listGlobalRemoteSessionIssuersRequestToJSON(
  listGlobalRemoteSessionIssuersRequest: ListGlobalRemoteSessionIssuersRequest,
): string;
/** @internal */
export declare const ListGlobalRemoteSessionIssuersResponse$inboundSchema: z.ZodMiniType<
  ListGlobalRemoteSessionIssuersResponse,
  unknown
>;
export declare function listGlobalRemoteSessionIssuersResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListGlobalRemoteSessionIssuersResponse, SDKValidationError>;
//# sourceMappingURL=listglobalremotesessionissuers.d.ts.map
