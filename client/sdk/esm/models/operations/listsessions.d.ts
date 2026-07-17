import * as z from "zod/v4-mini";
import {
  ListSessionsPayload,
  ListSessionsPayload$Outbound,
} from "../components/listsessionspayload.js";
export type ListSessionsSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListSessionsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  listSessionsPayload: ListSessionsPayload;
};
/** @internal */
export type ListSessionsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListSessionsSecurity$outboundSchema: z.ZodMiniType<
  ListSessionsSecurity$Outbound,
  ListSessionsSecurity
>;
export declare function listSessionsSecurityToJSON(
  listSessionsSecurity: ListSessionsSecurity,
): string;
/** @internal */
export type ListSessionsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  ListSessionsPayload: ListSessionsPayload$Outbound;
};
/** @internal */
export declare const ListSessionsRequest$outboundSchema: z.ZodMiniType<
  ListSessionsRequest$Outbound,
  ListSessionsRequest
>;
export declare function listSessionsRequestToJSON(
  listSessionsRequest: ListSessionsRequest,
): string;
//# sourceMappingURL=listsessions.d.ts.map
