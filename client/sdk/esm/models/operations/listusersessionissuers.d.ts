import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { ListUserSessionIssuersResult } from "../components/listusersessionissuersresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListUserSessionIssuersSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListUserSessionIssuersSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListUserSessionIssuersSecurity = {
  option1?: ListUserSessionIssuersSecurityOption1 | undefined;
  option2?: ListUserSessionIssuersSecurityOption2 | undefined;
};
export type ListUserSessionIssuersRequest = {
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
export type ListUserSessionIssuersResponse = {
  result: ListUserSessionIssuersResult;
};
/** @internal */
export type ListUserSessionIssuersSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListUserSessionIssuersSecurityOption1$outboundSchema: z.ZodMiniType<
  ListUserSessionIssuersSecurityOption1$Outbound,
  ListUserSessionIssuersSecurityOption1
>;
export declare function listUserSessionIssuersSecurityOption1ToJSON(
  listUserSessionIssuersSecurityOption1: ListUserSessionIssuersSecurityOption1,
): string;
/** @internal */
export type ListUserSessionIssuersSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListUserSessionIssuersSecurityOption2$outboundSchema: z.ZodMiniType<
  ListUserSessionIssuersSecurityOption2$Outbound,
  ListUserSessionIssuersSecurityOption2
>;
export declare function listUserSessionIssuersSecurityOption2ToJSON(
  listUserSessionIssuersSecurityOption2: ListUserSessionIssuersSecurityOption2,
): string;
/** @internal */
export type ListUserSessionIssuersSecurity$Outbound = {
  Option1?: ListUserSessionIssuersSecurityOption1$Outbound | undefined;
  Option2?: ListUserSessionIssuersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListUserSessionIssuersSecurity$outboundSchema: z.ZodMiniType<
  ListUserSessionIssuersSecurity$Outbound,
  ListUserSessionIssuersSecurity
>;
export declare function listUserSessionIssuersSecurityToJSON(
  listUserSessionIssuersSecurity: ListUserSessionIssuersSecurity,
): string;
/** @internal */
export type ListUserSessionIssuersRequest$Outbound = {
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListUserSessionIssuersRequest$outboundSchema: z.ZodMiniType<
  ListUserSessionIssuersRequest$Outbound,
  ListUserSessionIssuersRequest
>;
export declare function listUserSessionIssuersRequestToJSON(
  listUserSessionIssuersRequest: ListUserSessionIssuersRequest,
): string;
/** @internal */
export declare const ListUserSessionIssuersResponse$inboundSchema: z.ZodMiniType<
  ListUserSessionIssuersResponse,
  unknown
>;
export declare function listUserSessionIssuersResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListUserSessionIssuersResponse, SDKValidationError>;
//# sourceMappingURL=listusersessionissuers.d.ts.map
