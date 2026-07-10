import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListRemoteSessionsResult } from "../components/listremotesessionsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListRemoteSessionsSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListRemoteSessionsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListRemoteSessionsSecurity = {
  option1?: ListRemoteSessionsSecurityOption1 | undefined;
  option2?: ListRemoteSessionsSecurityOption2 | undefined;
};
export type ListRemoteSessionsRequest = {
  /**
   * Exact-match filter on subject URN.
   */
  subjectUrn?: string | undefined;
  /**
   * Filter by remote_session_client id.
   */
  remoteSessionClientId?: string | undefined;
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
export type ListRemoteSessionsResponse = {
  result: ListRemoteSessionsResult;
};
/** @internal */
export type ListRemoteSessionsSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRemoteSessionsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListRemoteSessionsSecurityOption1$Outbound,
  ListRemoteSessionsSecurityOption1
>;
export declare function listRemoteSessionsSecurityOption1ToJSON(
  listRemoteSessionsSecurityOption1: ListRemoteSessionsSecurityOption1,
): string;
/** @internal */
export type ListRemoteSessionsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRemoteSessionsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListRemoteSessionsSecurityOption2$Outbound,
  ListRemoteSessionsSecurityOption2
>;
export declare function listRemoteSessionsSecurityOption2ToJSON(
  listRemoteSessionsSecurityOption2: ListRemoteSessionsSecurityOption2,
): string;
/** @internal */
export type ListRemoteSessionsSecurity$Outbound = {
  Option1?: ListRemoteSessionsSecurityOption1$Outbound | undefined;
  Option2?: ListRemoteSessionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRemoteSessionsSecurity$outboundSchema: z.ZodMiniType<
  ListRemoteSessionsSecurity$Outbound,
  ListRemoteSessionsSecurity
>;
export declare function listRemoteSessionsSecurityToJSON(
  listRemoteSessionsSecurity: ListRemoteSessionsSecurity,
): string;
/** @internal */
export type ListRemoteSessionsRequest$Outbound = {
  subject_urn?: string | undefined;
  remote_session_client_id?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRemoteSessionsRequest$outboundSchema: z.ZodMiniType<
  ListRemoteSessionsRequest$Outbound,
  ListRemoteSessionsRequest
>;
export declare function listRemoteSessionsRequestToJSON(
  listRemoteSessionsRequest: ListRemoteSessionsRequest,
): string;
/** @internal */
export declare const ListRemoteSessionsResponse$inboundSchema: z.ZodMiniType<
  ListRemoteSessionsResponse,
  unknown
>;
export declare function listRemoteSessionsResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListRemoteSessionsResponse, SDKValidationError>;
//# sourceMappingURL=listremotesessions.d.ts.map
