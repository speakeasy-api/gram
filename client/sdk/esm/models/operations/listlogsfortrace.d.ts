import * as z from "zod/v4-mini";
export type ListLogsForTraceSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListLogsForTraceSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListLogsForTraceSecurityOption3 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListLogsForTraceSecurityOption4 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListLogsForTraceSecurity = {
  option1?: ListLogsForTraceSecurityOption1 | undefined;
  option2?: ListLogsForTraceSecurityOption2 | undefined;
  option3?: ListLogsForTraceSecurityOption3 | undefined;
  option4?: ListLogsForTraceSecurityOption4 | undefined;
};
export type ListLogsForTraceRequest = {
  /**
   * Trace ID (32 hex characters)
   */
  traceId: string;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type ListLogsForTraceSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListLogsForTraceSecurityOption1$outboundSchema: z.ZodMiniType<
  ListLogsForTraceSecurityOption1$Outbound,
  ListLogsForTraceSecurityOption1
>;
export declare function listLogsForTraceSecurityOption1ToJSON(
  listLogsForTraceSecurityOption1: ListLogsForTraceSecurityOption1,
): string;
/** @internal */
export type ListLogsForTraceSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListLogsForTraceSecurityOption2$outboundSchema: z.ZodMiniType<
  ListLogsForTraceSecurityOption2$Outbound,
  ListLogsForTraceSecurityOption2
>;
export declare function listLogsForTraceSecurityOption2ToJSON(
  listLogsForTraceSecurityOption2: ListLogsForTraceSecurityOption2,
): string;
/** @internal */
export type ListLogsForTraceSecurityOption3$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListLogsForTraceSecurityOption3$outboundSchema: z.ZodMiniType<
  ListLogsForTraceSecurityOption3$Outbound,
  ListLogsForTraceSecurityOption3
>;
export declare function listLogsForTraceSecurityOption3ToJSON(
  listLogsForTraceSecurityOption3: ListLogsForTraceSecurityOption3,
): string;
/** @internal */
export type ListLogsForTraceSecurityOption4$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListLogsForTraceSecurityOption4$outboundSchema: z.ZodMiniType<
  ListLogsForTraceSecurityOption4$Outbound,
  ListLogsForTraceSecurityOption4
>;
export declare function listLogsForTraceSecurityOption4ToJSON(
  listLogsForTraceSecurityOption4: ListLogsForTraceSecurityOption4,
): string;
/** @internal */
export type ListLogsForTraceSecurity$Outbound = {
  Option1?: ListLogsForTraceSecurityOption1$Outbound | undefined;
  Option2?: ListLogsForTraceSecurityOption2$Outbound | undefined;
  Option3?: ListLogsForTraceSecurityOption3$Outbound | undefined;
  Option4?: ListLogsForTraceSecurityOption4$Outbound | undefined;
};
/** @internal */
export declare const ListLogsForTraceSecurity$outboundSchema: z.ZodMiniType<
  ListLogsForTraceSecurity$Outbound,
  ListLogsForTraceSecurity
>;
export declare function listLogsForTraceSecurityToJSON(
  listLogsForTraceSecurity: ListLogsForTraceSecurity,
): string;
/** @internal */
export type ListLogsForTraceRequest$Outbound = {
  trace_id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListLogsForTraceRequest$outboundSchema: z.ZodMiniType<
  ListLogsForTraceRequest$Outbound,
  ListLogsForTraceRequest
>;
export declare function listLogsForTraceRequestToJSON(
  listLogsForTraceRequest: ListLogsForTraceRequest,
): string;
//# sourceMappingURL=listlogsfortrace.d.ts.map
