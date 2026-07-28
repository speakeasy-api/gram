import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListRemoteSessionIssuersResult } from "../components/listremotesessionissuersresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListRemoteSessionIssuersSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListRemoteSessionIssuersSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListRemoteSessionIssuersSecurity = {
  option1?: ListRemoteSessionIssuersSecurityOption1 | undefined;
  option2?: ListRemoteSessionIssuersSecurityOption2 | undefined;
};
export type ListRemoteSessionIssuersRequest = {
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
export type ListRemoteSessionIssuersResponse = {
  result: ListRemoteSessionIssuersResult;
};
/** @internal */
export type ListRemoteSessionIssuersSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRemoteSessionIssuersSecurityOption1$outboundSchema: z.ZodMiniType<
  ListRemoteSessionIssuersSecurityOption1$Outbound,
  ListRemoteSessionIssuersSecurityOption1
>;
export declare function listRemoteSessionIssuersSecurityOption1ToJSON(
  listRemoteSessionIssuersSecurityOption1: ListRemoteSessionIssuersSecurityOption1,
): string;
/** @internal */
export type ListRemoteSessionIssuersSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRemoteSessionIssuersSecurityOption2$outboundSchema: z.ZodMiniType<
  ListRemoteSessionIssuersSecurityOption2$Outbound,
  ListRemoteSessionIssuersSecurityOption2
>;
export declare function listRemoteSessionIssuersSecurityOption2ToJSON(
  listRemoteSessionIssuersSecurityOption2: ListRemoteSessionIssuersSecurityOption2,
): string;
/** @internal */
export type ListRemoteSessionIssuersSecurity$Outbound = {
  Option1?: ListRemoteSessionIssuersSecurityOption1$Outbound | undefined;
  Option2?: ListRemoteSessionIssuersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRemoteSessionIssuersSecurity$outboundSchema: z.ZodMiniType<
  ListRemoteSessionIssuersSecurity$Outbound,
  ListRemoteSessionIssuersSecurity
>;
export declare function listRemoteSessionIssuersSecurityToJSON(
  listRemoteSessionIssuersSecurity: ListRemoteSessionIssuersSecurity,
): string;
/** @internal */
export type ListRemoteSessionIssuersRequest$Outbound = {
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRemoteSessionIssuersRequest$outboundSchema: z.ZodMiniType<
  ListRemoteSessionIssuersRequest$Outbound,
  ListRemoteSessionIssuersRequest
>;
export declare function listRemoteSessionIssuersRequestToJSON(
  listRemoteSessionIssuersRequest: ListRemoteSessionIssuersRequest,
): string;
/** @internal */
export declare const ListRemoteSessionIssuersResponse$inboundSchema: z.ZodMiniType<
  ListRemoteSessionIssuersResponse,
  unknown
>;
export declare function listRemoteSessionIssuersResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListRemoteSessionIssuersResponse, SDKValidationError>;
//# sourceMappingURL=listremotesessionissuers.d.ts.map
