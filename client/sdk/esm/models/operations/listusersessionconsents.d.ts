import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListUserSessionConsentsResult } from "../components/listusersessionconsentsresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListUserSessionConsentsSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListUserSessionConsentsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListUserSessionConsentsSecurity = {
  option1?: ListUserSessionConsentsSecurityOption1 | undefined;
  option2?: ListUserSessionConsentsSecurityOption2 | undefined;
};
export type ListUserSessionConsentsRequest = {
  /**
   * Filter by subject URN.
   */
  subjectUrn?: string | undefined;
  /**
   * Filter by user_session_client id.
   */
  userSessionClientId?: string | undefined;
  /**
   * Filter by user_session_issuer id (joins through user_session_clients).
   */
  userSessionIssuerId?: string | undefined;
  /**
   * Pagination cursor: id of the last item from the previous page.
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
export type ListUserSessionConsentsResponse = {
  result: ListUserSessionConsentsResult;
};
/** @internal */
export type ListUserSessionConsentsSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListUserSessionConsentsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListUserSessionConsentsSecurityOption1$Outbound,
  ListUserSessionConsentsSecurityOption1
>;
export declare function listUserSessionConsentsSecurityOption1ToJSON(
  listUserSessionConsentsSecurityOption1: ListUserSessionConsentsSecurityOption1,
): string;
/** @internal */
export type ListUserSessionConsentsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListUserSessionConsentsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListUserSessionConsentsSecurityOption2$Outbound,
  ListUserSessionConsentsSecurityOption2
>;
export declare function listUserSessionConsentsSecurityOption2ToJSON(
  listUserSessionConsentsSecurityOption2: ListUserSessionConsentsSecurityOption2,
): string;
/** @internal */
export type ListUserSessionConsentsSecurity$Outbound = {
  Option1?: ListUserSessionConsentsSecurityOption1$Outbound | undefined;
  Option2?: ListUserSessionConsentsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListUserSessionConsentsSecurity$outboundSchema: z.ZodMiniType<
  ListUserSessionConsentsSecurity$Outbound,
  ListUserSessionConsentsSecurity
>;
export declare function listUserSessionConsentsSecurityToJSON(
  listUserSessionConsentsSecurity: ListUserSessionConsentsSecurity,
): string;
/** @internal */
export type ListUserSessionConsentsRequest$Outbound = {
  subject_urn?: string | undefined;
  user_session_client_id?: string | undefined;
  user_session_issuer_id?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListUserSessionConsentsRequest$outboundSchema: z.ZodMiniType<
  ListUserSessionConsentsRequest$Outbound,
  ListUserSessionConsentsRequest
>;
export declare function listUserSessionConsentsRequestToJSON(
  listUserSessionConsentsRequest: ListUserSessionConsentsRequest,
): string;
/** @internal */
export declare const ListUserSessionConsentsResponse$inboundSchema: z.ZodMiniType<
  ListUserSessionConsentsResponse,
  unknown
>;
export declare function listUserSessionConsentsResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListUserSessionConsentsResponse, SDKValidationError>;
//# sourceMappingURL=listusersessionconsents.d.ts.map
