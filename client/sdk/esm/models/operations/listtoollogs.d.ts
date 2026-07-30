import * as z from "zod/v3";
import { ClosedEnum } from "../../types/enums.js";
export type ListToolLogsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListToolLogsSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListToolLogsSecurity = {
  option1?: ListToolLogsSecurityOption1 | undefined;
  option2?: ListToolLogsSecurityOption2 | undefined;
};
/**
 * Status filter
 */
export declare const Status: {
  readonly Success: "success";
  readonly Failure: "failure";
};
/**
 * Status filter
 */
export type Status = ClosedEnum<typeof Status>;
/**
 * Tool type filter
 */
export declare const ToolType: {
  readonly Http: "http";
  readonly Function: "function";
  readonly Prompt: "prompt";
};
/**
 * Tool type filter
 */
export type ToolType = ClosedEnum<typeof ToolType>;
/**
 * Pagination direction
 */
export declare const Direction: {
  readonly Next: "next";
  readonly Prev: "prev";
};
/**
 * Pagination direction
 */
export type Direction = ClosedEnum<typeof Direction>;
/**
 * Sort order
 */
export declare const Sort: {
  readonly Asc: "asc";
  readonly Desc: "desc";
};
/**
 * Sort order
 */
export type Sort = ClosedEnum<typeof Sort>;
export type ListToolLogsRequest = {
  /**
   * Tool ID
   */
  toolId?: string | undefined;
  /**
   * Start timestamp
   */
  tsStart?: Date | undefined;
  /**
   * End timestamp
   */
  tsEnd?: Date | undefined;
  /**
   * Cursor for pagination
   */
  cursor?: string | undefined;
  /**
   * Status filter
   */
  status?: Status | undefined;
  /**
   * Server name filter
   */
  serverName?: string | undefined;
  /**
   * Tool name filter
   */
  toolName?: string | undefined;
  /**
   * Tool type filter
   */
  toolType?: ToolType | undefined;
  /**
   * Tool URNs filter
   */
  toolUrns?: Array<string> | undefined;
  /**
   * Number of items per page (1-100)
   */
  perPage?: number | undefined;
  /**
   * Pagination direction
   */
  direction?: Direction | undefined;
  /**
   * Sort order
   */
  sort?: Sort | undefined;
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
export type ListToolLogsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolLogsSecurityOption1$outboundSchema: z.ZodType<
  ListToolLogsSecurityOption1$Outbound,
  z.ZodTypeDef,
  ListToolLogsSecurityOption1
>;
export declare function listToolLogsSecurityOption1ToJSON(
  listToolLogsSecurityOption1: ListToolLogsSecurityOption1,
): string;
/** @internal */
export type ListToolLogsSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListToolLogsSecurityOption2$outboundSchema: z.ZodType<
  ListToolLogsSecurityOption2$Outbound,
  z.ZodTypeDef,
  ListToolLogsSecurityOption2
>;
export declare function listToolLogsSecurityOption2ToJSON(
  listToolLogsSecurityOption2: ListToolLogsSecurityOption2,
): string;
/** @internal */
export type ListToolLogsSecurity$Outbound = {
  Option1?: ListToolLogsSecurityOption1$Outbound | undefined;
  Option2?: ListToolLogsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListToolLogsSecurity$outboundSchema: z.ZodType<
  ListToolLogsSecurity$Outbound,
  z.ZodTypeDef,
  ListToolLogsSecurity
>;
export declare function listToolLogsSecurityToJSON(
  listToolLogsSecurity: ListToolLogsSecurity,
): string;
/** @internal */
export declare const Status$outboundSchema: z.ZodNativeEnum<typeof Status>;
/** @internal */
export declare const ToolType$outboundSchema: z.ZodNativeEnum<typeof ToolType>;
/** @internal */
export declare const Direction$outboundSchema: z.ZodNativeEnum<
  typeof Direction
>;
/** @internal */
export declare const Sort$outboundSchema: z.ZodNativeEnum<typeof Sort>;
/** @internal */
export type ListToolLogsRequest$Outbound = {
  tool_id?: string | undefined;
  ts_start?: string | undefined;
  ts_end?: string | undefined;
  cursor?: string | undefined;
  status?: string | undefined;
  server_name?: string | undefined;
  tool_name?: string | undefined;
  tool_type?: string | undefined;
  tool_urns?: Array<string> | undefined;
  per_page: number;
  direction: string;
  sort: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListToolLogsRequest$outboundSchema: z.ZodType<
  ListToolLogsRequest$Outbound,
  z.ZodTypeDef,
  ListToolLogsRequest
>;
export declare function listToolLogsRequestToJSON(
  listToolLogsRequest: ListToolLogsRequest,
): string;
//# sourceMappingURL=listtoollogs.d.ts.map
