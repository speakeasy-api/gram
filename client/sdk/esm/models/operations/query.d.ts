import * as z from "zod/v4-mini";
import {
  QueryPayload,
  QueryPayload$Outbound,
} from "../components/querypayload.js";
export type QuerySecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type QueryRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  queryPayload: QueryPayload;
};
/** @internal */
export type QuerySecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const QuerySecurity$outboundSchema: z.ZodMiniType<
  QuerySecurity$Outbound,
  QuerySecurity
>;
export declare function querySecurityToJSON(
  querySecurity: QuerySecurity,
): string;
/** @internal */
export type QueryRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  QueryPayload: QueryPayload$Outbound;
};
/** @internal */
export declare const QueryRequest$outboundSchema: z.ZodMiniType<
  QueryRequest$Outbound,
  QueryRequest
>;
export declare function queryRequestToJSON(queryRequest: QueryRequest): string;
//# sourceMappingURL=query.d.ts.map
