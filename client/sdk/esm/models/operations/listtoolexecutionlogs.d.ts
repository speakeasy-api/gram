import * as z from "zod/v3";
import { ClosedEnum } from "../../types/enums.js";
export type ListToolExecutionLogsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListToolExecutionLogsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListToolExecutionLogsSecurityOption3 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListToolExecutionLogsSecurityOption4 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListToolExecutionLogsSecurity = {
  option1?: ListToolExecutionLogsSecurityOption1 | undefined;
  option2?: ListToolExecutionLogsSecurityOption2 | undefined;
  option3?: ListToolExecutionLogsSecurityOption3 | undefined;
  option4?: ListToolExecutionLogsSecurityOption4 | undefined;
};
/**
 * Log level filter
 */
export declare const Level: {
  readonly Debug: "debug";
  readonly Info: "info";
  readonly Warn: "warn";
  readonly Error: "error";
};
/**
 * Log level filter
 */
export type Level = ClosedEnum<typeof Level>;
/**
 * Log source filter
 */
export declare const Source: {
  readonly Stdout: "stdout";
  readonly Stderr: "stderr";
};
/**
 * Log source filter
 */
export type Source = ClosedEnum<typeof Source>;
/**
 * Pagination direction
 */
export declare const QueryParamDirection: {
  readonly Next: "next";
  readonly Prev: "prev";
};
/**
 * Pagination direction
 */
export type QueryParamDirection = ClosedEnum<typeof QueryParamDirection>;
/**
 * Sort order
 */
export declare const QueryParamSort: {
  readonly Asc: "asc";
  readonly Desc: "desc";
};
/**
 * Sort order
 */
export type QueryParamSort = ClosedEnum<typeof QueryParamSort>;
export type ListToolExecutionLogsRequest = {
  /**
   * Start timestamp
   */
  tsStart?: Date | undefined;
  /**
   * End timestamp
   */
  tsEnd?: Date | undefined;
  /**
   * Deployment ID filter
   */
  deploymentId?: string | undefined;
  /**
   * Function ID filter
   */
  functionId?: string | undefined;
  /**
   * Instance filter
   */
  instance?: string | undefined;
  /**
   * Log level filter
   */
  level?: Level | undefined;
  /**
   * Log source filter
   */
  source?: Source | undefined;
  /**
   * Cursor for pagination
   */
  cursor?: string | undefined;
  /**
   * Number of items per page (1-100)
   */
  perPage?: number | undefined;
  /**
   * Pagination direction
   */
  direction?: QueryParamDirection | undefined;
  /**
   * Sort order
   */
  sort?: QueryParamSort | undefined;
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
export type ListToolExecutionLogsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolExecutionLogsSecurityOption1$outboundSchema: z.ZodType<
  ListToolExecutionLogsSecurityOption1$Outbound,
  z.ZodTypeDef,
  ListToolExecutionLogsSecurityOption1
>;
export declare function listToolExecutionLogsSecurityOption1ToJSON(
  listToolExecutionLogsSecurityOption1: ListToolExecutionLogsSecurityOption1,
): string;
/** @internal */
export type ListToolExecutionLogsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolExecutionLogsSecurityOption2$outboundSchema: z.ZodType<
  ListToolExecutionLogsSecurityOption2$Outbound,
  z.ZodTypeDef,
  ListToolExecutionLogsSecurityOption2
>;
export declare function listToolExecutionLogsSecurityOption2ToJSON(
  listToolExecutionLogsSecurityOption2: ListToolExecutionLogsSecurityOption2,
): string;
/** @internal */
export type ListToolExecutionLogsSecurityOption3$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolExecutionLogsSecurityOption3$outboundSchema: z.ZodType<
  ListToolExecutionLogsSecurityOption3$Outbound,
  z.ZodTypeDef,
  ListToolExecutionLogsSecurityOption3
>;
export declare function listToolExecutionLogsSecurityOption3ToJSON(
  listToolExecutionLogsSecurityOption3: ListToolExecutionLogsSecurityOption3,
): string;
/** @internal */
export type ListToolExecutionLogsSecurityOption4$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListToolExecutionLogsSecurityOption4$outboundSchema: z.ZodType<
  ListToolExecutionLogsSecurityOption4$Outbound,
  z.ZodTypeDef,
  ListToolExecutionLogsSecurityOption4
>;
export declare function listToolExecutionLogsSecurityOption4ToJSON(
  listToolExecutionLogsSecurityOption4: ListToolExecutionLogsSecurityOption4,
): string;
/** @internal */
export type ListToolExecutionLogsSecurity$Outbound = {
  Option1?: ListToolExecutionLogsSecurityOption1$Outbound | undefined;
  Option2?: ListToolExecutionLogsSecurityOption2$Outbound | undefined;
  Option3?: ListToolExecutionLogsSecurityOption3$Outbound | undefined;
  Option4?: ListToolExecutionLogsSecurityOption4$Outbound | undefined;
};
/** @internal */
export declare const ListToolExecutionLogsSecurity$outboundSchema: z.ZodType<
  ListToolExecutionLogsSecurity$Outbound,
  z.ZodTypeDef,
  ListToolExecutionLogsSecurity
>;
export declare function listToolExecutionLogsSecurityToJSON(
  listToolExecutionLogsSecurity: ListToolExecutionLogsSecurity,
): string;
/** @internal */
export declare const Level$outboundSchema: z.ZodNativeEnum<typeof Level>;
/** @internal */
export declare const Source$outboundSchema: z.ZodNativeEnum<typeof Source>;
/** @internal */
export declare const QueryParamDirection$outboundSchema: z.ZodNativeEnum<
  typeof QueryParamDirection
>;
/** @internal */
export declare const QueryParamSort$outboundSchema: z.ZodNativeEnum<
  typeof QueryParamSort
>;
/** @internal */
export type ListToolExecutionLogsRequest$Outbound = {
  ts_start?: string | undefined;
  ts_end?: string | undefined;
  deployment_id?: string | undefined;
  function_id?: string | undefined;
  instance?: string | undefined;
  level?: string | undefined;
  source?: string | undefined;
  cursor?: string | undefined;
  per_page: number;
  direction: string;
  sort: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListToolExecutionLogsRequest$outboundSchema: z.ZodType<
  ListToolExecutionLogsRequest$Outbound,
  z.ZodTypeDef,
  ListToolExecutionLogsRequest
>;
export declare function listToolExecutionLogsRequestToJSON(
  listToolExecutionLogsRequest: ListToolExecutionLogsRequest,
): string;
//# sourceMappingURL=listtoolexecutionlogs.d.ts.map
