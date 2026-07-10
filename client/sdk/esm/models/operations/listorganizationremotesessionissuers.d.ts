import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListOrganizationRemoteSessionIssuersResult } from "../components/listorganizationremotesessionissuersresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListOrganizationRemoteSessionIssuersSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type ListOrganizationRemoteSessionIssuersRequest = {
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
export type ListOrganizationRemoteSessionIssuersResponse = {
  result: ListOrganizationRemoteSessionIssuersResult;
};
/** @internal */
export type ListOrganizationRemoteSessionIssuersSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionIssuersSecurity$outboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionIssuersSecurity$Outbound,
  ListOrganizationRemoteSessionIssuersSecurity
>;
export declare function listOrganizationRemoteSessionIssuersSecurityToJSON(
  listOrganizationRemoteSessionIssuersSecurity: ListOrganizationRemoteSessionIssuersSecurity,
): string;
/** @internal */
export type ListOrganizationRemoteSessionIssuersRequest$Outbound = {
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionIssuersRequest$outboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionIssuersRequest$Outbound,
  ListOrganizationRemoteSessionIssuersRequest
>;
export declare function listOrganizationRemoteSessionIssuersRequestToJSON(
  listOrganizationRemoteSessionIssuersRequest: ListOrganizationRemoteSessionIssuersRequest,
): string;
/** @internal */
export declare const ListOrganizationRemoteSessionIssuersResponse$inboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionIssuersResponse,
  unknown
>;
export declare function listOrganizationRemoteSessionIssuersResponseFromJSON(
  jsonString: string,
): SafeParseResult<
  ListOrganizationRemoteSessionIssuersResponse,
  SDKValidationError
>;
//# sourceMappingURL=listorganizationremotesessionissuers.d.ts.map
